package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
}

func toAgentJSON(a postgres.Agent) agentJSON {
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
	case !slices.Contains([]string{"openai", "anthropic", "openrouter"}, cfg.Provider),
		!slices.Contains([]string{"single-pass", "map-reduce", "auto"}, cfg.Strategy),
		!slices.Contains([]string{"never", "critical", "warning", "any"}, cfg.CIFailOn),
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
	agents, err := s.queries.ListAgents(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]agentJSON, 0, len(agents))
	for _, a := range agents {
		out = append(out, toAgentJSON(a))
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
	writeJSON(w, http.StatusOK, toAgentJSON(a))
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
