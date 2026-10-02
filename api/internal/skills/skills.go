// Package skills creates and changes skills: named instructions that agents
// add to their review prompt. A skill's body is versioned: every new body gets
// the next version number and a snapshot, so the text an old review used can
// be found again. Other fields are edited in place.
package skills

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

var (
	// ErrNotFound means the skill isn't in the workspace.
	ErrNotFound = errors.New("skill not found")
	// ErrVersionNotFound means the skill has no such version.
	ErrVersionNotFound = errors.New("skill version not found")
	// ErrNameTaken means another skill in the workspace has the name.
	ErrNameTaken = errors.New("skill name taken")
)

// New is what creating a skill takes. An empty Source and a nil Enabled get
// the defaults. Message, when not empty, describes the first version.
type New struct {
	Name        string
	Description string
	Type        string
	Source      string
	Body        string
	Enabled     *bool
	Message     string
}

// Patch is a change to a skill. Nil fields stay as they are. Message, when not
// empty, describes the new version, if the body changes.
type Patch struct {
	Name        *string
	Description *string
	Type        *string
	Body        *string
	Enabled     *bool
	Message     string
}

// Store changes skills in the database.
type Store struct {
	db *pgxpool.Pool
}

// NewStore returns a Store using db.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Create adds a skill at version 1, with its first snapshot.
func (s *Store) Create(ctx context.Context, workspace uuid.UUID, in New) (postgres.Skill, error) {
	var sk postgres.Skill
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		var err error
		sk, err = q.CreateSkill(ctx, postgres.CreateSkillParams{
			WorkspaceID: workspace,
			Name:        in.Name,
			Description: in.Description,
			Type:        in.Type,
			Source:      cmp.Or(in.Source, "manual"),
			Body:        in.Body,
			Enabled:     enabled,
		})
		if err != nil {
			return err
		}
		return snapshot(ctx, q, sk, in.Message)
	})
	if err != nil {
		return postgres.Skill{}, fmt.Errorf("create skill: %w", nameTaken(err))
	}
	return sk, nil
}

// Update applies p to the skill. A new body gives it the next version number
// and a snapshot; other changes don't.
func (s *Store) Update(ctx context.Context, workspace, id uuid.UUID, p Patch) (postgres.Skill, error) {
	return s.change(ctx, workspace, id, func(_ *postgres.Queries, old postgres.Skill) (postgres.Skill, string, error) {
		return apply(old, p), p.Message, nil
	})
}

// Restore makes the body of an earlier version the skill's body, as a new
// version, so the history only grows. Restoring the current body changes
// nothing.
func (s *Store) Restore(ctx context.Context, workspace, id uuid.UUID, version int32) (postgres.Skill, error) {
	return s.change(ctx, workspace, id, func(q *postgres.Queries, old postgres.Skill) (postgres.Skill, string, error) {
		body, err := q.GetSkillVersionBody(ctx, postgres.GetSkillVersionBodyParams{SkillID: id, Version: version})
		if errors.Is(err, pgx.ErrNoRows) {
			return old, "", ErrVersionNotFound
		}
		if err != nil {
			return old, "", err
		}
		return apply(old, Patch{Body: &body}), fmt.Sprintf("Restored v%d", version), nil
	})
}

// change locks the skill, lets next work out its new state and the message of
// a new version, then saves it, with a snapshot when the version changed.
func (s *Store) change(ctx context.Context, workspace, id uuid.UUID,
	next func(*postgres.Queries, postgres.Skill) (postgres.Skill, string, error)) (postgres.Skill, error) {
	var sk postgres.Skill
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		old, err := q.LockSkill(ctx, postgres.LockSkillParams{WorkspaceID: workspace, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		n, message, err := next(q, old)
		if err != nil {
			return err
		}
		if sk, err = q.UpdateSkill(ctx, postgres.UpdateSkillParams{
			ID:          id,
			Name:        n.Name,
			Description: n.Description,
			Type:        n.Type,
			Body:        n.Body,
			Enabled:     n.Enabled,
			Version:     n.Version,
		}); err != nil {
			return err
		}
		if sk.Version != old.Version {
			return snapshot(ctx, q, sk, message)
		}
		return nil
	})
	if err != nil {
		return postgres.Skill{}, fmt.Errorf("change skill %s: %w", id, nameTaken(err))
	}
	return sk, nil
}

// apply returns old with p applied, and the next version number when the body
// changes.
func apply(old postgres.Skill, p Patch) postgres.Skill {
	next := old
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&next.Name, p.Name)
	set(&next.Description, p.Description)
	set(&next.Type, p.Type)
	set(&next.Body, p.Body)
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}
	if next.Body != old.Body {
		next.Version++
	}
	return next
}

// snapshot saves the skill's current body as its version.
func snapshot(ctx context.Context, q *postgres.Queries, sk postgres.Skill, message string) error {
	var m *string
	if message != "" {
		m = &message
	}
	return q.InsertSkillVersion(ctx, postgres.InsertSkillVersionParams{
		SkillID: sk.ID, Version: sk.Version, Body: sk.Body, Message: m,
	})
}

// Delete removes the skill, with its versions and its links to agents.
func (s *Store) Delete(ctx context.Context, workspace, id uuid.UUID) error {
	n, err := postgres.New(s.db).DeleteSkill(ctx, postgres.DeleteSkillParams{WorkspaceID: workspace, ID: id})
	if err != nil {
		return fmt.Errorf("delete skill %s: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// nameTaken returns ErrNameTaken, wrapping err, when err is a clash on the
// unique skill name; otherwise err.
func nameTaken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "skills_ws_name_uq" {
		return fmt.Errorf("%w: %w", ErrNameTaken, err)
	}
	return err
}
