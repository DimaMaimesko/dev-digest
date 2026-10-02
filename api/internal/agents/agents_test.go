package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

func ptr[T any](v T) *T { return &v }

func TestApply(t *testing.T) {
	old := postgres.Agent{
		Name: "General", Description: "d", Provider: "openai", Model: "gpt-4.1", SystemPrompt: "p",
		Strategy: "auto", CiFailOn: "critical", RepoIntel: true, Enabled: true, Version: 3,
		OutputSchema: []byte(`{"type": "object"}`),
	}
	tests := []struct {
		name        string
		patch       Patch
		wantVersion int32
		check       func(postgres.Agent) bool
	}{
		{"nothing", Patch{}, 3, nil},
		{"same values", Patch{Name: ptr("General"), Model: ptr("gpt-4.1"), RepoIntel: ptr(true)}, 3, nil},
		{"turning it off isn't a config change", Patch{Enabled: ptr(false)}, 3,
			func(a postgres.Agent) bool { return !a.Enabled }},
		{"new model", Patch{Model: ptr("o3")}, 4,
			func(a postgres.Agent) bool { return a.Model == "o3" }},
		{"new name counts, as in TS", Patch{Name: ptr("Renamed")}, 4, nil},
		{"several changes, one new version", Patch{Model: ptr("o3"), Strategy: ptr("map-reduce"), RepoIntel: ptr(false)}, 4,
			func(a postgres.Agent) bool { return a.Strategy == "map-reduce" && !a.RepoIntel }},
		{"an output schema always counts, as in TS", Patch{OutputSchema: json.RawMessage(`{"type": "object"}`)}, 4, nil},
		{"null clears the output schema", Patch{OutputSchema: json.RawMessage(`null`)}, 4,
			func(a postgres.Agent) bool { return a.OutputSchema == nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := apply(old, tt.patch)
			if next.Version != tt.wantVersion {
				t.Errorf("version = %d, want %d", next.Version, tt.wantVersion)
			}
			if tt.check != nil && !tt.check(next) {
				t.Errorf("agent = %+v", next)
			}
		})
	}
}

// fixture is a database with a workspace, a user and two skills in it, and a
// skill in another workspace.
type fixture struct {
	db                  *pgxpool.Pool
	store               *Store
	workspace, user     uuid.UUID
	skillA, skillB      uuid.UUID
	otherWorkspaceSkill uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: pgtest.New(t)}
	f.store = NewStore(f.db)
	f.workspace = f.id(t, `INSERT INTO workspaces (name) VALUES ('default') RETURNING id`)
	f.user = f.id(t, `INSERT INTO users (email, name) VALUES ('you@local', 'You') RETURNING id`)
	skill := `INSERT INTO skills (workspace_id, name, description, type, source, body)
		VALUES ($1, gen_random_uuid()::text, 'd', 'custom', 'manual', 'b') RETURNING id`
	f.skillA = f.id(t, skill, f.workspace)
	f.skillB = f.id(t, skill, f.workspace)
	f.otherWorkspaceSkill = f.id(t, skill, f.id(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`))
	return f
}

func (f fixture) id(t *testing.T, sql string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return id
}

func (f fixture) create(t *testing.T) postgres.Agent {
	t.Helper()
	a, err := f.store.Create(context.Background(), f.workspace, f.user, New{
		Name: "General", Provider: "openai", Model: "gpt-4.1", SystemPrompt: "You review code.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// versions returns the agent's snapshots: version → config.
func (f fixture) versions(t *testing.T, agent uuid.UUID) map[int32]map[string]any {
	t.Helper()
	rows, err := postgres.New(f.db).ListAgentVersions(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int32]map[string]any{}
	for _, r := range rows {
		var cfg map[string]any
		if err := json.Unmarshal(r.ConfigJson, &cfg); err != nil {
			t.Fatal(err)
		}
		out[r.Version] = cfg
	}
	return out
}

func TestCreate(t *testing.T) {
	f := newFixture(t)
	a := f.create(t)

	if a.Version != 1 || a.Strategy != "single-pass" || a.CiFailOn != "critical" || !a.RepoIntel || !a.Enabled ||
		a.CreatedBy == nil || *a.CreatedBy != f.user || a.OutputSchema != nil {
		t.Errorf("agent = %+v, want version 1 with the defaults", a)
	}
	v := f.versions(t, a.ID)
	if len(v) != 1 || v[1]["model"] != "gpt-4.1" || v[1]["output_schema"] != nil {
		t.Errorf("snapshots = %v, want version 1", v)
	}
}

func TestUpdateSnapshotsConfigChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t)

	a, err := f.store.Update(ctx, f.workspace, a.ID, Patch{Model: ptr("o3"), OutputSchema: json.RawMessage(`{"type": "object"}`)})
	if err != nil || a.Version != 2 {
		t.Fatalf("Update: version %d, err %v", a.Version, err)
	}
	a, err = f.store.Update(ctx, f.workspace, a.ID, Patch{Enabled: ptr(false)})
	if err != nil || a.Version != 2 || a.Enabled {
		t.Fatalf("turning it off: version %d, enabled %v, err %v", a.Version, a.Enabled, err)
	}

	v := f.versions(t, a.ID)
	if len(v) != 2 || v[2]["model"] != "o3" || v[1]["model"] != "gpt-4.1" {
		t.Errorf("snapshots = %v, want versions 1 and 2", v)
	}
	if schema, _ := v[2]["output_schema"].(map[string]any); schema["type"] != "object" {
		t.Errorf("version 2 output_schema = %v", v[2]["output_schema"])
	}
}

// Updates at the same time must each get their own version and snapshot. In
// the TS server both read the same version, so one snapshot was lost.
func TestUpdateConcurrently(t *testing.T) {
	f := newFixture(t)
	a := f.create(t)
	const n = 10

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			_, err := f.store.Update(context.Background(), f.workspace, a.ID, Patch{Model: ptr(fmt.Sprintf("model-%d", i))})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	final, err := postgres.New(f.db).GetAgent(context.Background(), postgres.GetAgentParams{WorkspaceID: f.workspace, ID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	v := f.versions(t, a.ID)
	if final.Version != 1+n || len(v) != 1+n {
		t.Fatalf("version %d with %d snapshots, want %d of each", final.Version, len(v), 1+n)
	}
	// The newest snapshot is the config the agent ended with.
	if v[final.Version]["model"] != final.Model {
		t.Errorf("latest snapshot model %v, agent model %s", v[final.Version]["model"], final.Model)
	}
}

func TestNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := f.id(t, `INSERT INTO workspaces (name) VALUES ('third') RETURNING id`)
	a := f.create(t)

	for name, err := range map[string]error{
		"update in another workspace": func() error { _, err := f.store.Update(ctx, other, a.ID, Patch{}); return err }(),
		"delete in another workspace": f.store.Delete(ctx, other, a.ID),
		"skills in another workspace": f.store.SetSkills(ctx, other, a.ID, nil),
		"update of a missing agent":   func() error { _, err := f.store.Update(ctx, f.workspace, uuid.New(), Patch{}); return err }(),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestSkills(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t)
	links := func() []postgres.ListAgentSkillsRow {
		t.Helper()
		l, err := postgres.New(f.db).ListAgentSkills(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}

	if err := f.store.SetSkills(ctx, f.workspace, a.ID, []uuid.UUID{f.skillB, f.skillA}); err != nil {
		t.Fatal(err)
	}
	if l := links(); len(l) != 2 || l[0].SkillID != f.skillB || l[1].SkillID != f.skillA {
		t.Errorf("after SetSkills: %+v", l)
	}

	// A skill from another workspace is refused, and nothing changes. The TS
	// server linked it.
	err := f.store.SetSkills(ctx, f.workspace, a.ID, []uuid.UUID{f.skillA, f.otherWorkspaceSkill})
	if !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("err = %v, want ErrUnknownSkill", err)
	}
	if l := links(); len(l) != 2 || l[0].SkillID != f.skillB {
		t.Errorf("links changed after a refused update: %+v", l)
	}

	// Linking one without an order puts it last; with an order, it moves.
	if err := f.store.SetSkills(ctx, f.workspace, a.ID, []uuid.UUID{f.skillA}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.LinkSkill(ctx, f.workspace, a.ID, f.skillB, nil); err != nil {
		t.Fatal(err)
	}
	if l := links(); len(l) != 2 || l[1].SkillID != f.skillB || l[1].Order != 1 {
		t.Errorf("after LinkSkill: %+v", l)
	}
	if err := f.store.LinkSkill(ctx, f.workspace, a.ID, f.skillA, ptr[int32](5)); err != nil {
		t.Fatal(err)
	}
	if l := links(); l[1].SkillID != f.skillA || l[1].Order != 5 {
		t.Errorf("after moving skill A: %+v", l)
	}
}

// A snapshot records the skills linked when it was taken, in order.
func TestSnapshotHasSkills(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t)
	if err := f.store.SetSkills(ctx, f.workspace, a.ID, []uuid.UUID{f.skillB, f.skillA}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Update(ctx, f.workspace, a.ID, Patch{Model: ptr("o3")}); err != nil {
		t.Fatal(err)
	}
	v := f.versions(t, a.ID)
	want := []any{f.skillB.String(), f.skillA.String()}
	if got, _ := v[2]["skills"].([]any); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("version 2 skills = %v, want %v", v[2]["skills"], want)
	}
	if got, _ := v[1]["skills"].([]any); len(got) != 0 {
		t.Errorf("version 1 skills = %v, want none: it was taken before linking", got)
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t)
	if err := f.store.SetSkills(ctx, f.workspace, a.ID, []uuid.UUID{f.skillA}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.workspace, a.ID); err != nil {
		t.Fatal(err)
	}
	if v := f.versions(t, a.ID); len(v) != 0 {
		t.Errorf("snapshots left: %v", v)
	}
	if err := f.store.Delete(ctx, f.workspace, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
}
