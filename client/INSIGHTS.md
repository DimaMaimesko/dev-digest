# Insights — client/

Lessons learned in the web app. Repo-wide ones (Docker, ports, CI) are in `../INSIGHTS.md`.

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" or "Do not touch" in `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · <kind> · short title
Symptom: … · Cause: … · Rule: … · Evidence: file:line · Promoted: no
```
`<kind>`: mistake · pattern · decision · context. Add entries with `/engineering-insights`
(`.claude/skills/engineering-insights/`): it has the rules and a format checker.

---

## 2026-09-30 · mistake · Importing a value from `@devdigest/shared` breaks the build
Symptom: `next build` / `pnpm dev` fails to resolve `./contracts/*.js`.
Cause: `vendor/shared` is written for Node ESM (`.js` import suffixes). Types are erased, but a
runtime value pulls `index.ts` and its re-exports into webpack, which can't resolve them.
Rule: `import type` only. Need a runtime value? Mirror it in `src/lib/` (see `feature-models.ts`).
Promoted: yes (CLAUDE.md "Do not touch")

## 2026-09-30 · context · A test that passes without the API isn't testing fetch
Symptom: component tests pass while the real screen breaks on a changed response shape.
Cause: tests mock the hook modules, and responses are never parsed at runtime (Zod types only).
Rule: a change to a response shape needs the e2e flows (`../scripts/e2e.sh`), not just `pnpm test`.
Promoted: no

## 2026-09-30 · mistake · `NEXT_PUBLIC_API_BASE` changes don't show up
Symptom: the app keeps calling the old API URL after editing `.env`.
Cause: `NEXT_PUBLIC_*` is inlined at build/dev-start time.
Rule: restart `pnpm dev` (or rebuild) after changing it. CI builds with it set (`e2e-web.yml`).
Promoted: no
