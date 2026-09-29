// Command api serves the DevDigest HTTP API for the web app.
//
// It listens on 127.0.0.1 only, so other machines can't reach it. It reads its
// settings from the environment, like the TS server: DATABASE_URL, API_PORT
// (default 3001), WEB_PORT (default 3000; the web app's port, allowed by
// CORS), DEVDIGEST_CLONE_DIR (default ~/.devdigest/workspace; a relative path
// is relative to the working directory) and LOG_LEVEL (default info). API keys
// come from ~/.devdigest/secrets.json, then from the environment. The database
// must be migrated and seeded, as scripts/dev.sh does.
//
// It doesn't read server/.env. To run it next to the TS server, with the same
// settings and another port, build it and start it from server/:
//
//	make build
//	cd ../server && (set -a; . ./.env; set +a; API_PORT=3002 ../api/bin/api)
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

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
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
		return errors.New("the database has no default workspace; run `pnpm db:migrate && pnpm db:seed` in server/")
	}
	if err != nil {
		return fmt.Errorf("find the default workspace: %w", err)
	}

	// Loopback only, like the TS server: the API stores API keys and runs git,
	// so other machines on the network must not reach it.
	ln, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(cfg.port))
	if err != nil {
		return err // e.g. the port is taken
	}
	srv := &http.Server{
		Handler: httpapi.New(httpapi.Config{
			DB:        pool,
			Workspace: workspace,
			WebOrigin: cfg.webOrigin,
			CloneDir:  cfg.cloneDir,
			Secrets:   secrets.New(cfg.secretsPath, getenv),
			Log:       log,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
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

// config is the server's settings.
type config struct {
	databaseURL string
	port        int
	webOrigin   string // the web app's origin, allowed by CORS
	cloneDir    string // absolute
	secretsPath string
	logLevel    slog.Level
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
