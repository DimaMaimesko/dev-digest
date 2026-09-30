# Testing & CI strategy

DevDigest is three independent parts (no workspace): the Go API, the web app
and the browser e2e. Testing is organised as **one suite per part**, each with
its own CI workflow, runner, and path filter. A suite runs only when its part
(or a file it depends on) changes.

## Philosophy — typological, not exhaustive

We do **not** chase line coverage. Each suite covers the *kinds* of things that
can break in that layer — one happy path plus the edge that actually matters per
workflow — and deliberately skips the rest. Concretely:

- **Test behaviour at the seams**, not implementation details. Routes, adapters,
  contracts, the review pipeline, the rendered component.
- **Fake the outside world.** LLMs and GitHub are replaced by small
  hand-written fakes (in-process `httptest` servers or structs implementing the
  interface) in the `_test.go` files, so tests are hermetic and key-free.
- **One real integration per data-backed workflow**, against a real Postgres —
  not a mock DB — because the bugs there live in SQL, migrations, and wiring.
- **A few end-to-end browser flows** over the *main* user journeys, on seeded
  data, with no LLM in the loop.

If a test wouldn't catch a class of regression we care about, we don't write it.

## Suite map

| Suite | Package | Kind | Runner | Workflow | Docker? |
|-------|---------|------|--------|----------|---------|
| api | `api/` | unit + integration (real Postgres) | `go test -race` | `api.yml` | **yes** |
| client | `client/` | component / unit (jsdom) | vitest | `client.yml` | no |
| e2e web | `e2e/` | browser e2e (deterministic) | agent-browser + `run.ts` | `e2e-web.yml` | yes (stack) |

## What each suite covers

**client** — components render and react to interaction (React Testing Library
+ jsdom). The data hooks are mocked (`vi.mock`); no API, DB, or browser. Covers the PR-review
surface (list, diff, findings, run controls) and the agent editor.

**api** — `make check`: gofmt, `go vet`, staticcheck and `go test -race`.
Domain packages (`internal/review`, `internal/diff`, `internal/repointel`, …)
are tested without I/O: prompt assembly, grounding, diff parsing, the repo-intel
index against golden files. Database-backed tests (routes in
`internal/httpapi`, the runner, jobs, migrations, seed) get a throwaway
migrated Postgres from `internal/pgtest`: one testcontainers pgvector container
per test binary, one database per test. They skip when Docker isn't running.

**e2e web** — see `e2e/README.md`. Deterministic agent-browser flows over the
main journeys (boot → PR list → PR detail; agents) against a real seeded stack.
No `chat`, no model key.

## Running locally

```sh
# per package
cd api    && make check          # needs Docker for the database tests
cd client && pnpm test           # + pnpm typecheck

# browser e2e (needs the full stack + agent-browser CLI)
./scripts/dev.sh
npm i -g agent-browser && agent-browser install
cd e2e && npm install && npm test
```

## Conventions

- **Go tests are table-driven** (`t.Run`), with the standard `testing`
  package only: no assertion or mock libraries.
- **Hermetic by default.** Models and GitHub are fakes in the tests; no test
  needs a real key or network access beyond Docker.
- **E2E specs are deterministic batch JSON** (`e2e/specs/*.flow.json`) using
  only `--url` / `--text` / `find` locators — never the AI `chat` command.
- **CI is path-filtered per part.** Files a suite depends on outside its
  folder are in its workflow's `paths:` (e.g. `docs/agent-prompts/**` triggers
  `api`, because a test keeps the seed's embedded prompts equal to them).
- **`api/clones/`** (with dev.sh's default `DEVDIGEST_CLONE_DIR=./clones`) **is
  runtime data**, git-ignored and never read by any suite.
