# api/ — DevDigest backend in Go

Go 1.26 · pgx + sqlc · tree-sitter (cgo: needs a C compiler) · Anthropic Go SDK · testcontainers for tests.

The owner is learning Go: write **idiomatic Go**, never TypeScript translated line by line.
The old TS backend is at tag `ts-final` (`git show ts-final:server/src/app.ts`). Use it
for *behavior* (a route's edge case, a JSON field), never as a guide to structure. A TS bug
is fixed in Go with a test and listed in `docs/deviations-from-ts.md`.

## Commands (from api/)
- `make check`: gofmt, vet, staticcheck, `go test -race`. Before every commit. DB tests need Docker.
- `make generate`: after editing `internal/postgres/queries/*.sql` or adding a migration
- `make build`: `bin/api`, `bin/db`, `bin/mcp`, `bin/review`
- `go run ./cmd/db migrate` / `seed`: uses `DATABASE_URL`

## Map
- `cmd/{api,db,mcp,review}`: wiring only; every dependency is built here
- `internal/httpapi`: routes, JSON, error envelope, middleware
- `internal/review`: the domain: prompt, grounding, structured output, `Run`
- `internal/runner`, `internal/jobs`: background reviews; background jobs
- `internal/repointel`: the indexer and a review's context
- `internal/repos`, `pulls`, `agents`, `skills`, `secrets`, `seed`, `migrate`: one job each
- `internal/mcpserver`: the MCP server's tools, a client of the HTTP API
- `internal/{openai,anthropic,claudecode,github,git}`: adapters (`claudecode` runs `claude -p`)
- `internal/postgres`: sqlc output · `migrations/`: embedded SQL
- Every package, and what it was ported from → `docs/architecture.md`

## Conventions that differ from habit
- No DI container, no package-level mutable state. Pass values in.
- Small interfaces live in the package that uses them. Accept interfaces, return structs.
- No `utils`/`helpers`/`common` packages. Flat packages, no stutter (`review.Ground`).
- Domain packages (`review`, `diff`) never import HTTP, SQL or SDK packages.
- `context.Context` first on anything doing I/O. Every goroutine has an owner and a way to stop.
- Tests: stdlib `testing`, table-driven, hand-written fakes. No assertion or mock libraries.
- Standard library first. A new dependency is justified in its commit message.
- The full TS → Go table → `docs/go-idioms.md`

## Do not touch
- `internal/postgres/*.go`: generated. Edit `queries/*.sql`, then `make generate`.
- `migrations/`: never edit an applied migration. Add `NNNN_name.sql` + its `_journal.json` entry.
- `internal/review/testdata/*.golden`: byte-for-byte prompts from the TS engine. Change only on purpose.
- `internal/repointel/testdata/*.golden.json`: what the TS parser found. Same rule.
- `internal/seed` prompts are copies of `../docs/agent-prompts`. A test keeps them equal; change both.
- JSON shapes of existing routes: `client/src/vendor/shared/contracts` must still parse them.
- `clones/`: runtime data with a stale CLAUDE.md inside. Never read or edit it as project code.

## Read when needed
- Adding or changing a route → `specs/http-api.md`
- Review engine, prompt, grounding, runner, live log → `specs/review-run.md`
- Indexer, repo map, callers → `specs/repo-intel.md`
- Adding, cloning, refreshing repos; background jobs → `specs/repos-jobs.md`
- API keys, secrets file, test-connection → `specs/secrets.md`
- The MCP server, its tools and token budget → `specs/mcp.md`
- "Why does Go differ from TS here?" → `docs/deviations-from-ts.md`
- How the port was done and verified → `docs/history.md`
- A strange DB, cgo, staticcheck or runner failure → `INSIGHTS.md` first
