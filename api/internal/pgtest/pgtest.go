// Package pgtest gives tests a throwaway Postgres database with the DevDigest
// schema.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// template is the database every test database is copied from.
const template = "devdigest_template"

var (
	startOnce sync.Once
	adminURL  *url.URL // the container's "postgres" database
	startErr  error

	createMu sync.Mutex // Postgres can't copy one template twice at the same time
	created  int
)

// New returns a pool for a new database with every migration applied, and
// closes it when the test ends.
//
// The first call in a test binary starts a Postgres container; testcontainers
// removes it when the binary exits. Every call copies a migrated template
// database, which takes milliseconds, so tests don't see each other's rows.
// New skips the test when Docker isn't running.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	startOnce.Do(func() { startErr = start(context.Background()) })
	if startErr != nil {
		t.Fatalf("start test Postgres: %v", startErr)
	}

	ctx := context.Background()
	name := newDatabase(t, ctx)
	pool, err := pgxpool.New(ctx, withDatabase(adminURL, name))
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newDatabase(t *testing.T, ctx context.Context) string {
	t.Helper()
	createMu.Lock()
	defer createMu.Unlock()
	created++
	name := fmt.Sprintf("test_%d", created)

	conn, err := pgx.Connect(ctx, adminURL.String())
	if err != nil {
		t.Fatalf("connect to test Postgres: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+template); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	return name
}

// start runs the container and migrates the template database.
func start(ctx context.Context) error {
	container, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
		postgres.WithDatabase(template),
		postgres.WithUsername("devdigest"),
		postgres.WithPassword("devdigest"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return err
	}
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return err
	}
	u, err := url.Parse(connStr)
	if err != nil {
		return err
	}
	adminURL, err = url.Parse(withDatabase(u, "postgres"))
	if err != nil {
		return err
	}

	// The connection must be closed before the template can be copied.
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return migrate(ctx, conn)
}

// migrate applies the TS server's Drizzle migrations in order, the way
// server/src/db/migrate.ts does. Their names start with a sequence number
// (0000_init.sql, ...), so name order is migration order.
func migrate(ctx context.Context, conn *pgx.Conn) error {
	// The migrations declare vector columns but don't create the pgvector
	// extension; migrate.ts creates it first.
	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return err
	}
	dir := migrationsDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations in %s", dir)
	}
	slices.Sort(files)
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		// With no arguments, pgx sends the file as one simple-protocol query,
		// which may hold many statements.
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

// migrationsDir finds the Drizzle migrations from this source file, so it
// works from any package's test directory.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "server", "src", "db", "migrations")
}

// withDatabase returns u with its database name replaced.
func withDatabase(u *url.URL, name string) string {
	c := *u
	c.Path = "/" + strings.TrimPrefix(name, "/")
	return c.String()
}
