package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func setup(t *testing.T) (*pgxpool.Pool, uuid.UUID, *jobs.Runner) {
	t.Helper()
	db := pgtest.New(t)
	var ws uuid.UUID
	if err := db.QueryRow(context.Background(), `INSERT INTO workspaces (name) VALUES ('w') RETURNING id`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	r := jobs.New(db, slog.New(slog.DiscardHandler))
	t.Cleanup(r.Close)
	return db, ws, r
}

type row struct {
	Kind, Status, Payload string
	Attempts              int
	Error                 *string
	Started, Finished     bool
}

func job(t *testing.T, db *pgxpool.Pool, id uuid.UUID) row {
	t.Helper()
	var r row
	err := db.QueryRow(context.Background(), `SELECT kind, status, payload::text, attempts, error,
		started_at IS NOT NULL, finished_at IS NOT NULL FROM jobs WHERE id = $1`, id).
		Scan(&r.Kind, &r.Status, &r.Payload, &r.Attempts, &r.Error, &r.Started, &r.Finished)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestEnqueue(t *testing.T) {
	db, ws, r := setup(t)
	ctx := context.Background()
	ran := make(chan struct{}, 1)
	ok, _ := r.Enqueue(ctx, ws, "clone", map[string]string{"repoId": "x"}, func(context.Context) error { ran <- struct{}{}; return nil })
	bad, _ := r.Enqueue(ctx, ws, "clone", nil, func(context.Context) error { return errors.New("no such repo") })
	r.Wait()
	<-ran
	if got := job(t, db, ok); got.Kind != "clone" || got.Status != "done" || got.Payload != `{"repoId": "x"}` ||
		got.Attempts != 1 || got.Error != nil || !got.Started || !got.Finished {
		t.Errorf("done job = %+v", got)
	}
	if got := job(t, db, bad); got.Status != "failed" || got.Error == nil || *got.Error != "no such repo" {
		t.Errorf("failed job = %+v", got)
	}
}

// At most 3 jobs run at once.
func TestSlots(t *testing.T) {
	_, ws, r := setup(t)
	var running, most atomic.Int32
	gate := make(chan struct{})
	for range 6 {
		r.Enqueue(context.Background(), ws, "k", nil, func(context.Context) error {
			n := running.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			<-gate
			running.Add(-1)
			return nil
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(gate)
	r.Wait()
	if most.Load() != 3 {
		t.Errorf("%d jobs ran at once, want 3", most.Load())
	}
}

// Closing stops the running jobs and the waiting ones; both end failed.
func TestClose(t *testing.T) {
	db, ws, r := setup(t)
	started := make(chan struct{}, 3)
	var ids []uuid.UUID
	for range 4 {
		id, _ := r.Enqueue(context.Background(), ws, "k", nil, func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		ids = append(ids, id)
	}
	for range 3 {
		<-started
	}
	r.Close()
	for _, id := range ids {
		if got := job(t, db, id); got.Status != "failed" {
			t.Errorf("job %s: %+v", id, got)
		}
	}
}
