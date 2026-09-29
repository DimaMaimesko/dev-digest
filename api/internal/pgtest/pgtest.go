// Package pgtest gives tests a throwaway Postgres database with the DevDigest
// schema.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/DimaMaimesko/dev-digest/api/internal/migrate"
	"github.com/DimaMaimesko/dev-digest/api/migrations"
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
	return open(t, template)
}

// NewEmpty is New for a database with no migration applied: nothing but
// what Postgres creates.
func NewEmpty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return open(t, "template0")
}

func open(t *testing.T, from string) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	startOnce.Do(func() { startErr = start(context.Background()) })
	if startErr != nil {
		t.Fatalf("start test Postgres: %v", startErr)
	}

	ctx := context.Background()
	name := newDatabase(t, ctx, from)
	pool, err := pgxpool.New(ctx, withDatabase(adminURL, name))
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newDatabase(t *testing.T, ctx context.Context, from string) string {
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
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+from); err != nil {
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
	// With internal/migrate, as the API's databases are.
	_, err = migrate.Run(ctx, conn, migrations.FS)
	return err
}

// withDatabase returns u with its database name replaced.
func withDatabase(u *url.URL, name string) string {
	c := *u
	c.Path = "/" + strings.TrimPrefix(name, "/")
	return c.String()
}
