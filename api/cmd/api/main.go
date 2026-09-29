// Command api serves the DevDigest HTTP API for the web app.
//
// It reads its settings from the environment, like the TS server:
// DATABASE_URL, API_PORT (default 3001), WEB_PORT (default 3000; the web
// app's port, allowed by CORS) and LOG_LEVEL (default info). The database must
// be migrated and seeded, as scripts/dev.sh does.
//
// To run it next to the TS server, give it another port:
//
//	API_PORT=3002 go run ./cmd/api
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
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
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

	// IPv4 on every interface, like the TS server. With network "tcp", Go
	// would open a dual-stack IPv6 socket, which macOS lets share a port with
	// a server already listening on IPv4: both would run on one port.
	ln, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(cfg.port))
	if err != nil {
		return err // e.g. the port is taken
	}
	srv := &http.Server{
		Handler:           httpapi.New(pool, workspace, cfg.webOrigin, log).Handler(),
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
	logLevel    slog.Level
}

// loadConfig reads the settings from the environment, with the same names and
// defaults as the TS server.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		databaseURL: getenv("DATABASE_URL"),
		port:        3001,
		webOrigin:   "http://localhost:3000",
		logLevel:    slog.LevelInfo,
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
