# DevDigest — local-first AI pull-request review (course starter)

Three parts, each with its own toolchain and its own CLAUDE.md (no monorepo workspace):

| Folder | What | Port |
|---|---|---|
| `api/` | Go API: Postgres, repo-intel, the review engine | 3001 |
| `client/` | Next.js 15 studio | 3000 |
| `e2e/` | Deterministic browser flows (agent-browser) | — |

Only Postgres (pgvector) runs in Docker; the API and the web app run on the host.

## Stack
Go 1.26 (cgo for tree-sitter) · Postgres 16 + pgvector · Next.js 15, React 19 · Node ≥ 22, pnpm ≥ 10

## Commands
- `./scripts/dev.sh [--no-seed|--no-client|--db-only]`: the whole stack; API settings in `api/.env`
- `./scripts/e2e.sh`: e2e flows on an isolated, freshly seeded stack (alternate ports)

## Cross-part rules
- The client talks to the API only over HTTP. The JSON shapes are the Zod contracts in
  `client/src/vendor/shared/contracts`; a change to one side updates the other in the same change.
- CI is one path-filtered workflow per part (`api.yml`, `client.yml`, `e2e-web.yml`). A file a part
  depends on outside its folder goes in that workflow's `paths:`.
- The TypeScript backend is gone (tag `ts-final`). Don't recreate `server/` or `reviewer-core/`.

## Gotchas
- Postgres listens on host port **5433**, not 5432.
- NEVER `docker compose down -v`: it deletes the dev volume with every imported repo and review.
- The API doesn't migrate on boot: `cd api && go run ./cmd/db migrate`.
- `api/clones/` is runtime data (git-ignored). It holds full clones, including an old copy of
  this repo with a stale CLAUDE.md. Never read or edit it as project code.

## Insights loop
- Before the first change in a task, read the `INSIGHTS.md` of each part you'll touch (the root
  one too for env, scripts or CI). Trust its entries unless the code says otherwise. Then name the
  3 entries most relevant to the task, one line each, or say "no relevant insights".
- At the end of a task that hit a failure, a wrong turn or a decision, run `/engineering-insights`.
  Don't skip it: it's how the next session avoids the same trap.

## Read when needed
- End-to-end behavior of the product → `specs/review-flow.md`
- Architecture overview → `README.md`; testing strategy → `TESTING.md`
- Building a feature spec-first (agents, `specs/`, `plans/`, `/run-plan`) → `.claude/agents/README.md`
- Something broke in the environment (Docker, ports, toolchain, CI) → `INSIGHTS.md`
