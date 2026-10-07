# Insights — api/

Lessons learned in the Go API. Repo-wide ones (Docker, ports, CI) are in `../INSIGHTS.md`.

Format: newest first. When a rule bites twice, promote it to one line under
"Gotchas" or "Do not touch" in `CLAUDE.md` and mark it `Promoted: yes`.

```
## YYYY-MM-DD · <kind> · short title
Symptom: … · Cause: … · Rule: … · Evidence: file:line · Promoted: no
```
`<kind>`: mistake · pattern · decision · context. Add entries with `/engineering-insights`
(`.claude/skills/engineering-insights/`): it has the rules and a format checker.

---

## 2026-10-08 · context · PR files, commits and comments are one GitHub page (max 100)
Cause: the GitHub client fetches `/pulls/{n}/files|commits|comments?per_page=100` once, no paging
Rule: never treat the served file list as complete; `files_count` can exceed it. Paging is an api/ change
Evidence: api/internal/github/github.go:196, :222, :288; Smart Diff header uses the listed count
Promoted: no

## 2026-10-06 · context · `claude -p` flags that silently break the claudecode adapter
Symptom: a run on the Pro subscription fails or ignores the schema, or each call costs ~20k input tokens.
Cause: `--json-schema` answers through an extra tool turn; `--bare` reads only ANTHROPIC_API_KEY, never the OAuth login; the CLI's default system prompt is ~20k tokens.
Rule: never add `--max-turns 1` or `--bare` to `claudecode`; always pass `--system-prompt`. Read the answer from `structured_output`; errors are `is_error: true` + `result`.
Evidence: api/internal/claudecode/claudecode.go CompleteJSON; check by hand with `env -u ANTHROPIC_API_KEY claude -p --output-format json …`
Promoted: no

## 2026-10-06 · decision · Claude Code reroutes the `anthropic` provider instead of being a fourth provider
Cause: a new provider value would change the Zod `Provider` enum, settings, secrets status and the client's provider lists; this is a one-person, local-only mode.
Rule: keep it behind `ANTHROPIC_VIA_CLAUDE_CODE=true` in cmd/api `reviewModel` and `ModelAPIs.ClaudeCode`; add a real provider only if both modes must coexist.
Evidence: api/cmd/api/main.go reviewModel; api/internal/httpapi/models.go fetchModels
Promoted: no

## 2026-10-05 · mistake · A retry loop must report a cancel as ctx.Err(), not the last HTTP error
Symptom: TestCompleteJSONStopsWhenCancelled failed only in a full `go test -race ./...`: "API returned 500… want context.Canceled".
Cause: a cancel landing after the 500 arrived made `retryable(ctx, err)` say "stop", so `send` returned the 500.
Rule: in an HTTP retry loop, check `ctx.Err()` right after a failed attempt and return it; keep ctx out of `retryable`.
Evidence: internal/openai/openai.go `send`, internal/github/github.go `send`; repro: `go test -race -run <Test> -count=3000 -cpu 1,2,8`
Promoted: no

## 2026-10-05 · pattern · Smoke-testing `cmd/mcp` by hand over stdio
Symptom: piping an `initialize` message into `go -C api run ./cmd/mcp` prints nothing.
Cause: stdin closes at once, so the stdio transport ends before the server answers.
Rule: keep stdin open: `{ printf '%s\n' '<json-rpc>' …; sleep 6; } | ./mcp`; send `notifications/initialized` before `tools/call`.
Evidence: api/cmd/mcp/main.go; api/specs/mcp.md "Using it"
Promoted: no

## 2026-10-02 · context · Rows made in one transaction share `created_at`
Symptom: "oldest first" lists (agents, skills) come back in a random but stable order after a seed; a test comparing order was flaky.
Cause: `now()` is the transaction's start time, so the seed's rows tie on `created_at` and `ORDER BY created_at, id` falls back to the random UUID.
Rule: in tests, give rows distinct `created_at` values before asserting order; don't expect seeded rows in insert order.
Evidence: api/internal/seed/seed.go (one transaction); api/internal/httpapi/skills_test.go TestListSkills
Promoted: no

## 2026-10-01 · decision · Long-lived requests end through `Server.CloseStreams`, not `BaseContext`
Symptom: Ctrl-C with a live log open waited 10s, then `api: context deadline exceeded`, exit 1.
Cause: `http.Server.Shutdown` never cancels request contexts. A `BaseContext` cancelled on shutdown was rejected: it would cut every in-flight request too.
Rule: a new streaming or long-poll handler must also stop on `s.streams` (`context.AfterFunc`, as `runEvents` does).
Evidence: api/internal/httpapi/runs.go runEvents; api/internal/httpapi/runs_test.go TestShutdownEndsLiveLogs
Promoted: no

## 2026-10-01 · mistake · sqlc makes a computed column non-null
Symptom: `sum(x)`, `x::float8`, `CAST`, `NULLIF`, a scalar subquery: all generate `float64`, never `*float64`.
Cause: sqlc infers nullability only for table columns (and LEFT JOINed ones); any expression is non-null.
Rule: return a count beside it (`count(x) AS priced_runs`) and build the nil in Go, as `ListPulls` does.
Evidence: api/internal/postgres/queries/pulls.sql (ListPulls); api/internal/httpapi/pulls.go knownCost
Promoted: no

## 2026-10-01 · context · A new migration fails `pgtest` TestNew
Symptom: `TestNew` fails right after adding a migration, though the migration is fine.
Cause: it proves every migration ran by checking the *last* migration's effect.
Rule: when adding `NNNN_*.sql`, point `pgtest_test.go` at the new migration's effect.
Evidence: api/internal/pgtest/pgtest_test.go:14
Promoted: no

## 2026-10-01 · context · A cancelled run never gets its outcome columns
Symptom: a cancelled run keeps NULL tokens/duration; a new column set in `FinishRun` stays empty for it.
Cause: `CancelRun` sets `status = 'cancelled'` at once; `FinishRun` updates only `WHERE status = 'running'`.
Rule: to record anything for a cancelled run, use a separate UPDATE without the status filter.
Evidence: api/internal/postgres/queries/runs_write.sql:16, :44; api/internal/runner/runner.go:293
Promoted: no

## 2026-09-30 · mistake · `make check` passes, but the database tests never ran
Symptom: green `make check` locally, red in CI.
Cause: tests that need Postgres (`internal/pgtest`) **skip** when Docker isn't running.
Rule: start Docker before `make check`; look for `SKIP` in `go test -v` output when in doubt.
Promoted: no

## 2026-09-30 · mistake · staticcheck can't read the export data
Symptom: `make lint` fails with export-data errors after a Go toolchain upgrade.
Cause: staticcheck 2026.1 (the version the Makefile and `api.yml` pin) can't read Go 1.27's
export data (found during `d513c34`; v0.8.1 worked).
Rule: when the local Go is newer than `go.mod`, match staticcheck to it; keep CI's pin in step.
Promoted: no

## 2026-09-30 · mistake · The API won't start by hand
Symptom: `./bin/api` exits with "no default workspace" or connects to the wrong database.
Cause: the API doesn't read `api/.env` itself (only `dev.sh` loads it), and it refuses to start
on an unseeded database.
Rule: `(set -a; . ./.env; set +a; ./bin/api)` after `go run ./cmd/db migrate && go run ./cmd/db seed`.
Promoted: no

## 2026-09-30 · context · A GitHub token saved inside an old clone
Symptom: a token shows up in `clones/<owner>/<name>/.git/config`.
Cause: clones made by the TS server stored the token in the remote URL. Go sends it as a
header per git command and never saves it.
Rule: in that clone, `git remote set-url origin https://github.com/<owner>/<name>.git`.
Promoted: no

## 2026-09-30 · context · The first `make generate` takes minutes
Symptom: `make generate` seems to hang.
Cause: the first run compiles sqlc's Postgres parser.
Rule: wait; later runs are fast.
Promoted: no
