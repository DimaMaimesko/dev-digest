// Package jobs runs slow work, such as cloning a repository, in the
// background, a few at a time, and records each job in the jobs table.
package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Runner runs jobs.
type Runner struct {
	q       *postgres.Queries
	log     *slog.Logger
	slots   chan struct{} // one per job that may run at once
	timeout time.Duration // for each job

	ctx  context.Context // ends when the runner closes
	stop context.CancelFunc
	wg   sync.WaitGroup
}

// New returns a Runner that runs 3 jobs at once, as the TS server did, each
// for at most 2 minutes. Close stops it.
func New(db *pgxpool.Pool, log *slog.Logger) *Runner {
	ctx, stop := context.WithCancel(context.Background())
	return &Runner{
		q: postgres.New(db), log: log, slots: make(chan struct{}, 3), timeout: 2 * time.Minute,
		ctx: ctx, stop: stop,
	}
}

// Func is a job's work.
type Func func(ctx context.Context) error

// Enqueue records a job of the workspace and runs work in the background
// when a slot is free. kind and payload describe it in the jobs table.
func (r *Runner) Enqueue(ctx context.Context, workspace uuid.UUID, kind string, payload any, work Func) (uuid.UUID, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := r.q.CreateJob(ctx, postgres.CreateJobParams{WorkspaceID: workspace, Kind: kind, Payload: data})
	if err != nil {
		return uuid.Nil, err
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		select {
		case r.slots <- struct{}{}:
			defer func() { <-r.slots }()
		case <-r.ctx.Done():
			r.finish(id, kind, context.Canceled)
			return
		}
		r.run(id, kind, work)
	}()
	return id, nil
}

func (r *Runner) run(id uuid.UUID, kind string, work Func) {
	if err := r.q.StartJob(r.ctx, id); err != nil {
		r.log.Error("job not started", "job", id, "kind", kind, "err", err)
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	r.finish(id, kind, work(ctx))
}

// finish records a job's outcome, even while the runner closes.
func (r *Runner) finish(id uuid.UUID, kind string, jobErr error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 10*time.Second)
	defer cancel()
	status, msg := "done", (*string)(nil)
	if jobErr != nil {
		status = "failed"
		m := jobErr.Error()
		msg = &m
		r.log.Error("job failed", "job", id, "kind", kind, "err", jobErr)
	}
	if err := r.q.FinishJob(ctx, postgres.FinishJobParams{ID: id, Status: status, Error: msg}); err != nil {
		r.log.Error("job outcome not saved", "job", id, "kind", kind, "err", err)
	}
}

// Wait waits for the jobs enqueued so far to end.
func (r *Runner) Wait() { r.wg.Wait() }

// Close stops the jobs, which end as failed, and waits for them.
func (r *Runner) Close() {
	r.stop()
	r.wg.Wait()
}
