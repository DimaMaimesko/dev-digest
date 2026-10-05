# Skills

Reusable AI skills that provide specialized knowledge and workflows. Canonical location is `.claude/skills/` with a symlink at `.cursor/skills/ → ../.claude/skills` for Cursor compatibility. Shared with the team via version control.

## Catalog

| Skill | Scope | Description |
|-------|-------|-------------|
| [fastify-best-practices](fastify-best-practices/SKILL.md) | Legacy | Fastify routes, plugins — the TypeScript backend is gone; not used |
| [drizzle-orm-patterns](drizzle-orm-patterns/SKILL.md) | Legacy | Drizzle ORM — the API uses pgx + sqlc now; not used |
| [go-api-skeleton](go-api-skeleton/SKILL.md) | Backend | New Go APIs in the greenlight layout — not this repo's layout; don't use for `api/` |
| [postgresql-table-design](postgresql-table-design/SKILL.md) | Backend | Postgres schema design, data types, indexing, constraints |
| [next-best-practices](next-best-practices/SKILL.md) | Frontend | Next.js App Router, RSC boundaries, data fetching, optimization |
| [react-best-practices](react-best-practices/SKILL.md) | Frontend | React anti-patterns, state management, hooks rules |
| [react-testing-library](react-testing-library/SKILL.md) | Frontend | General-purpose React Testing Library guide with Vitest |
| [zod](zod/SKILL.md) | Full-stack | Zod schema validation, parsing, error handling, type inference |
| [typescript-expert](typescript-expert/SKILL.md) | Full-stack | Type-level programming, performance, tooling, migrations |
| [security](security/SKILL.md) | Full-stack | OWASP Top 10:2025, auth, injection, uploads, secrets |
| [mermaid-diagram](mermaid-diagram/SKILL.md) | Shared | Mermaid diagrams in markdown (flowcharts, sequence, ERD, …) |
| [onboard](onboard/SKILL.md) | Project | New-team-member onboarding: goal, stack, architecture, module connections, diagrams |
| [engineering-insights](engineering-insights/SKILL.md) | Project | End-of-task capture of non-obvious lessons into each part's `INSIGHTS.md`; format checker |
| [run-plan](run-plan/SKILL.md) | Project | Runs an approved `plans/<slug>.md`: implementers by task DAG, review gate, bounded fix loop |

## Routing by path

Which skills to load before touching a file. `implementation-planner` copies this into each plan
task's "Skills to use"; `implementer` and `test-writer` fall back to it for anything a plan misses.

| Path | Skills | Notes |
|---|---|---|
| `api/**/*.go` | none | No Go skill fits: the conventions are `api/CLAUDE.md` and `api/docs/go-idioms.md` |
| `api/migrations/*.sql`, `api/internal/postgres/queries/*.sql` | `postgresql-table-design` | Then `make generate`; never hand-edit `internal/postgres/*.go` |
| `api/internal/httpapi/**`, `api/internal/secrets/**`, anything reading PR text into a prompt | `security` | Input validation, secrets, untrusted text |
| `client/src/app/**` (pages, layouts, route handlers) | `next-best-practices`, `react-best-practices` | |
| `client/src/**/*.tsx` components, `client/src/lib/hooks/**` | `react-best-practices` (+ `typescript-expert` for tricky types) | |
| `client/src/**/*.test.tsx` | `react-testing-library` | |
| `client/src/vendor/shared/contracts/**` | `zod` | Must still parse the API's JSON |
| `e2e/**` | none | `e2e/docs/writing-flows.md` |
| Diagrams in any Markdown | `mermaid-diagram` | |

## What Are Skills?

Skills are modular packages that extend the AI agent with specialized knowledge and workflows. Unlike rules (always applied) or agents (invoked for specific tasks), skills are loaded on-demand when the agent determines they're relevant.

### Skills vs Rules vs Commands vs Agents

| Type | Scope | Loaded | Purpose |
|------|-------|--------|---------|
| **Rules** (`.mdc`) | Project conventions | Always or by file pattern | Persistent guardrails |
| **Commands** (`.md`) | User actions | On `/command` invocation | Slash commands |
| **Skills** (`.md`) | Domain knowledge | On-demand by agent | Specialized knowledge |
| **Agents** (`.md`) | Workflows | Via Task tool | Subagent orchestration |

## Creating New Skills

Each skill has:

- `SKILL.md` — Main skill file with rules and conventions (required)
- `examples.md` — Code examples showing good/bad patterns (recommended)
- `references.md` — Sources and rationale (optional)
