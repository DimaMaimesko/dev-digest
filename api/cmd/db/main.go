// Command db prepares DevDigest's database.
//
//	go run ./cmd/db migrate   # apply the migrations the database hasn't had
//	go run ./cmd/db seed      # add the default workspace, settings and agents
//
// It connects to DATABASE_URL (default: the docker-compose database). The
// migrations are Drizzle's, in ../server/src/db/migrations from api/, or the
// folder in -dir or MIGRATIONS_DIR. A database the TS server migrated or
// seeded continues where it left off.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/migrate"
	"github.com/DimaMaimesko/dev-digest/api/internal/seed"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}
}

const usage = "usage: db migrate [-dir DIR] | db seed"

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	flags := flag.NewFlagSet("db "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", cmp.Or(getenv("MIGRATIONS_DIR"), "../server/src/db/migrations"), "the migrations folder")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	url := cmp.Or(getenv("DATABASE_URL"), "postgres://devdigest:devdigest@localhost:5433/devdigest")

	switch args[0] {
	case "migrate":
		conn, err := pgx.Connect(ctx, url)
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		applied, err := migrate.Run(ctx, conn, *dir)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fmt.Fprintln(out, "the database is up to date")
		}
		for _, tag := range applied {
			fmt.Fprintln(out, "applied", tag)
		}
		return nil
	case "seed":
		conn, err := pgx.Connect(ctx, url)
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		workspace, user, err := seed.Run(ctx, conn)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "seeded: workspace %s, user %s\n", workspace, user)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", args[0], usage)
}
