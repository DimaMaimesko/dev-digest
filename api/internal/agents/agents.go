// Package agents creates and changes reviewer agents, and keeps the history
// of their configs: every config change gets a new version number and a
// snapshot, so an old review can be traced to the exact config that made it.
package agents

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

var (
	// ErrNotFound means the agent isn't in the workspace.
	ErrNotFound = errors.New("agent not found")
	// ErrUnknownSkill means a skill to link isn't in the workspace.
	ErrUnknownSkill = errors.New("unknown skill")
)

// New is what creating an agent takes. Empty Strategy and CIFailOn, and a nil
// RepoIntel or Enabled, get the database defaults.
type New struct {
	Name         string
	Description  string
	Provider     string
	Model        string
	SystemPrompt string
	OutputSchema json.RawMessage // any JSON; nil or null for none
	Strategy     string
	CIFailOn     string
	RepoIntel    *bool
	Enabled      *bool
}

// Patch is a change to an agent. Nil fields stay as they are. OutputSchema is
// raw JSON: nil leaves it, "null" clears it.
type Patch struct {
	Name         *string
	Description  *string
	Provider     *string
	Model        *string
	SystemPrompt *string
	OutputSchema json.RawMessage
	Strategy     *string
	CIFailOn     *string
	RepoIntel    *bool
	Enabled      *bool
}

// Store changes agents in the database.
type Store struct {
	db *pgxpool.Pool
}

// NewStore returns a Store using db.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Create adds an agent at version 1, with its first snapshot.
func (s *Store) Create(ctx context.Context, workspace, createdBy uuid.UUID, in New) (postgres.Agent, error) {
	var a postgres.Agent
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		var err error
		a, err = q.CreateAgent(ctx, postgres.CreateAgentParams{
			WorkspaceID:  workspace,
			Name:         in.Name,
			Description:  in.Description,
			Provider:     in.Provider,
			Model:        in.Model,
			SystemPrompt: in.SystemPrompt,
			OutputSchema: jsonOrNull(in.OutputSchema),
			Strategy:     cmp.Or(in.Strategy, "single-pass"),
			CiFailOn:     cmp.Or(in.CIFailOn, "critical"),
			RepoIntel:    boolOr(in.RepoIntel, true),
			Enabled:      boolOr(in.Enabled, true),
			CreatedBy:    &createdBy,
		})
		if err != nil {
			return err
		}
		return snapshot(ctx, q, a)
	})
	if err != nil {
		return postgres.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	return a, nil
}

// Update applies p to the agent. When p changes its config (anything but
// Enabled), the agent gets the next version number and a snapshot.
func (s *Store) Update(ctx context.Context, workspace, id uuid.UUID, p Patch) (postgres.Agent, error) {
	var a postgres.Agent
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		old, err := q.LockAgent(ctx, postgres.LockAgentParams{WorkspaceID: workspace, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		next := apply(old, p)
		if a, err = q.UpdateAgent(ctx, postgres.UpdateAgentParams{
			ID:           id,
			Name:         next.Name,
			Description:  next.Description,
			Provider:     next.Provider,
			Model:        next.Model,
			SystemPrompt: next.SystemPrompt,
			OutputSchema: next.OutputSchema,
			Strategy:     next.Strategy,
			CiFailOn:     next.CiFailOn,
			RepoIntel:    next.RepoIntel,
			Enabled:      next.Enabled,
			Version:      next.Version,
		}); err != nil {
			return err
		}
		if a.Version != old.Version {
			return snapshot(ctx, q, a)
		}
		return nil
	})
	if err != nil {
		return postgres.Agent{}, fmt.Errorf("update agent %s: %w", id, err)
	}
	return a, nil
}

// apply returns old with p applied, and the next version number when that
// changes the config. The rule is the TS server's: changing the name or
// description counts, though the snapshot doesn't hold them, and sending an
// output schema always counts, even the same one.
func apply(old postgres.Agent, p Patch) postgres.Agent {
	next := old
	changed := false
	set := func(dst *string, v *string) {
		if v != nil && *v != *dst {
			*dst = *v
			changed = true
		}
	}
	set(&next.Name, p.Name)
	set(&next.Description, p.Description)
	set(&next.Provider, p.Provider)
	set(&next.Model, p.Model)
	set(&next.SystemPrompt, p.SystemPrompt)
	set(&next.Strategy, p.Strategy)
	set(&next.CiFailOn, p.CIFailOn)
	if p.RepoIntel != nil && *p.RepoIntel != next.RepoIntel {
		next.RepoIntel = *p.RepoIntel
		changed = true
	}
	if p.OutputSchema != nil {
		next.OutputSchema = jsonOrNull(p.OutputSchema)
		changed = true
	}
	if p.Enabled != nil {
		next.Enabled = *p.Enabled // turning an agent on or off isn't a config change
	}
	if changed {
		next.Version++
	}
	return next
}

// snapshot saves the agent's current config as its version.
func snapshot(ctx context.Context, q *postgres.Queries, a postgres.Agent) error {
	links, err := q.ListAgentSkills(ctx, a.ID)
	if err != nil {
		return err
	}
	skills := make([]string, 0, len(links))
	for _, l := range links {
		skills = append(skills, l.SkillID.String())
	}
	outputSchema := json.RawMessage("null")
	if a.OutputSchema != nil {
		outputSchema = a.OutputSchema
	}
	config, err := json.Marshal(map[string]any{
		"provider":      a.Provider,
		"model":         a.Model,
		"system_prompt": a.SystemPrompt,
		"output_schema": outputSchema,
		"strategy":      a.Strategy,
		"ci_fail_on":    a.CiFailOn,
		"repo_intel":    a.RepoIntel,
		"skills":        skills,
	})
	if err != nil {
		return err
	}
	return q.InsertAgentVersion(ctx, postgres.InsertAgentVersionParams{AgentID: a.ID, Version: a.Version, ConfigJson: config})
}

// Delete removes the agent, with its versions and skill links. Its past runs
// stay, without the agent.
func (s *Store) Delete(ctx context.Context, workspace, id uuid.UUID) error {
	n, err := postgres.New(s.db).DeleteAgent(ctx, postgres.DeleteAgentParams{WorkspaceID: workspace, ID: id})
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSkills replaces the agent's linked skills with skills, in that order.
func (s *Store) SetSkills(ctx context.Context, workspace, agent uuid.UUID, skills []uuid.UUID) error {
	return s.changeSkills(ctx, workspace, agent, skills, func(q *postgres.Queries) error {
		if err := q.UnlinkAllSkills(ctx, agent); err != nil {
			return err
		}
		for i, skill := range skills {
			if err := q.LinkSkill(ctx, postgres.LinkSkillParams{AgentID: agent, SkillID: skill, Order: int32(i)}); err != nil {
				return err
			}
		}
		return nil
	})
}

// LinkSkill links one skill to the agent, keeping the others. Without an
// order it goes last; a skill already linked gets the new order.
func (s *Store) LinkSkill(ctx context.Context, workspace, agent, skill uuid.UUID, order *int32) error {
	return s.changeSkills(ctx, workspace, agent, []uuid.UUID{skill}, func(q *postgres.Queries) error {
		if order == nil {
			n, err := q.CountLinkedSkills(ctx, agent)
			if err != nil {
				return err
			}
			last := int32(n)
			order = &last
		}
		return q.LinkSkill(ctx, postgres.LinkSkillParams{AgentID: agent, SkillID: skill, Order: *order})
	})
}

// changeSkills runs change in a transaction, after checking that the agent
// and all of skills are in the workspace.
func (s *Store) changeSkills(ctx context.Context, workspace, agent uuid.UUID, skills []uuid.UUID, change func(*postgres.Queries) error) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		if _, err := q.LockAgent(ctx, postgres.LockAgentParams{WorkspaceID: workspace, ID: agent}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		found, err := q.SkillsInWorkspace(ctx, postgres.SkillsInWorkspaceParams{WorkspaceID: workspace, Ids: skills})
		if err != nil {
			return err
		}
		for _, id := range skills {
			if !slices.Contains(found, id) {
				return fmt.Errorf("%w: %s", ErrUnknownSkill, id)
			}
		}
		return change(q)
	})
	if err != nil {
		return fmt.Errorf("change skills of agent %s: %w", agent, err)
	}
	return nil
}

// jsonOrNull returns v, or nil (SQL NULL) when v is empty or JSON null.
func jsonOrNull(v json.RawMessage) []byte {
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return nil
	}
	return v
}

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
