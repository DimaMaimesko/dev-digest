# api/ — DevDigest backend in Go

This module is a Go rewrite of `server/` (Fastify) and `reviewer-core/`. The
Next.js client in `client/` stays as it is, so the Go API must serve the same
HTTP routes and the same JSON shapes.

The owner is learning Go. The code must be **idiomatic Go**, not TypeScript
translated line by line ("don't write Java in Go").

## Porting rule: port behavior, not structure

The TypeScript code is the reference for *what* the system does: routes, JSON
shapes, business rules. Its tests are the spec. It is not a guide to *how* Go
code should be organized.

- Before porting a file, read its TS tests and every caller.
- If the TS code has a bug, fix it in Go, add a test for it, and list it under
  "Deviations from the TS code" in `README.md`.
- If a TS pattern has no Go equivalent, don't imitate it. Write it the Go way
  and explain the difference in the commit message.
- Order the moves by shared state. While both servers run (the Go server
  forwards unported routes to TS), a route that writes state the TS server
  caches in memory must move only after every TS route that reads that cache.
  Example: `POST /settings/test-connection` writes `secrets.json`, which TS
  reads once and caches, so it stays on TS until the reviews move.

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
  internal/httpapi  routes, JSON, middleware (CORS, security headers, logs)
  internal/postgres sqlc-generated queries; edit queries/*.sql, then `make generate`
  internal/pgtest   throwaway migrated Postgres for tests
  internal/secrets  API keys: ~/.devdigest/secrets.json, then the environment
  internal/agents   creating and changing agents, with their version history
  internal/github   client for GitHub's REST API (net/http)
  internal/pulls    saving pull requests from GitHub
  internal/diff     parse unified diffs; which new-file lines a hunk shows
  internal/review   domain: findings, grounding, prompt, LLM interface, structured output, Run
  internal/openai   adapter: OpenAI-compatible chat completions (OpenAI, OpenRouter, Ollama)
```

## Commands

Run from `api/`:

- `make check`: gofmt check, `go vet`, `staticcheck`, `go test -race`. Run it before every commit. Database tests need Docker running.
- `make generate`: regenerate `internal/postgres` after changing SQL.
- `PARITY_TS_URL=http://localhost:3001 go test ./internal/httpapi -run Parity -v`: compare with the running TS server.
- `make fmt`: format everything.

## Status

The migration plan has 6 phases. Update this list as phases finish.

1. ✅ Port `reviewer-core` into `internal/review`, with an OpenAI-compatible adapter and the `cmd/review` CLI
2. ✅ API skeleton and the database-backed read endpoints (18 of 22 `GET` routes; the other 4 need GitHub, the LLM adapters or the run bus). Add each new route to the walk in `parity_test.go`.
3. **In progress:** write paths. ✅ Fallback proxy (`TS_API_URL`): unported routes are forwarded to the TS server, so the web app can run on the Go server. ✅ `PUT /settings`. ✅ Agent writes (`internal/agents`). ✅ Accept and dismiss findings, delete reviews and runs. ✅ GitHub sync on pull request reads, `POST /repos/{id}/poll` (`internal/github`, `internal/pulls`). ✅ PR comments, read and post. Next: the model lists (an Anthropic adapter). `POST /settings/test-connection` waits for phase 4 (see "Order the moves by shared state").
4. Reviews: run executor, background runs, SSE
5. repo-intel (tree-sitter)
6. Remove the TS server; migrations and CI move to Go
