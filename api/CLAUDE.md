# api/ — DevDigest backend in Go

This module is the DevDigest backend, a Go rewrite of the TypeScript
`server/` (Fastify) and `reviewer-core/`. Those were removed once everything
was ported; they live on at commit `e9e4574` (tag `ts-final`), e.g.
`git show ts-final:server/src/app.ts`. The Next.js client in `client/` was
built against them, so the API keeps the same HTTP routes and JSON shapes.

The owner is learning Go. The code must be **idiomatic Go**, not TypeScript
translated line by line ("don't write Java in Go").

## The TS code as a reference: behavior, not structure

When a question is "what did the old server do?" (a route's edge case, a JSON
field, a business rule), `ts-final` and its tests answer it. They are never a
guide to *how* Go code should be organized.

- A TS bug is fixed in Go, with a test, and listed under "Deviations from the
  TS code" in `README.md`.
- A TS pattern with no Go equivalent is not imitated. Write it the Go way.

## Go rules

| Don't carry over from TS | Do this in Go |
|---|---|
| DI container (`platform/container.ts`) | Build every dependency in `cmd/*/main.go` and pass it into constructors |
| One central interfaces file (`vendor/shared/adapters.ts`) | Define small interfaces in the package that *uses* them. Accept interfaces, return structs. |
| Service classes that can reach everything | A struct holds only the dependencies it uses. Use a plain function when there's no state. |
| `throw` and error class hierarchies | Return errors. Wrap with `fmt.Errorf("doing x: %w", err)`, check with `errors.Is` / `errors.As` |
| Cancellation flags and `checkCancelled()` callbacks | `context.Context` as the first parameter of anything that does I/O or can take long |
| `helpers.ts`, `constants.ts`, `_shared/`, `utils` | No utils, helpers or common packages. Put code next to what uses it. |
| Deep folders and `index.ts` barrel files | Flat packages named for what they provide. Avoid stutter: `review.Ground`, not `review.GroundReviewFindings` |
| Global singletons (`export const runBus`) | No package-level mutable state. Pass values in. |
| Fire-and-forget `void promise` | Every goroutine has an owner, a `context`, and a way to stop |
| `mocks.ts` / mock frameworks | Small hand-written fakes in `_test.go` files |
| Zod runtime schemas | Plain structs; validate where input enters the system |
| Drizzle ORM | SQL with `pgx` + `sqlc` |
| `undefined` / optional everywhere | Useful zero values. Use a pointer only when "absent" differs from "zero". |

Clean architecture, kept light: follow the **dependency rule** (domain packages
such as `internal/review` never import HTTP, SQL or SDK packages). Skip the
ceremony: no package per layer, no interface for every struct, no mapping
between identical structs. Add structure when it's needed, not in advance.

Other rules:
- Standard library first. Add a dependency only when it saves real work, and
  say why in the commit.
- Tests: standard `testing`, table-driven with `t.Run`, external `_test`
  packages for public APIs. No assertion libraries.
- Doc comments on every exported identifier, written as full sentences.

## Layout

```
api/
  cmd/api           HTTP API server
  cmd/review        command-line review of a diff
  cmd/db            migrate and seed the database
  migrations        the SQL migrations in Drizzle's format (journal + .sql), embedded; sqlc reads them too
  internal/httpapi  routes, JSON, middleware (CORS, security headers, logs)
  internal/postgres sqlc-generated queries; edit queries/*.sql, then `make generate`
  internal/pgtest   throwaway migrated Postgres for tests
  internal/migrate  applies the migrations (Drizzle-compatible bookkeeping)
  internal/seed     the starting data
  internal/secrets  API keys: ~/.devdigest/secrets.json, then the environment
  internal/agents   creating and changing agents, with their version history
  internal/github   client for GitHub's REST API (net/http)
  internal/pulls    saving pull requests from GitHub
  internal/git      running git in a clone (the review diff)
  internal/repointel repo-intel: the indexer, and a review's context from it (tree-sitter)
  internal/runner   runs reviews in the background; live logs; cancel
  internal/repos    adding, cloning, refreshing, removing repositories
  internal/jobs     background jobs, 3 at a time, recorded in the jobs table
  internal/diff     parse unified diffs; which new-file lines a hunk shows
  internal/review   domain: findings, grounding, prompt, LLM interface, structured output, Run
  internal/openai   adapter: OpenAI-compatible chat completions (OpenAI, OpenRouter, Ollama)
  internal/anthropic adapter: Anthropic's API, through the official SDK
```

## Commands

Run from `api/`:

- `make check`: gofmt check, `go vet`, `staticcheck`, `go test -race`. Run it before every commit. Database tests need Docker running. Building needs cgo (tree-sitter) and a C compiler.
- `make generate`: regenerate `internal/postgres` after changing SQL.
- `make fmt`: format everything.
- `go run ./cmd/db migrate` and `go run ./cmd/db seed`: prepare a database (DATABASE_URL).

## Status

The migration from TS is finished: all 6 phases are done.

1. ✅ Port `reviewer-core` into `internal/review`, with an OpenAI-compatible adapter and the `cmd/review` CLI
2. ✅ API skeleton and the database-backed read endpoints (18 of 22 `GET` routes; the other 4 need GitHub, the LLM adapters or the run bus), each checked by a parity test against the running TS server.
3. ✅ Write paths. ✅ Fallback proxy (`TS_API_URL`): unported routes are forwarded to the TS server, so the web app can run on the Go server. ✅ `PUT /settings`. ✅ Agent writes (`internal/agents`). ✅ Accept and dismiss findings, delete reviews and runs. ✅ GitHub sync on pull request reads, `POST /repos/{id}/poll` (`internal/github`, `internal/pulls`). ✅ PR comments, read and post. ✅ Model lists (`internal/anthropic`). The repository routes and `POST /settings/test-connection` move in phase 5.
4. ✅ Reviews: inputs (`internal/git`, `internal/repointel` with the tree-sitter callers), Anthropic for reviews, the run executor (`internal/runner`), and the review, events and cancel routes. Prompts checked byte-for-byte against TS with fake models. `POST /settings/test-connection` moves with phase 5: it writes `secrets.json`, which the TS server read once and cached, so it moved after every route reading that cache.
5. ✅ Repositories: clone and fetch, job runner, `POST /repos`, refresh, delete (`internal/repos`, `internal/jobs`); the repo-intel indexer and `POST /repos/{id}/resync` (`repointel.Indexer`; checked against the TS index table by table); `POST /settings/test-connection`. All 40 routes are ported.
6. ✅ Migrations and seed in Go (`internal/migrate`, compatible with Drizzle's bookkeeping; `internal/seed`; `cmd/db`), with the migrations in `api/migrations` (embedded). `scripts/dev.sh`, `scripts/e2e.sh` and the `e2e web` workflow run the Go server, with its settings in `api/.env`. `server/`, `reviewer-core/`, their workflows, the fallback proxy (`TS_API_URL`) and the parity test are removed; `ts-final` keeps them.
