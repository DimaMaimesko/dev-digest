// Package onboarding starts, tracks and stores a repository's onboarding
// tour: it gathers a repository's inputs (through internal/git and
// internal/repointel), hands them to internal/tour's Generate, and persists
// each run's outcome. It is the orchestration layer around the pure
// internal/tour domain, the same way internal/repos and internal/runner wrap
// their own domain packages.
package onboarding

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// jobKind names the background job Start enqueues, in the jobs table.
const jobKind = "onboarding-tour"

// ErrNotFound means the workspace has no such repository.
var ErrNotFound = errors.New("repository not found")

// ErrNotIndexed means the repository has no completed index to generate a
// tour from (AC-4).
var ErrNotIndexed = errors.New("repository is not indexed")

// ConfigError means a tour's feature model has no client: the API key (or
// the Claude Code CLI) its provider needs isn't configured (AC-33).
type ConfigError struct {
	// Provider is the feature's chosen provider.
	Provider string
	// Err is the error cfg.LLM(Provider) returned.
	Err error
}

func (e *ConfigError) Error() string { return e.Err.Error() }

// Unwrap lets errors.Is/As see through to Err.
func (e *ConfigError) Unwrap() error { return e.Err }

// Config is what a Store needs.
type Config struct {
	DB   *pgxpool.Pool
	Jobs *jobs.Runner
	// LLM returns the model client for a provider, or an error saying why
	// there is none (a missing API key, an unknown provider). The same
	// shape as runner.LLMFor.
	LLM func(provider string) (review.LLM, error)
	Log *slog.Logger
	// SectionTimeout, when non-zero, overrides tour.Generate's default
	// section timeout (90s): it's read here only so tests can shrink it
	// through Config, and passed to Generate per call rather than held as
	// package-level state (api/CLAUDE.md "no package-level mutable state").
	SectionTimeout time.Duration
}

// Store starts, tracks and stores onboarding tours.
type Store struct {
	db             *pgxpool.Pool
	q              *postgres.Queries
	jobs           *jobs.Runner
	llm            func(provider string) (review.LLM, error)
	log            *slog.Logger
	index          *repointel.Index
	sectionTimeout time.Duration
}

// New returns a Store.
func New(cfg Config) *Store {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Store{
		db:             cfg.DB,
		q:              postgres.New(cfg.DB),
		jobs:           cfg.Jobs,
		llm:            cfg.LLM,
		log:            log,
		index:          repointel.New(cfg.DB),
		sectionTimeout: cfg.SectionTimeout,
	}
}

// FailStale marks failed the runs still marked queued or running when the
// server starts: a server that stopped during generation left them (AC-11).
// It returns how many there were.
func (s *Store) FailStale(ctx context.Context) (int64, error) {
	return s.q.FailStaleOnboardingRuns(ctx)
}

// int32Ptr returns a pointer to n as an int32, for the nullable usage
// columns, which are always set once a run has an outcome.
func int32Ptr(n int) *int32 {
	v := int32(n)
	return &v
}
