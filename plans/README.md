# Plans

Implementation plans: the **how** for one feature-spec (see `../specs/README.md`), or for a small
change clear enough to plan directly. Written by the `implementation-planner` agent, executed by
`/run-plan plan:plans/<slug>.md`, checked by `plan-verifier`.

- **Name:** the spec's slug (`specs/run-cost.md` → `plans/run-cost.md`); kebab-case otherwise.
- **Status:** `draft` → `approved` (by you, after reading it) → `done`.
- **Bar:** short enough to read before coding, concrete enough to execute without design decisions.

## Shape

```md
# <Feature> — Implementation Plan
Status: draft

## Source requirements          ← spec path + the AC-IDs covered
## Clarifications & recommendations
## Execution mode               ← single | multi, recommended and confirmed
## Modules affected             ← api / client / e2e, and what's out of scope
## Architectural constraints    ← cited from CLAUDE.md / INSIGHTS.md
## Approach                     ← design, existing code by file:line
## Tasks
### T1 — <title>
- Action:
- Module:          api | client | e2e
- Type:            migration | query | domain | route | contract | hook | component | flow | test
- Skills to use:   exact names from .claude/skills/, or "none (api/CLAUDE.md)"
- Owned paths:     files only this task edits; parallel tasks never share one
- Depends-on:      T-ids, or —
- Known gotchas:   cited INSIGHTS.md / CLAUDE.md entries
- Acceptance:      AC-IDs and how the task proves them
## Verification                 ← commands per part; manual checks listed separately
```

`/run-plan` reads the `## Tasks` fields to build its dependency graph, so keep the field names
exactly as above.

## Verification commands

| Part | Scoped | Full |
|---|---|---|
| `api/` | `gofmt -l .` · `go build ./...` · `go vet ./...` · `go test -race ./internal/<pkg>/...` | `make check` (DB tests need Docker) |
| `client/` | `pnpm typecheck` · `pnpm exec vitest run <paths>` | `pnpm typecheck && pnpm test` |
| `e2e/` | — | `./scripts/e2e.sh` |
