# Insights — e2e/

Lessons learned in the browser suite. Repo-wide ones (Docker, ports, CI) are in `../INSIGHTS.md`.

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" in `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · <kind> · short title
Symptom: … · Cause: … · Rule: … · Evidence: file:line · Promoted: no
```
`<kind>`: mistake · pattern · decision · context. Add entries with `/engineering-insights`
(`.claude/skills/engineering-insights/`): it has the rules and a format checker.

---

## 2026-09-30 · mistake · Flows 02/04/05 land on the wrong repository
Symptom: the PR row "Add rate limiting…" never appears.
Cause: `/` redirects to the *first* repo; a dev DB with other imported repos lands elsewhere.
Rule: run through `../scripts/e2e.sh`, which starts from an empty, freshly seeded Postgres.
Promoted: yes (CLAUDE.md)

## 2026-09-30 · mistake · The stack starts, then every flow fails at once
Symptom: `e2e.sh` boots Postgres, the API and the web app; every step fails immediately.
Cause: the `agent-browser` CLI (or its Chrome) isn't installed.
Rule: `npm i -g agent-browser && agent-browser install`.
Promoted: yes (CLAUDE.md "Commands")

## 2026-09-30 · context · A flow breaks after a change outside e2e/
Symptom: red `e2e web` on a PR that only touched `client/` or `api/`.
Cause: flows wait for visible text from the seed and the client's copy (see `docs/writing-flows.md`).
Rule: the workflow runs on `client/**` and `api/**` for this reason. Fix the flow or the text,
not the timeout.
Promoted: no
