package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/agents"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// agentJSON is a reviewer agent as the web app reads it (Agent in
// server/src/vendor/shared/contracts/knowledge.ts).
type agentJSON struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	SystemPrompt string          `json:"system_prompt"`
	OutputSchema json.RawMessage `json:"output_schema"` // any JSON, passed through; null when unset
	Enabled      bool            `json:"enabled"`
	Version      int32           `json:"version"`
	Strategy     string          `json:"strategy"`
	CIFailOn     string          `json:"ci_fail_on"`
	RepoIntel    bool            `json:"repo_intel"`
	SkillCount   int64           `json:"skill_count"` // how many skills it links
}

func toAgentJSON(a postgres.Agent, skillCount int64) agentJSON {
	return agentJSON{
		ID:           a.ID.String(),
		Name:         a.Name,
		Description:  a.Description,
		Provider:     a.Provider,
		Model:        a.Model,
		SystemPrompt: a.SystemPrompt,
		OutputSchema: a.OutputSchema, // nil (SQL NULL) encodes as null
		Enabled:      a.Enabled,
		Version:      a.Version,
		Strategy:     a.Strategy,
		CIFailOn:     a.CiFailOn,
		RepoIntel:    a.RepoIntel,
		SkillCount:   skillCount,
	}
}

// agentVersionJSON is one saved snapshot of an agent's config (AgentVersion
// in the contracts). A new one is saved whenever the config changes.
type agentVersionJSON struct {
	AgentID   string          `json:"agent_id"`
	Version   int32           `json:"version"`
	Config    agentConfigJSON `json:"config"`
	CreatedAt string          `json:"created_at"`
}

// agentConfigJSON is the config inside a snapshot (AgentVersionConfig).
type agentConfigJSON struct {
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	SystemPrompt string          `json:"system_prompt"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"` // left out when the snapshot has none
	Strategy     string          `json:"strategy"`
	CIFailOn     string          `json:"ci_fail_on"`
	RepoIntel    bool            `json:"repo_intel"`
	Skills       []string        `json:"skills"` // linked skill IDs, in order
}

// toAgentVersionJSON decodes a stored snapshot. Snapshots are untyped JSON in
// the database and older ones could have another shape, so it checks the
// config the way the TS server's Zod schema does, and fails rather than send
// the web app something it can't read. Unknown keys are dropped.
func toAgentVersionJSON(v postgres.AgentVersion) (agentVersionJSON, error) {
	var cfg agentConfigJSON
	if err := json.Unmarshal(v.ConfigJson, &cfg); err != nil {
		return agentVersionJSON{}, fmt.Errorf("agent %s version %d: %w", v.AgentID, v.Version, err)
	}
	switch {
	case !slices.Contains(providers, cfg.Provider),
		!slices.Contains(strategies, cfg.Strategy),
		!slices.Contains(ciFailOns, cfg.CIFailOn),
		cfg.Skills == nil:
		return agentVersionJSON{}, fmt.Errorf("agent %s version %d: config has a missing or unknown provider, strategy, ci_fail_on or skills", v.AgentID, v.Version)
	}
	return agentVersionJSON{
		AgentID:   v.AgentID.String(),
		Version:   v.Version,
		Config:    cfg,
		CreatedAt: jsTime(v.CreatedAt),
	}, nil
}

// listAgents answers GET /agents: the workspace's agents, oldest first.
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListAgents(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]agentJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAgentJSON(row.Agent, row.SkillCount))
	}
	writeJSON(w, http.StatusOK, out)
}

// getAgent answers GET /agents/{id}.
func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.queries.GetAgent(r.Context(), postgres.GetAgentParams{WorkspaceID: s.workspace, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeAgent(w, r, http.StatusOK, a)
}

// writeAgent answers with the agent and the number of skills it links.
func (s *Server) writeAgent(w http.ResponseWriter, r *http.Request, status int, a postgres.Agent) {
	n, err := s.queries.CountLinkedSkills(r.Context(), a.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, status, toAgentJSON(a, n))
}

// listAgentVersions answers GET /agents/{id}/versions: the agent's config
// history, newest first.
func (s *Server) listAgentVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.existingAgent(w, r)
	if !ok {
		return
	}
	versions, err := s.queries.ListAgentVersions(r.Context(), id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]agentVersionJSON, 0, len(versions))
	for _, v := range versions {
		j, err := toAgentVersionJSON(v)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

// getAgentVersion answers GET /agents/{id}/versions/{version}: one snapshot.
func (s *Server) getAgentVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	version, ok := pathPositiveInt(w, r, "version")
	if !ok {
		return
	}
	v, err := s.queries.GetAgentVersion(r.Context(), postgres.GetAgentVersionParams{
		WorkspaceID: s.workspace, AgentID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Agent version not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out, err := toAgentVersionJSON(v)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentSkillJSON is a skill linked to an agent (AgentSkillLink).
type agentSkillJSON struct {
	AgentID string `json:"agent_id"`
	SkillID string `json:"skill_id"`
	Order   int32  `json:"order"`
}

// listAgentSkills answers GET /agents/{id}/skills: the agent's linked skills,
// in the order it uses them.
func (s *Server) listAgentSkills(w http.ResponseWriter, r *http.Request) {
	id, ok := s.existingAgent(w, r)
	if !ok {
		return
	}
	s.writeAgentSkills(w, r, id)
}

// writeAgentSkills answers with the agent's linked skills, in order.
func (s *Server) writeAgentSkills(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	links, err := s.queries.ListAgentSkills(r.Context(), id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]agentSkillJSON, 0, len(links))
	for _, l := range links {
		out = append(out, agentSkillJSON{AgentID: id.String(), SkillID: l.SkillID.String(), Order: l.Order})
	}
	writeJSON(w, http.StatusOK, out)
}

// strategies and ciFailOns are the allowed values of an agent's review
// strategy and CI gate (ReviewStrategy and CiFailOn in the contracts).
var (
	strategies = []string{"single-pass", "map-reduce", "auto"}
	ciFailOns  = []string{"never", "critical", "warning", "any"}
)

// createAgent answers POST /agents with the new agent, at version 1.
func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	in := agents.New{
		Name:         deref(f.str("name", true, notEmpty)),
		Description:  deref(f.str("description", false, nil)),
		Provider:     deref(f.str("provider", true, among(providers...))),
		Model:        deref(f.str("model", true, notEmpty)),
		SystemPrompt: deref(f.str("system_prompt", true, notEmpty)),
		OutputSchema: f.anyJSON("output_schema"),
		Strategy:     deref(f.str("strategy", false, among(strategies...))),
		CIFailOn:     deref(f.str("ci_fail_on", false, among(ciFailOns...))),
		RepoIntel:    f.boolean("repo_intel"),
		Enabled:      f.boolean("enabled"),
	}
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	a, err := s.agents.Create(r.Context(), s.workspace, s.user, in)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAgentJSON(a, 0)) // a new agent links no skills
}

// updateAgent answers PUT /agents/{id}: it changes the fields in the body.
// A config change gives the agent a new version; turning it on or off
// doesn't.
func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	patch := agents.Patch{
		Name:         f.str("name", false, notEmpty),
		Description:  f.str("description", false, nil),
		Provider:     f.str("provider", false, among(providers...)),
		Model:        f.str("model", false, notEmpty),
		SystemPrompt: f.str("system_prompt", false, notEmpty),
		OutputSchema: f.anyJSON("output_schema"),
		Strategy:     f.str("strategy", false, among(strategies...)),
		CIFailOn:     f.str("ci_fail_on", false, among(ciFailOns...)),
		RepoIntel:    f.boolean("repo_intel"),
		Enabled:      f.boolean("enabled"),
	}
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	a, err := s.agents.Update(r.Context(), s.workspace, id, patch)
	if errors.Is(err, agents.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeAgent(w, r, http.StatusOK, a)
}

// deleteAgent answers DELETE /agents/{id}. The agent's versions and skill
// links go with it; its past runs stay.
func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.agents.Delete(r.Context(), s.workspace, id)
	if errors.Is(err, agents.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// changeAgentSkills answers POST /agents/{id}/skills with the agent's linked
// skills after the change. The body either replaces them all, in order
// ({"skill_ids": [...]}), or links one ({"skill_id": "...", "order": 2};
// without an order it goes last).
func (s *Server) changeAgentSkills(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	skillIDs, setAll := f.ids("skill_ids")
	skillID := f.id("skill_id")
	order := f.int32("order")
	switch {
	case len(f.issues) > 0:
	case !setAll && skillID == nil:
		f.bad("Provide skill_ids (set/reorder) or skill_id (link one)")
	case setAll && hasDuplicates(skillIDs):
		f.bad("Lists the same skill twice", "skill_ids")
	}
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}

	var err error
	if setAll {
		err = s.agents.SetSkills(r.Context(), s.workspace, id, skillIDs)
	} else {
		err = s.agents.LinkSkill(r.Context(), s.workspace, id, *skillID, order)
	}
	switch {
	case errors.Is(err, agents.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	case errors.Is(err, agents.ErrUnknownSkill):
		// The TS server answered 500 with the database's foreign key error.
		key := "skill_id"
		if setAll {
			key = "skill_ids"
		}
		invalid(w, []issue{{Path: []string{key}, Message: "Not a skill in this workspace"}})
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.writeAgentSkills(w, r, id)
}

func hasDuplicates(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// deref returns *p, or the zero value ("", 0, …) for nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// existingAgent reads the {id} of an agent in the workspace. When the ID is
// invalid or there is no such agent, it answers and returns false.
func (s *Server) existingAgent(w http.ResponseWriter, r *http.Request) (id uuid.UUID, ok bool) {
	id, ok = pathID(w, r)
	if !ok {
		return id, false
	}
	exists, err := s.queries.AgentExists(r.Context(), postgres.AgentExistsParams{WorkspaceID: s.workspace, ID: id})
	if err != nil {
		s.internalError(w, r, err)
		return id, false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return id, false
	}
	return id, true
}
