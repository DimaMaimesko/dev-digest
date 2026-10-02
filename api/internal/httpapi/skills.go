package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/skills"
)

// skillJSON is a skill as the web app reads it (Skill in
// client/src/vendor/shared/contracts/knowledge.ts).
type skillJSON struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Type          string          `json:"type"`
	Source        string          `json:"source"`
	Body          string          `json:"body"`
	Enabled       bool            `json:"enabled"`
	Version       int32           `json:"version"`
	EvidenceFiles json.RawMessage `json:"evidence_files"` // a JSON array of paths; null when unset
	AgentCount    int64           `json:"agent_count"`    // how many agents use it
	CreatedAt     string          `json:"created_at"`
}

func toSkillJSON(sk postgres.Skill, agentCount int64) skillJSON {
	return skillJSON{
		ID:            sk.ID.String(),
		Name:          sk.Name,
		Description:   sk.Description,
		Type:          sk.Type,
		Source:        sk.Source,
		Body:          sk.Body,
		Enabled:       sk.Enabled,
		Version:       sk.Version,
		EvidenceFiles: sk.EvidenceFiles, // nil (SQL NULL) encodes as null
		AgentCount:    agentCount,
		CreatedAt:     jsTime(sk.CreatedAt),
	}
}

// skillVersionJSON is one saved body of a skill (SkillVersion in the
// contracts).
type skillVersionJSON struct {
	SkillID   string  `json:"skill_id"`
	Version   int32   `json:"version"`
	Body      string  `json:"body"`
	Message   *string `json:"message"`
	CreatedAt string  `json:"created_at"`
}

// skillTypes and skillSources are the allowed values of a skill's type and
// where it came from (SkillType and SkillSource in the contracts).
var (
	skillTypes   = []string{"rubric", "convention", "security", "custom"}
	skillSources = []string{"manual", "imported_url", "extracted", "community"}
)

// listSkills answers GET /skills: the workspace's skills, oldest first.
func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListSkills(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]skillJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSkillJSON(row.Skill, row.AgentCount))
	}
	writeJSON(w, http.StatusOK, out)
}

// getSkill answers GET /skills/{id}.
func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.writeSkill(w, r, http.StatusOK, id)
}

// writeSkill answers with the skill and the number of agents using it.
func (s *Server) writeSkill(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	row, err := s.queries.GetSkill(r.Context(), postgres.GetSkillParams{WorkspaceID: s.workspace, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Skill not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, status, toSkillJSON(row.Skill, row.AgentCount))
}

// createSkill answers POST /skills with the new skill, at version 1.
func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	in := skills.New{
		Name:        deref(f.str("name", true, notEmpty)),
		Description: deref(f.str("description", false, nil)),
		Type:        deref(f.str("type", true, among(skillTypes...))),
		Source:      deref(f.str("source", false, among(skillSources...))),
		Body:        deref(f.str("body", true, notEmpty)),
		Enabled:     f.boolean("enabled"),
		Message:     deref(f.str("message", false, nil)),
	}
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	sk, err := s.skills.Create(r.Context(), s.workspace, in)
	if !s.skillChanged(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, toSkillJSON(sk, 0))
}

// updateSkill answers PUT /skills/{id}: it changes the fields in the body. A
// new body gives the skill a new version, described by "message" if given.
func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	patch := skills.Patch{
		Name:        f.str("name", false, notEmpty),
		Description: f.str("description", false, nil),
		Type:        f.str("type", false, among(skillTypes...)),
		Body:        f.str("body", false, notEmpty),
		Enabled:     f.boolean("enabled"),
		Message:     deref(f.str("message", false, nil)),
	}
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	_, err := s.skills.Update(r.Context(), s.workspace, id, patch)
	if !s.skillChanged(w, r, err) {
		return
	}
	s.writeSkill(w, r, http.StatusOK, id)
}

// restoreSkillVersion answers POST /skills/{id}/versions/{version}/restore:
// the version's body becomes the skill's, as a new version.
func (s *Server) restoreSkillVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	version, ok := pathPositiveInt(w, r, "version")
	if !ok {
		return
	}
	_, err := s.skills.Restore(r.Context(), s.workspace, id, version)
	if !s.skillChanged(w, r, err) {
		return
	}
	s.writeSkill(w, r, http.StatusOK, id)
}

// skillChanged answers when creating or changing a skill failed, and reports
// whether it succeeded.
func (s *Server) skillChanged(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, skills.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Skill not found")
	case errors.Is(err, skills.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Skill version not found")
	case errors.Is(err, skills.ErrNameTaken):
		invalid(w, []issue{{Path: []string{"name"}, Message: "A skill with this name already exists"}})
	default:
		s.internalError(w, r, err)
	}
	return false
}

// deleteSkill answers DELETE /skills/{id}. Its versions and its links to
// agents go with it.
func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.skills.Delete(r.Context(), s.workspace, id)
	if errors.Is(err, skills.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Skill not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// listSkillVersions answers GET /skills/{id}/versions: the skill's body
// history, newest first.
func (s *Server) listSkillVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.existingSkill(w, r)
	if !ok {
		return
	}
	versions, err := s.queries.ListSkillVersions(r.Context(), id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]skillVersionJSON, 0, len(versions))
	for _, v := range versions {
		out = append(out, skillVersionJSON{
			SkillID:   v.SkillID.String(),
			Version:   v.Version,
			Body:      v.Body,
			Message:   v.Message,
			CreatedAt: jsTime(v.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// listSkillAgents answers GET /skills/{id}/agents: the agents using the
// skill, oldest first.
func (s *Server) listSkillAgents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.existingSkill(w, r)
	if !ok {
		return
	}
	agents, err := s.queries.ListSkillAgents(r.Context(), postgres.ListSkillAgentsParams{WorkspaceID: s.workspace, SkillID: id})
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

// existingSkill reads the {id} of a skill in the workspace. When the ID is
// invalid or there is no such skill, it answers and returns false.
func (s *Server) existingSkill(w http.ResponseWriter, r *http.Request) (id uuid.UUID, ok bool) {
	id, ok = pathID(w, r)
	if !ok {
		return id, false
	}
	exists, err := s.queries.SkillExists(r.Context(), postgres.SkillExistsParams{WorkspaceID: s.workspace, ID: id})
	if err != nil {
		s.internalError(w, r, err)
		return id, false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "not_found", "Skill not found")
		return id, false
	}
	return id, true
}
