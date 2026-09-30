# `api/` — DevDigest backend in Go

The DevDigest backend: the HTTP API the Next.js client calls, the review
engine and repo-intel. Rules for the code are in [`CLAUDE.md`](CLAUDE.md).

It is a rewrite of the TypeScript backend, `server/` (Fastify) and
`reviewer-core/`, which were removed once all of it was ported. The last
commit that has them is `e9e4574` (tag `ts-final`). All 40 routes serve the
same JSON as the TS server did, so the client didn't change. With a GitHub
token, reading pull requests first syncs them from GitHub; without one, or
when GitHub can't be reached, the saved ones are served.

## Where to read more

| | |
|---|---|
| [`specs/`](specs) | What the API must do: [HTTP routes](specs/http-api.md), [review runs](specs/review-run.md), [repo-intel](specs/repo-intel.md), [repositories and jobs](specs/repos-jobs.md), [secrets](specs/secrets.md) |
| [`docs/architecture.md`](docs/architecture.md) | Every package, what it was ported from, the dependencies |
| [`docs/go-idioms.md`](docs/go-idioms.md) | What not to carry over from TypeScript |
| [`docs/deviations-from-ts.md`](docs/deviations-from-ts.md) | TS bugs fixed in Go, and TS oddities kept |
| [`docs/history.md`](docs/history.md) | How the port was done and verified |
| [`INSIGHTS.md`](INSIGHTS.md) | Lessons learned: toolchain, database tests, running by hand |

## Migrate and seed the database

```sh
cd api
go run ./cmd/db migrate   # apply the migrations the database hasn't had
go run ./cmd/db seed      # default workspace and user, settings, demo data, agents
```

Both use `DATABASE_URL` (default: the docker-compose database). The
migrations live in [`migrations/`](migrations), in Drizzle's format, and are
built into the binary (`migrations.FS`), so `bin/db` runs from any directory.
`internal/migrate` records them the way Drizzle's migrator does, in
`drizzle.__drizzle_migrations`, with the same SHA-256 and time, so a
database the TS server migrated goes on with Go.

To change the schema, add the next `NNNN_name.sql` and its entry at the end of
`migrations/meta/_journal.json` (a later `when`), then `make generate`. Never
edit an applied migration. The `meta/*_snapshot.json` files are drizzle-kit's
record of the TS schema up to the last TS migration; migrations written by
hand don't update them. The seed is idempotent, like the TS one: it adds only
what isn't there, and never overwrites a setting.

## Run the API

`./scripts/dev.sh` builds it, migrates and seeds the database, and runs it
on :3001 with the web app on :3000.

It uses the same database, environment variables and secrets file
(`~/.devdigest/secrets.json`) as the TS server did. The database must be
migrated and seeded, as `./scripts/dev.sh` does.

It doesn't read `api/.env` itself: `dev.sh` copies it from
[`.env.example`](.env.example) on the first run, loads it for the Go commands
(a variable already set in the shell wins, as with dotenv) and starts the API
from `api/`, so relative paths such as `DEVDIGEST_CLONE_DIR=./clones` resolve
there. To run it by hand, on another port:

```sh
cd api && make build   # writes bin/api, bin/db and bin/review
(set -a; . ./.env; set +a; API_PORT=3002 ./bin/api)
curl localhost:3002/workspace
```

## Review a diff from the command line

```sh
cd api
export OPENROUTER_API_KEY=sk-or-...
git diff main | go run ./cmd/review \
    -model deepseek/deepseek-v4-flash \
    -prompt ../docs/agent-prompts/general-reviewer.md
```

Progress goes to standard error and the review to standard output. Useful flags:

| Flag | Meaning |
|---|---|
| `-provider openai` | Call OpenAI instead, with `OPENAI_API_KEY` |
| `-provider anthropic` | Call Anthropic instead, with `ANTHROPIC_API_KEY` |
| `-base-url URL` | Call any OpenAI-compatible API, such as Ollama at `http://localhost:11434/v1` |
| `-strategy` | `auto` (default), `single-pass` or `map-reduce` |
| `-task "Review PR #482"` | A line framing the review |
| `-diff FILE` | Read the diff from a file instead of standard input |
| `-json` | Print the result as JSON |
| `-quiet` | Don't print progress |

Run `go run ./cmd/review -h` for the full list.

## Commands

```sh
make check      # gofmt check, go vet, staticcheck, go test -race (DB tests need Docker)
make fmt        # format all files
make generate   # regenerate internal/postgres from the SQL (the first run takes minutes)
make build      # bin/api, bin/db, bin/review
```

`make lint` needs staticcheck: `go install honnef.co/go/tools/cmd/staticcheck@2026.1`.
