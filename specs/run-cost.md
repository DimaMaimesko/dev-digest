# Spec — run cost

What a review run costs in US dollars, shown on three screens. It covers all three parts. The
review-run details this changes are in `api/specs/review-run.md` ("Run cost isn't tracked"
goes away).

Status: **implemented**; `./scripts/e2e.sh` not yet run. The screen designs (screenshots) are
still to be checked against this spec: placement follows the existing layout of each screen.

## Decisions

| Question | Decision | Why |
|---|---|---|
| Where the price comes from | **Only what the provider bills.** OpenRouter reports each call's cost (`usage.cost`); other providers (OpenAI, Anthropic, OpenAI-compatible such as Ollama) report none, so their cost is **unknown** and shows "—". | An exact number or none. No price table to maintain, no estimate shown as a bill. |
| PR list: which cost | **Sum of every run of the PR**, any status. | Answers "what did reviewing this PR cost". |
| Failed / cancelled runs | **Count what was spent**: the model calls that finished before the failure or the cancel. | Otherwise the PR sum under-reports. Today these runs even save `tokens = 0`. |
| Money type | `double precision`, column `cost_usd` | The name and type every other cost column in the schema uses (`ci_runs`, `eval_*`); display-only, no arithmetic beyond `SUM`. |

## What must hold

### Meaning of the number
- `cost_usd` is a run's cost in USD: the sum of `usage.cost` over **every model call the run
  made**, which covers per-file calls (map-reduce) and repair retries after an invalid answer.
- **`NULL` means unknown**: the provider reports no cost, or the run predates this feature, or
  no model call finished. **`0` means free** (e.g. an OpenRouter `:free` model). They render
  differently: "—" vs "$0.00".
- A call that fails (network error, timeout, cancel mid-call) has no response, so its cost
  can't be counted. The run's cost is then a lower bound. Accepted.

### Screen 1 — PR list (`/repos/{repoId}/pulls`)
- A **Cost** column sits between **Score** and **Status**.
- Value: the sum of `cost_usd` over all the PR's runs. Runs with unknown cost are left out of
  the sum; if no run has a known cost (or there are no runs) it shows "—".
- Not sortable or filterable (out of scope).

### Screen 2 — PR timeline (`RunHistory`, PR detail → Agent runs)
- Each run row shows its cost in the right-hand column, **next to the run's time**.
- A running run shows no cost. Done, failed and cancelled runs show it when known; unknown
  shows nothing, to keep the row quiet. Screen 3 shows the "—".

### Screen 3 — Run trace sidebar (`RunTraceDrawer` → Stats)
- The Stats row reads **Duration · Tokens · Findings · Cost**.
- Value: `trace.stats.cost_usd`; "—" when null or absent (traces saved before this feature).
- Failed and cancelled runs' traces now carry their real tokens and cost too, not 0.

### Formatting (one helper, all three screens)
- `null`/`undefined` → "—" · `0` → "$0.00" · `< $0.01` → 4 decimals ("$0.0042") ·
  otherwise 2 decimals ("$0.12", "$3.40").
- The exact value, unrounded, goes in the `title` tooltip.

## Data flow

```
OpenRouter response usage.cost ──► openai.Client.CompleteJSON ──► review.JSONResponse.CostUSD
                                                                      │ summed per attempt, per chunk
                                                                      ▼
                                                        review.Usage (in answer, in Result)
                                                                      │ also returned with an error
                                                                      ▼
runner.save / runner.finishFailed ──► agent_runs.cost_usd ──► GET /pulls/{id}/runs  (screen 2)
                                  │                       └─► GET /repos/{id}/pulls, SUM per PR (screen 1)
                                  └─► run_traces.trace.stats.cost_usd ──► GET /runs/{id}/trace (screen 3)
```

## API contract changes (JSON; Zod in `client/src/vendor/shared/contracts`)

| Route | Field | Zod |
|---|---|---|
| `GET /repos/{id}/pulls` (list item) | `cost_usd: number \| null` | `PrMeta.cost_usd: z.number().nullish()` (list endpoint only, like `score`) |
| `GET /pulls/{id}/runs` | `cost_usd: number \| null` | `RunSummary.cost_usd: z.number().nullable()` |
| `GET /runs/{id}/trace` | `stats.cost_usd: number \| null` | `RunStats.cost_usd: z.number().nullish()` (old traces lack it) |

All additions: nothing existing changes shape.

## Plan

Each step builds and passes its tests alone; one commit each.

### 1. Provider: read OpenRouter's cost — `api/internal/openai`, `api/internal/review/llm.go`
- `review.JSONResponse` gains `CostUSD *float64` (nil = the provider doesn't say).
- `openai.Client`: the OpenRouter client (`NewOpenRouter`) sends `"usage": {"include": true}`
  and reads `usage.cost`. OpenAI rejects unknown fields, so gate it like `session_id` is gated
  today (turn `sendSessionID` into one "is OpenRouter" switch, or add a second flag).
- **Verify first** with one real OpenRouter call that `usage.cost` comes back and in which unit
  (USD credits expected). If it's always returned now, still send `include` — harmless.
- `anthropic`: nothing; `CostUSD` stays nil.
- Tests: `openai_test.go` — OpenRouter body has `usage.include`, OpenAI's doesn't; `usage.cost`
  parses into `CostUSD`; missing `cost` → nil.

### 2. Domain: sum usage, keep it on failure — `api/internal/review`
- New `review.Usage{TokensIn, TokensOut int; CostUSD *float64}` with `Add(JSONResponse)`:
  tokens add; cost adds when the response has one; stays nil only if no response had one.
- Embed `Usage` in `answer` (`structured.go`) and `Result` (`run.go`), replacing their
  `TokensIn`/`TokensOut` fields; promotion keeps `res.TokensIn` compiling everywhere.
- **Usage survives errors** (the `io.Reader` convention: a value *and* an error, documented):
  `askForReview` returns what its attempts spent even when it fails (`ErrInvalidReview`, or an
  LLM error on a retry); `Run` returns `Result{Usage: spent}` with its error, including
  `ctx` cancellation between chunks.
- Tests: cost summed over retries and over map-reduce chunks; nil when no response had cost;
  `Run` failing on chunk 2 returns chunk 1's usage.

### 3. Storage — `api/migrations`, `api/internal/postgres`
- `0010_run_cost.sql`: `ALTER TABLE agent_runs ADD COLUMN cost_usd double precision;` and
  `CREATE INDEX agent_runs_pr_id_idx ON agent_runs (pr_id);` (the PR list now aggregates runs
  per PR and `ListRuns` filters by `pr_id`; there's no index today). Add its `_journal.json`
  entry; never edit 0009.
- Queries:
  - `FinishRun`: `cost_usd = @cost_usd`.
  - New `RecordRunUsage :exec` — `UPDATE agent_runs SET tokens_in, tokens_out, cost_usd WHERE id = $1`,
    **no status filter**. Needed because `CancelRun` sets `status = 'cancelled'` at once, so
    `FinishRun` (`WHERE status = 'running'`) never touches a cancelled run.
  - `ListRuns`: select `r.cost_usd`.
  - `ListPulls`: `LEFT JOIN (SELECT pr_id, sum(cost_usd), count(cost_usd) AS priced_runs … GROUP BY pr_id)`.
    sqlc types every computed column as non-null (casts, `sum`, scalar subqueries alike), so
    the query returns `cost_usd` with `priced_runs`, and `httpapi.knownCost` turns
    `priced_runs = 0` into `null`.
- `make generate`.

### 4. Runner — `api/internal/runner`
- `runOne` returns `(review.Usage, error)`; on error the loop passes that usage to `finishFailed`.
- `finishFailed(j, pull, status, msg, took, usage)`: `FinishRun` with the real tokens and cost
  (not zeros), then always `RecordRunUsage`, so a cancelled run keeps them too.
- `save`: the "cancelled before its review was saved" branch (`LockRunningRun` → no rows) now
  calls `RecordRunUsage` in the same transaction; the model answered, and that was billed.
- Trace: `Stats.CostUSD *float64 \`json:"cost_usd"\``; `successTrace` sets it; `failureTrace`
  takes the usage and sets tokens and cost. Fix the stale contract path in `trace.go:14`.
- Live log: the final line says the cost when known ("Run complete — $0.0123; trace persisted").
- `cmd/review`: print the cost next to tokens (`main.go:172`) and add `cost_usd` to its JSON.
- Tests (`pgtest`, need Docker — see `api/INSIGHTS.md`): done run stores `cost_usd` in row and
  trace; failed-after-one-call run stores that call's tokens and cost; cancelled run keeps usage;
  non-OpenRouter fake → `NULL`.

### 5. HTTP + contracts — `api/internal/httpapi`, `client/src/vendor/shared/contracts`
- `runJSON.CostUSD`, `pullListItemJSON.CostUSD` (`*float64`, `json:"cost_usd"`).
- Zod: the three fields in the table above, **in the same commit**.
- Tests: `reviews_test.go`, `pulls_test.go` — field present, `null` when unknown, PR sum ignores
  NULL runs and is `null` when all are.

### 6. Client — three screens
- `client/src/lib/format-cost.ts`: `formatCost(usd?: number | null): string` + unit test.
  (A runtime helper lives in `src/lib`, never imported as a value from `@devdigest/shared`.)
- Screen 1: `pulls/constants.ts` — `COLUMN_KEYS` gets `"cost"` after `"score"`, `GRID` gets a
  ~72px column; `PRRow.tsx` — the cell; `messages/en/prReview.json` — `list.columns.cost`.
- Screen 2: `RunHistory.tsx` — cost beside `ran_at` in the right column, not for running runs.
- Screen 3: `TraceBody.tsx` — a fourth `<Stat label={t("trace.stat.cost")} …>`;
  `messages/en/runs.json` — `trace.stat.cost`. Check that four stats still fit the drawer width
  (`statsRow` is a flex row).
- Tests: `PRRow`, `RunHistory`, `TraceBody` render a value and "—".

### 7. Seed + e2e — `api/internal/seed`, `e2e/specs`
- The seed had a review for PR #482 but **no run**, so no screen could show a cost without a
  model call. `seed.demoRun` adds one finished run ($0.0021, tokens, duration; its `run_traces`
  row) and links the seeded review's `run_id`. Like the review, it has no agent: the demo is
  seeded before the agents.
- e2e: flow 02 checks the Cost header and the seeded value in the list; flow 04 checks the value
  in the timeline row and in the trace drawer's Stats. A response-shape change needs these flows,
  not only `pnpm test` (`client/INSIGHTS.md`). Run with `./scripts/e2e.sh` only.
- Risk: flow 04 expects the seeded review in the first accordion; a seeded run must keep that.

### 8. Docs
- `api/specs/review-run.md`: a "Cost" section (the rules under *Meaning of the number*); drop
  "Run cost isn't tracked". `api/specs/http-api.md`: the three new fields.
- `api/internal/pgtest`: its "every migration ran" check looks for 0010's index now.
- `client/specs/pr-list.md`, `client/specs/pr-detail.md`: the new column and values.
- `specs/review-flow.md`: one line in the review step.
- Set this spec's status to **done** once e2e has run.

## Out of scope
- Cost for providers that don't report it (no price table, no estimate).
- Sorting/filtering the PR list by cost; budgets, alerts, per-agent or per-repo totals.
- Backfilling cost for runs made before this feature (they stay "—").
- `ci_runs` / eval cost columns.

## Done when
- A review with an OpenRouter agent shows the same non-null cost in the timeline row, the trace
  Stats and (summed) the PR list; the OpenRouter dashboard's cost for that session matches.
- The same review with an OpenAI or Anthropic agent shows "—" on all three screens.
- Cancelling a run after its first map-reduce call keeps that call's tokens and cost.
- `make check` (with Docker up), `pnpm test`, `pnpm typecheck` and `./scripts/e2e.sh` pass.
