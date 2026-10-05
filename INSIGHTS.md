# Insights — repo-wide

Lessons learned the hard way: environment, Docker, ports, toolchain, CI.
Package-specific ones live in each part's own `INSIGHTS.md` (`api/INSIGHTS.md`, …).

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" in the matching `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · <kind> · short title
Symptom: … · Cause: … · Rule: … · Evidence: file:line · Promoted: no
```
`<kind>`: mistake · pattern · decision · context. Add entries with `/engineering-insights`
(`.claude/skills/engineering-insights/`): it has the rules and a format checker.

---

## 2026-10-06 · mistake · Imported Claude agents still describe the TypeScript repo
Symptom: the SDD agents pointed at `server/`, `LEARNINGS.md`, `pnpm`/`vitest` for the API, and agents and skills that don't exist (`architecture-reviewer`, `pr-self-review`).
Cause: they were copied from the pre-Go version of the project; nothing loads or validates `.claude/agents/*.md` against this repo.
Rule: after adding or copying an agent or skill, grep it for `server/|reviewer-core|LEARNINGS|pnpm arch|pr-self-review`, and check every agent/skill it names exists in `.claude/`.
Evidence: .claude/agents/README.md
Promoted: no

## 2026-10-06 · context · DevDigest MCP tool names repeat the server name
Symptom: an agent's `tools:` allowlist named `mcp__devdigest__get_conventions`, so the agent silently got no MCP tools.
Cause: the tools are registered as `devdigest_get_conventions` etc., so the full name is `mcp__devdigest__devdigest_get_conventions`.
Rule: copy MCP tool names into `tools:` from a live session's tool list, never write them by hand.
Evidence: .claude/agents/spec-creator.md:4, api/specs/mcp.md "Tools"
Promoted: no

## 2026-09-30 · mistake · e2e.sh brings the stack up, then every flow fails
Symptom: `./scripts/e2e.sh` starts Postgres, the API and the web app, then the flows fail at once.
Cause: the flows shell out to the `agent-browser` CLI, which isn't installed.
Rule: `npm i -g agent-browser && agent-browser install` once per machine.
Promoted: no

## 2026-09-30 · mistake · e2e flows fail against the dev database
Symptom: flows 02/04/05 land on the wrong repository.
Cause: flow 02 follows the home redirect to the *first* repo; the dev DB has other imported repos.
Rule: run e2e only through `scripts/e2e.sh` (fresh, isolated DB), never against the dev stack.
Promoted: yes (e2e/README.md)

## 2026-09-30 · context · Postgres port clash
Symptom: `docker compose up` fails, or the API connects to the wrong database.
Cause: a native Postgres holds 5432; the compose file publishes 5433 to avoid it.
Rule: if 5433 is taken too, change it in `docker-compose.yml` **and** `DATABASE_URL` in `api/.env`.
Promoted: yes (root CLAUDE.md)
