# Insights — repo-wide

Lessons learned the hard way: environment, Docker, ports, toolchain, CI.
Package-specific ones live in each part's own `INSIGHTS.md` (`api/INSIGHTS.md`, …).

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" in the matching `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · short title
Symptom: what you saw · Cause: why · Rule: what to do · Promoted: no
```

---

## 2026-09-30 · e2e.sh brings the stack up, then every flow fails
Symptom: `./scripts/e2e.sh` starts Postgres, the API and the web app, then the flows fail at once.
Cause: the flows shell out to the `agent-browser` CLI, which isn't installed.
Rule: `npm i -g agent-browser && agent-browser install` once per machine.
Promoted: no

## 2026-09-30 · e2e flows fail against the dev database
Symptom: flows 02/04/05 land on the wrong repository.
Cause: flow 02 follows the home redirect to the *first* repo; the dev DB has other imported repos.
Rule: run e2e only through `scripts/e2e.sh` (fresh, isolated DB), never against the dev stack.
Promoted: yes (e2e/README.md)

## 2026-09-30 · Postgres port clash
Symptom: `docker compose up` fails, or the API connects to the wrong database.
Cause: a native Postgres holds 5432; the compose file publishes 5433 to avoid it.
Rule: if 5433 is taken too, change it in `docker-compose.yml` **and** `DATABASE_URL` in `api/.env`.
Promoted: yes (root CLAUDE.md)
