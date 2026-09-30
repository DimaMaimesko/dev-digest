# engineering-insights: examples

## Vague → useful

**Bad:** too generic to act on.
```
## 2026-10-01 · mistake · Be careful with tests
Symptom: tests were flaky
Cause: environment
Rule: make sure the environment is set up
Promoted: no
```

**Good:** names the mechanism, the check and the fix.
```
## 2026-09-30 · mistake · `make check` passes, but the database tests never ran
Symptom: green `make check` locally, red in CI.
Cause: tests that need Postgres (`internal/pgtest`) **skip** when Docker isn't running.
Rule: start Docker before `make check`; look for `SKIP` in `go test -v` output when in doubt.
Evidence: api/internal/pgtest
Promoted: no
```

---

**Bad:** restates what `client/CLAUDE.md` already says.
```
## 2026-10-01 · pattern · Use hooks for data
Cause: that's the convention
Rule: components call hooks from `@/lib/hooks`, not fetch
Promoted: no
```
Drop it. It's already under "Conventions" in `client/CLAUDE.md`.

---

**Bad:** a replay of the session, not a conclusion.
```
## 2026-10-01 · mistake · Getting the API to run
Symptom: first I ran ./bin/api, it complained, then I exported DATABASE_URL by hand,
then it said no workspace, then we looked at dev.sh…
```

**Good:** the conclusion only.
```
## 2026-09-30 · mistake · The API won't start by hand
Symptom: `./bin/api` exits with "no default workspace" or connects to the wrong database.
Cause: the API doesn't read `api/.env` itself (only `dev.sh` loads it), and it refuses to start
on an unseeded database.
Rule: `(set -a; . ./.env; set +a; ./bin/api)` after `go run ./cmd/db migrate && go run ./cmd/db seed`.
Promoted: no
```

---

## A decision

```
## 2026-10-01 · decision · Contracts stay types-only on the client
Cause: a runtime import from `@devdigest/shared` pulls `.js`-suffixed ESM into webpack and
breaks the build; parsing responses at runtime was rejected as not worth a vendored copy.
Rule: `import type` only; mirror a needed runtime value in `src/lib/` (see `feature-models.ts`).
Evidence: client/src/lib/feature-models.ts
Promoted: no
```

## A correction (the old entry stays, one line added)

```
## 2026-09-30 · mistake · staticcheck can't read the export data
…
Promoted: no
Superseded: 2026-11-02 · staticcheck pin moved to v0.9 in api.yml
```

## Promotion after the second bite

The same trap shows up again, and `api/INSIGHTS.md` already has it with
`Promoted: no`. Don't add a second entry. Propose to the user:

> Add to `api/CLAUDE.md` → "Commands": "DB tests **skip** without Docker; start it before `make check`."

After they agree: `Promoted: yes (api/CLAUDE.md "Commands")`.
