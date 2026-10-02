package skills

import (
	"context"
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
	old := postgres.Skill{Name: "rubric", Description: "d", Type: "rubric", Body: "# Rubric", Enabled: true, Version: 3}
	tests := []struct {
		name        string
		patch       Patch
		wantVersion int32
		check       func(postgres.Skill) bool
	}{
		{"nothing", Patch{}, 3, nil},
		{"the same body", Patch{Body: ptr("# Rubric")}, 3, nil},
		{"a new body", Patch{Body: ptr("# Rubric v2")}, 4,
			func(s postgres.Skill) bool { return s.Body == "# Rubric v2" }},
		{"renaming isn't a new version", Patch{Name: ptr("quality")}, 3,
			func(s postgres.Skill) bool { return s.Name == "quality" }},
		{"nor is a new type, description or turning it off",
			Patch{Type: ptr("security"), Description: ptr("new"), Enabled: ptr(false)}, 3,
			func(s postgres.Skill) bool { return s.Type == "security" && s.Description == "new" && !s.Enabled }},
		{"several changes, one new version", Patch{Name: ptr("quality"), Body: ptr("x")}, 4,
			func(s postgres.Skill) bool { return s.Name == "quality" && s.Body == "x" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := apply(old, tt.patch)
			if next.Version != tt.wantVersion {
				t.Errorf("version = %d, want %d", next.Version, tt.wantVersion)
			}
			if tt.check != nil && !tt.check(next) {
				t.Errorf("skill = %+v", next)
			}
		})
	}
}

// fixture is a database with a workspace in it.
type fixture struct {
	db        *pgxpool.Pool
	store     *Store
	workspace uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: pgtest.New(t)}
	f.store = NewStore(f.db)
	if err := f.db.QueryRow(context.Background(),
		`INSERT INTO workspaces (name) VALUES ('default') RETURNING id`).Scan(&f.workspace); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) create(t *testing.T, name string) postgres.Skill {
	t.Helper()
	sk, err := f.store.Create(context.Background(), f.workspace, New{
		Name: name, Type: "rubric", Body: "v1 body", Message: "Initial rubric",
	})
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

// versions returns the skill's snapshots, newest first, as "vN body (message)".
func (f fixture) versions(t *testing.T, skill uuid.UUID) []string {
	t.Helper()
	rows, err := postgres.New(f.db).ListSkillVersions(context.Background(), skill)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		m := "-"
		if r.Message != nil {
			m = *r.Message
		}
		out = append(out, fmt.Sprintf("v%d %s (%s)", r.Version, r.Body, m))
	}
	return out
}

func equal(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func TestCreate(t *testing.T) {
	f := newFixture(t)
	sk := f.create(t, "rubric")

	if sk.Version != 1 || sk.Source != "manual" || !sk.Enabled || sk.EvidenceFiles != nil {
		t.Errorf("skill = %+v, want version 1 with the defaults", sk)
	}
	if got, want := f.versions(t, sk.ID), []string{"v1 v1 body (Initial rubric)"}; !equal(got, want) {
		t.Errorf("versions = %v, want %v", got, want)
	}
}

func TestNameTaken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.create(t, "rubric")
	other := f.create(t, "other")

	if _, err := f.store.Create(ctx, f.workspace, New{Name: "rubric", Type: "rubric", Body: "b"}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("creating a second %q: err = %v, want ErrNameTaken", "rubric", err)
	}
	if _, err := f.store.Update(ctx, f.workspace, other.ID, Patch{Name: ptr("rubric")}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("renaming to a taken name: err = %v, want ErrNameTaken", err)
	}

	// Another workspace may use the name.
	var ws uuid.UUID
	if err := f.db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, ws, New{Name: "rubric", Type: "rubric", Body: "b"}); err != nil {
		t.Errorf("same name in another workspace: %v", err)
	}
}

func TestUpdateSnapshotsNewBodies(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sk := f.create(t, "rubric")

	sk, err := f.store.Update(ctx, f.workspace, sk.ID, Patch{Body: ptr("v2 body"), Message: "Added tests"})
	if err != nil || sk.Version != 2 {
		t.Fatalf("new body: version %d, err %v; want version 2", sk.Version, err)
	}
	sk, err = f.store.Update(ctx, f.workspace, sk.ID, Patch{Name: ptr("quality"), Enabled: ptr(false), Message: "ignored"})
	if err != nil || sk.Version != 2 || sk.Name != "quality" || sk.Enabled {
		t.Fatalf("rename and turn off: %+v, err %v; want version 2", sk, err)
	}
	sk, err = f.store.Update(ctx, f.workspace, sk.ID, Patch{Body: ptr("v3 body")})
	if err != nil || sk.Version != 3 {
		t.Fatalf("new body, no message: version %d, err %v; want version 3", sk.Version, err)
	}

	want := []string{"v3 v3 body (-)", "v2 v2 body (Added tests)", "v1 v1 body (Initial rubric)"}
	if got := f.versions(t, sk.ID); !equal(got, want) {
		t.Errorf("versions = %v, want %v", got, want)
	}
}

func TestRestore(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sk := f.create(t, "rubric")
	if _, err := f.store.Update(ctx, f.workspace, sk.ID, Patch{Body: ptr("v2 body")}); err != nil {
		t.Fatal(err)
	}

	sk, err := f.store.Restore(ctx, f.workspace, sk.ID, 1)
	if err != nil || sk.Version != 3 || sk.Body != "v1 body" {
		t.Fatalf("restore v1: %+v, err %v; want v1's body as version 3", sk, err)
	}
	// Restoring the body it already has changes nothing.
	if sk, err = f.store.Restore(ctx, f.workspace, sk.ID, 3); err != nil || sk.Version != 3 {
		t.Fatalf("restore the current body: version %d, err %v; want 3", sk.Version, err)
	}
	want := []string{"v3 v1 body (Restored v1)", "v2 v2 body (-)", "v1 v1 body (Initial rubric)"}
	if got := f.versions(t, sk.ID); !equal(got, want) {
		t.Errorf("versions = %v, want %v", got, want)
	}

	if _, err := f.store.Restore(ctx, f.workspace, sk.ID, 9); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("restore a missing version: err = %v, want ErrVersionNotFound", err)
	}
}

func TestUpdateConcurrently(t *testing.T) {
	f := newFixture(t)
	sk := f.create(t, "rubric")

	// Each new body gets its own version, though they all start at version 1.
	const n = 5
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			_, err := f.store.Update(context.Background(), f.workspace, sk.ID, Patch{Body: ptr(fmt.Sprint("body ", i))})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(f.versions(t, sk.ID)); got != n+1 {
		t.Errorf("%d versions, want %d", got, n+1)
	}
}

func TestNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sk := f.create(t, "rubric")
	missing := uuid.New()

	if _, err := f.store.Update(ctx, f.workspace, missing, Patch{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update: err = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Restore(ctx, f.workspace, missing, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore: err = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, uuid.New(), sk.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete from another workspace: err = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, f.workspace, sk.ID); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if err := f.store.Delete(ctx, f.workspace, sk.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete twice: err = %v, want ErrNotFound", err)
	}
}
