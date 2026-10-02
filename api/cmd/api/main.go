// Command api serves the DevDigest HTTP API for the web app.
//
// It listens on 127.0.0.1 only, so other machines can't reach it. It reads its
// settings from the environment, like the TS server: DATABASE_URL, API_PORT
// (default 3001), WEB_PORT (default 3000; the web app's port, allowed by
// CORS), DEVDIGEST_CLONE_DIR (default ~/.devdigest/workspace; a relative path
// is relative to the working directory) and LOG_LEVEL (default info). API keys
// come from ~/.devdigest/secrets.json, then from the environment. The database
// must be migrated and seeded (cmd/db), as scripts/dev.sh does.
//
// It doesn't read api/.env itself: scripts/dev.sh loads that file into its
// environment.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/anthropic"
	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/openai"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
	"github.com/DimaMaimesko/dev-digest/api/internal/repos"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		stop()
		os.Exit(1)
	}
}

// run serves the API until ctx is cancelled, then shuts down gracefully:
// requests in progress get up to 10 seconds to finish.
func run(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: cfg.logLevel}))

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	defer pool.Close()

	workspace, err := postgres.New(pool).WorkspaceByName(ctx, "default")
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the database has no default workspace; run `db migrate` and `db seed` (cmd/db)")
	}
	if err != nil {
		return fmt.Errorf("find the default workspace: %w", err)
	}
	user, err := postgres.New(pool).UserByEmail(ctx, "you@local")
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the database has no local user (you@local); run `db seed` (cmd/db)")
	}
	if err != nil {
		return fmt.Errorf("find the local user: %w", err)
	}

	// Loopback only, like the TS server: the API stores API keys and runs git,
	// so other machines on the network must not reach it.
	ln, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(cfg.port))
	if err != nil {
		return err // e.g. the port is taken
	}

	store := secrets.New(cfg.secretsPath, getenv)
	modelAPIs := httpapi.ModelAPIs{
		OpenAI:     openai.OpenAIURL,
		OpenRouter: openai.OpenRouterURL,
		Anthropic:  anthropic.DefaultURL,
	}
	reviews := runner.New(runner.Config{
		DB:        pool,
		LLM:       reviewModel(store, modelAPIs),
		CloneDir:  cfg.cloneDir,
		RepoIntel: cfg.repoIntel,
		Log:       log,
	})
	defer reviews.Close() // after the server stops: the runs in progress end as failed
	githubToken := func() (string, error) { return store.Get(secrets.GitHubToken) }
	background := jobs.New(pool, log)
	defer background.Close() // likewise for clones in progress
	if n, err := reviews.FailStale(ctx); err != nil {
		return fmt.Errorf("mark stale runs failed: %w", err)
	} else if n > 0 {
		log.Info("marked runs left running by a stopped server as failed", "runs", n)
	}

	api := httpapi.New(httpapi.Config{
		DB:        pool,
		Workspace: workspace,
		User:      user,
		WebOrigin: cfg.webOrigin,
		CloneDir:  cfg.cloneDir,
		Secrets:   store,
		Log:       log,
		GitHubAPI: github.DefaultURL,
		ModelAPIs: modelAPIs,
		Runner:    reviews,
		Repos: repos.NewStore(repos.Config{
			DB:       pool,
			Jobs:     background,
			CloneDir: cfg.cloneDir,
			Token:    githubToken,
			Indexer:  repointel.NewIndexer(pool, githubToken),
		}),
	})
	srv := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	// Shutdown waits for every request, and a run's live log would otherwise
	// last until the run ends.
	srv.RegisterOnShutdown(api.CloseStreams)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("DevDigest API listening", "url", fmt.Sprintf("http://localhost:%d", cfg.port))

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// reviewModel returns the model client for a provider, with its API key
// read from the secrets store at each run, so a key saved in the web app
// takes effect at once.
func reviewModel(store *secrets.Store, apis httpapi.ModelAPIs) runner.LLMFor {
	return func(provider string) (review.LLM, error) {
		name, ok := secrets.ProviderKey(provider)
		if !ok {
			return nil, fmt.Errorf("unknown provider %q", provider)
		}
		key, err := store.Get(name)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("%s is not configured", name)
		}
		switch provider {
		case "openai":
			return openai.NewCompatible(apis.OpenAI, key), nil
		case "openrouter":
			return openai.NewOpenRouter(key), nil
		}
		return anthropic.New(apis.Anthropic, key), nil
	}
}

// config is the server's settings.
type config struct {
	databaseURL string
	port        int
	webOrigin   string // the web app's origin, allowed by CORS
	cloneDir    string // absolute
	secretsPath string
	logLevel    slog.Level
	repoIntel   bool // repo-intel context in reviews; on unless REPO_INTEL_ENABLED=false
}

// loadConfig reads the settings from the environment, with the same names and
// defaults as the TS server.
func loadConfig(getenv func(string) string) (config, error) {
	home := getenv("HOME")
	if home == "" {
		return config{}, errors.New("HOME is not set")
	}
	cfg := config{
		databaseURL: getenv("DATABASE_URL"),
		port:        3001,
		webOrigin:   "http://localhost:3000",
		cloneDir:    filepath.Join(home, ".devdigest", "workspace"),
		secretsPath: filepath.Join(home, ".devdigest", "secrets.json"),
		logLevel:    slog.LevelInfo,
		repoIntel:   getenv("REPO_INTEL_ENABLED") != "false",
	}
	if v := getenv("DEVDIGEST_CLONE_DIR"); v != "" {
		dir, err := filepath.Abs(v)
		if err != nil {
			return config{}, fmt.Errorf("DEVDIGEST_CLONE_DIR %q: %w", v, err)
		}
		cfg.cloneDir = dir
	}
	if cfg.databaseURL == "" {
		cfg.databaseURL = "postgres://devdigest:devdigest@localhost:5433/devdigest"
	}
	if v := getenv("API_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return config{}, fmt.Errorf("API_PORT %q: not a number", v)
		}
		cfg.port = port
	}
	if v := getenv("WEB_PORT"); v != "" {
		if _, err := strconv.Atoi(v); err != nil {
			return config{}, fmt.Errorf("WEB_PORT %q: not a number", v)
		}
		cfg.webOrigin = "http://localhost:" + v
	}
	switch v := getenv("LOG_LEVEL"); v {
	case "", "info":
	case "trace", "debug":
		cfg.logLevel = slog.LevelDebug
	case "warn":
		cfg.logLevel = slog.LevelWarn
	case "error", "fatal":
		cfg.logLevel = slog.LevelError
	case "silent":
		cfg.logLevel = slog.LevelError + 1 // above every level we log at
	default:
		return config{}, fmt.Errorf("LOG_LEVEL %q: want fatal, error, warn, info, debug, trace or silent", v)
	}
	return cfg, nil
}
