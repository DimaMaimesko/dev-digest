// Package migrate applies the database migrations: the SQL files Drizzle
// generated from the TS schema, recorded in the table Drizzle's migrator
// uses (drizzle.__drizzle_migrations), so a database either migrated keeps
// working with the other.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Beginner starts transactions: a *pgx.Conn or a *pgxpool.Pool.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// migration is one entry of Drizzle's journal.
type migration struct {
	Tag  string `json:"tag"`  // the file's name, without .sql
	When int64  `json:"when"` // milliseconds since 1970, when it was generated; its order
}

// breakpoint separates the statements of a migration file.
const breakpoint = "--> statement-breakpoint"

// Run applies the migrations in dir (Drizzle's folder: meta/_journal.json
// and one .sql file per entry) that the database hasn't had, in one
// transaction, and returns their tags. Like Drizzle, a migration counts as
// applied when it is older than the last one recorded. Unlike Drizzle,
// migrators running at once take turns.
func Run(ctx context.Context, db Beginner, dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta", "_journal.json"))
	if err != nil {
		return nil, fmt.Errorf("read the migrations journal: %w", err)
	}
	var journal struct{ Entries []migration }
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("migrations journal: %w", err)
	}

	var applied []string
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			// First: "IF NOT EXISTS" doesn't keep two runs from both creating.
			`SELECT pg_advisory_xact_lock(hashtextextended('devdigest-migrate', 0))`,
			// Tables declare vector columns; pgvector must exist first.
			`CREATE EXTENSION IF NOT EXISTS vector`,
			`CREATE SCHEMA IF NOT EXISTS drizzle`,
			`CREATE TABLE IF NOT EXISTS drizzle.__drizzle_migrations (id SERIAL PRIMARY KEY, hash text NOT NULL, created_at bigint)`,
		} {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return err
			}
		}
		var last *int64
		err := tx.QueryRow(ctx, `SELECT created_at FROM drizzle.__drizzle_migrations ORDER BY created_at DESC LIMIT 1`).Scan(&last)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		for _, m := range journal.Entries {
			if last != nil && *last >= m.When {
				continue
			}
			sql, err := os.ReadFile(filepath.Join(dir, m.Tag+".sql"))
			if err != nil {
				return fmt.Errorf("migration %s: %w", m.Tag, err)
			}
			for _, stmt := range strings.Split(string(sql), breakpoint) {
				if strings.TrimSpace(stmt) == "" {
					continue
				}
				if _, err := tx.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("migration %s: %w", m.Tag, err)
				}
			}
			sum := sha256.Sum256(sql)
			if _, err := tx.Exec(ctx, `INSERT INTO drizzle.__drizzle_migrations (hash, created_at) VALUES ($1, $2)`,
				hex.EncodeToString(sum[:]), m.When); err != nil {
				return err
			}
			applied = append(applied, m.Tag)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return applied, nil
}
