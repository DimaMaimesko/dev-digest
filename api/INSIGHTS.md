# Insights — api/

Lessons learned in the Go API. Repo-wide ones (Docker, ports, CI) are in `../INSIGHTS.md`.

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" or "Do not touch" in `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · short title
Symptom: … · Cause: … · Rule: … · Promoted: no
```

---

## 2026-09-30 · `make check` passes, but the database tests never ran
Symptom: green `make check` locally, red in CI.
Cause: tests that need Postgres (`internal/pgtest`) **skip** when Docker isn't running.
Rule: start Docker before `make check`; look for `SKIP` in `go test -v` output when in doubt.
Promoted: no

## 2026-09-30 · staticcheck can't read the export data
Symptom: `make lint` fails with export-data errors after a Go toolchain upgrade.
Cause: staticcheck 2026.1 (the version the Makefile and `api.yml` pin) can't read Go 1.27's
export data (found during `d513c34`; v0.8.1 worked).
Rule: when the local Go is newer than `go.mod`, match staticcheck to it; keep CI's pin in step.
Promoted: no

## 2026-09-30 · The API won't start by hand
Symptom: `./bin/api` exits with "no default workspace" or connects to the wrong database.
Cause: the API doesn't read `api/.env` itself (only `dev.sh` loads it), and it refuses to start
on an unseeded database.
Rule: `(set -a; . ./.env; set +a; ./bin/api)` after `go run ./cmd/db migrate && go run ./cmd/db seed`.
Promoted: no

## 2026-09-30 · A GitHub token saved inside an old clone
Symptom: a token shows up in `clones/<owner>/<name>/.git/config`.
Cause: clones made by the TS server stored the token in the remote URL. Go sends it as a
header per git command and never saves it.
Rule: in that clone, `git remote set-url origin https://github.com/<owner>/<name>.git`.
Promoted: no

## 2026-09-30 · The first `make generate` takes minutes
Symptom: `make generate` seems to hang.
Cause: the first run compiles sqlc's Postgres parser.
Rule: wait; later runs are fast.
Promoted: no
