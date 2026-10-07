# Intent Layer — Implementation Plan
Status: approved

## Source requirements
`specs/intent-layer.md` (Status: approved; open questions resolved in its "Decisions").
This plan covers every AC: AC-1 … AC-31, including AC-17a. Living specs it changes (the spec's
`Changes:` line): `api/specs/review-run.md`, `api/specs/http-api.md`, `client/specs/pr-detail.md`,
`client/specs/settings.md`, `specs/review-flow.md`.

## Clarifications & recommendations

**BLOCKING PREREQUISITE (user action, before `/run-plan`).** AC-17 / AC-17a and the caller's
brief assume `api/internal/claudecode`, `ANTHROPIC_VIA_CLAUDE_CODE` and the Claude Code branch in
`reviewModel`. **None of these are on `main`.** They exist only on the local branch
`claudecode-adapter` (commit `042289e`, one commit on top of `main`'s `b8b456d`), with the two
2026-10-06 `api/INSIGHTS.md` entries about `claude -p`. Merge or fast-forward that branch into
the working branch first. `run-plan` never merges, and T6 does not compile without it.
Everything below cites the code as it is on that branch (`git show claudecode-adapter:<path>`).

Gaps between the spec and the code, and how this plan settles them. **All of these are my
judgment calls; check them before approving.**

1. **What counts as a "plan/spec link" (AC-7/8/9).** AC-9's verify step counts
   `http://169.254.169.254/` (no file extension) as an unresolved reference, so a narrow
   "only `*.md` links" rule would fail it. Rule used here: every link target in the
   description (a Markdown `[x](target)` or autolink, or a bare `http(s)://` URL) that is
   **not** an image (`![…](…)`) and **not** a same-repo issue/PR reference is a document
   reference. A bare relative path in prose counts only when it has a text-doc extension
   (`.md .mdx .txt .rst .adoc`) and contains a `/`. Without this condition, prose that
   mentions `src/config.ts` would be read as a plan. **Consequence:** any external link
   (a CI badge, a Jira link) makes the reference unresolved, so the intent can't be "high"
   (AC-8). That is the literal reading of the spec. The alternative is to report non-GitHub
   links as unresolved without letting them block "high"; that needs a spec change.
2. **The head commit is usually missing from the clone.** Clones are shallow clones of the
   default branch (`internal/repos/repos.go:187`, `internal/git/git.go:118` `Sync`), so
   `<head_sha>:<path>` is often absent. Plan: if `git cat-file -e <sha>^{commit}` fails, run one
   `git fetch --depth 1 origin <sha>` (to github.com, with the token header, as `git.Fetch`
   does), then retry. If the commit is still missing, every document link is unresolved with
   "head commit not available". This keeps the network invariant: GitHub only.
3. **Cost via Claude Code (AC-30).** On the branch, `claudecode.CompleteJSON` deliberately
   leaves `CostUSD` nil, because `total_cost_usd` is the API-equivalent price, not the
   subscription charge (`claudecode.go`, comment in `CompleteJSON`). This plan keeps that:
   tokens are recorded and the cost is `null`. The spec's "cost is whatever the CLI reports"
   is read as "the adapter's report", which is null. Change the adapter only if you want the
   API-equivalent figure shown.
4. **Cancel during derivation (AC-26).** Derivation runs under the context of the request's
   first not-yet-cancelled run (the "trigger run"), which also carries the intent's cost
   (AC-30). If the user cancels the trigger run while derivation runs, the call stops, that
   run ends as cancelled, and the remaining runs of the request review **without** intent.
   They don't re-derive, which keeps AC-1's "at most once".
5. **"Trivial description" (AC-14).** Remove HTML comments (`<!-- … -->`). Drop lines that are
   only a Markdown heading (`^\s*#{1,6}\s`) or an unchecked checkbox (`^\s*[-*]\s*\[ \]`).
   Remove all whitespace. The description is trivial when fewer than 50 runes remain.
6. **Symlinks.** Files are read from the git object database at the commit, never from the
   working tree. A symlink entry (mode `120000`) is resolved once, lexically, against the
   link's directory. A target that escapes the repository gives "outside repository"; a
   second hop gives "not a text file".
7. **A self-reference** (`#482` in PR #482's own text) is skipped silently. It isn't a source.
8. **Feature picker provider.** `SettingsModels.tsx:30-33` always saves `provider:
   "openrouter"`. With the new default `anthropic`/`haiku`, picking a model from the list saves
   `openrouter/<model>`. That is existing behaviour and is out of scope. Resetting to
   `anthropic/haiku` isn't possible from the UI today. A follow-up should add a provider
   selector to the picker.
9. **Runner without an intent model.** When `runner.Config.IntentLLM` is nil (only in tests),
   derivation is skipped without a log line, so the existing runner tests and logs stay
   unchanged. `cmd/api` always wires it.

## Execution mode
Recommended: **multi**. There is real parallel work: four independent foundation tasks
(contracts, prompt, DB, adapters), then API and client tasks that share only the contract.
The user confirmed: **multi**.

## Modules affected
- **api/**: migration 0012, queries, the `review` prompt section, a new domain package
  `internal/intent`, git/GitHub adapter additions, runner orchestration, the trace, wiring in
  `cmd/api`, `GET /pulls/{id}/intent`, the seed.
- **client/**: the Zod contracts (additive), the feature-model registry default and description,
  the Intent panel on the Overview tab, the intent section in the trace drawer.
- **e2e/**: one new read-only flow, 08, for the seeded intent (AC-31).
- **Out of scope:** `cmd/review` (CLI) and `cmd/mcp` get no intent. No hand editing and no
  refresh button (Decisions 5, 10). No conformance checking. No trackers other than GitHub. No
  provider selector in the Feature Models picker (recommendation 8).

## Architectural constraints
- `api/CLAUDE.md` "Conventions that differ from habit": domain packages (`review`, and the new
  `intent`) never import HTTP, SQL or SDK packages. Small interfaces live in the package that
  uses them. `context.Context` comes first on all I/O. Tests are table-driven with hand-written
  fakes. No `utils` packages.
- `api/CLAUDE.md` "Do not touch": never edit `internal/postgres/*.go` by hand (run `make
  generate`). Never edit an applied migration (add `0012_*.sql` plus a `_journal.json` entry).
  `internal/review/testdata/*.golden` changes only on purpose: **this plan changes none and only
  adds `intent.user.golden`**. Existing route JSON shapes must keep parsing in the client.
- `api/specs/review-run.md` "Inputs": the assembled prompt stays byte-for-byte equal to the
  goldens. PR text is wrapped in `<untrusted>` with the closing tag escaped
  (`internal/review/prompt.go:117-128`).
- `api/INSIGHTS.md` 2026-10-06 (on `claudecode-adapter`), "`claude -p` flags…": never add
  `--max-turns 1` or `--bare` to `claudecode`, and always pass `--system-prompt`. The intent
  call therefore always sets a non-empty `JSONRequest.System`, and nobody edits
  `internal/claudecode`.
- `api/INSIGHTS.md` 2026-10-06 "Claude Code reroutes the `anthropic` provider": keep it behind
  `ANTHROPIC_VIA_CLAUDE_CODE` in `cmd/api` `reviewModel`. No fourth provider (the Zod `Provider`
  enum doesn't change).
- `api/INSIGHTS.md` 2026-10-05 "retry loop must report a cancel as ctx.Err()": the new GitHub
  `Issue` call goes through the existing `send` loop and must not add its own retry.
- `api/INSIGHTS.md` 2026-10-01 "A new migration fails `pgtest` TestNew": point
  `pgtest_test.go` at 0012's effect.
- `api/INSIGHTS.md` 2026-10-01 "sqlc makes a computed column non-null": the intent query
  selects plain columns only.
- `api/INSIGHTS.md` 2026-10-01 "A cancelled run never gets its outcome columns": the trigger
  run's intent usage, when that run is cancelled, goes through `RecordRunUsage`
  (`runner.go:309`), never `FinishRun` alone.
- `api/INSIGHTS.md` 2026-09-30 "`make check` passes, but the database tests never ran": T3, T6
  and T7 need Docker running.
- `client/CLAUDE.md` "Do not touch": `src/vendor/shared/` is imported as types only. Runtime
  registry values are mirrored in `src/lib/feature-models.ts`. A contract changes only
  together with the Go API.
- `client/CLAUDE.md` "Conventions": component folder layout, `s` style objects, `useTranslations`
  with a new namespace file, hooks only in `src/lib/hooks`, mutations and settles invalidate
  keys, tests mock hook modules.
- `client/INSIGHTS.md` 2026-09-30 "A test that passes without the API isn't testing fetch": the
  response shape change is proven by e2e (T10), not only by vitest.
- `e2e/CLAUDE.md` "Rules for flows": deterministic locators, read-only, seeded data only, next
  `NN-` number, a README coverage row. "Gotchas": flows depend on seed text and client copy.
- Root `CLAUDE.md` "Cross-part rules": contract and Go change in the same change. Root
  `INSIGHTS.md`: no relevant entry beyond the e2e setup ones (run e2e only via `scripts/e2e.sh`).

## Approach

**Flow.** `POST /pulls/{id}/review` → `runner.Start` (unchanged) → `execute`
(`runner.go:132`) loads the diff, then **once per request** calls the new `r.intentFor(...)`
before the job loop. That function:
1. Reads the stored intent for the PR. If its `fingerprint` equals
   `intent.Fingerprint(head_sha, title, body)`, it reuses it (AC-18) at no cost.
2. Otherwise it resolves the model from `settings.feature_models.review_intent` (the `settings`
   rows of the workspace, as `httpapi.settings` reads them). The default is
   `anthropic`/`haiku`. It gets a client from `Config.IntentLLM(provider, model)`.
3. It gathers sources through `intent.Gather` (links, the clone at head, GitHub issues, branch,
   commit subjects, diff file stats).
4. It calls `intent.Derive` (one retry on an invalid answer, clip to limits) under
   `context.WithTimeout(trigger.ctx, 60s)`.
5. On success it upserts `pr_intent`. On failure it leaves the stored row untouched (AC-25).

Then each job's `runOne` sets `prompt.Intent` (rendered text) when there is an intent. The trigger
run adds the intent call's `review.Usage` to its own usage, on every exit path. The trace gains
an `intent` object and `prompt_assembly.intent`.

**Prompt section (T2).** Add `Intent string` (untrusted, pre-rendered) to `review.Prompt`
(`prompt.go:34`) and `Intent string \`json:"intent,omitempty"\`` to `Assembly` (`prompt.go:50`).
In `Assemble`, put it right after the PR description:
`add("## PR intent\n", wrapIf("pr-intent", a.Intent))`. An empty intent adds nothing, so
`full.*.golden` and `minimal.user.golden` stay byte-identical (AC-4). `Run` already calls
`in.Prompt.Assemble(c.diff)` per chunk (`run.go:115`), so map-reduce gets the intent in every call
(AC-3). Export thin wrappers `review.Untrusted(source, content)` (around `untrusted`,
`prompt.go:120`) and `review.JSONObject(text)` (around `jsonObject`, `structured.go:120`) for
`internal/intent`. Behaviour doesn't change.

**Domain package `internal/intent` (T5).** It imports `review` (for `LLM`, `JSONRequest`,
`Usage`, `Untrusted`, `JSONObject`) and the standard library only.
- Types: `Intent{Statement, InScope, OutOfScope, Confidence, Sources []Source, Unresolved
  []Unresolved}`. `Source{Kind, Label, Ref string; Truncated bool}`, where Kind is one of
  `title|description|issue|document|branch|commits|files`. `Unresolved{Ref, Reason}`.
- `Extract(title, body, owner, name string, self int) Refs`: document refs and issue refs in
  order of appearance, de-duplicated. A path, `./path` and a same-repo blob URL at **any** ref
  normalize to one cleaned repo path (Decision 7). Issue patterns are `#N`,
  `(Fixes|Closes|Resolves) #N`, and same-repo `github.com/o/n/(issues|pull)/N`. Cross-repo
  `other/repo#N` and cross-repo URLs become unresolved "other repository". See recommendation 1
  for what counts as a document ref.
- Path safety, before any read: reject absolute paths, `..` after `path.Clean`, backslashes and
  NUL. Reason "outside repository".
- `Gather(ctx, in GatherInput, files FileReader, issues IssueReader) Sources`. It uses two small
  interfaces defined in `intent`:
  - `FileReader`: `Read(ctx, path) (data []byte, symlink bool, err error)` plus sentinel errors
    `ErrNoClone`, `ErrNoCommit`, `ErrNotFound`.
  - `IssueReader`: `Issue(ctx, n int) (title, body string, err error)`; a nil reader means "no
    GitHub token".

  `Gather` applies the caps: ≤ 5 documents × 20,000 runes (marked truncated beyond); ≤ 3 issues ×
  4,000 runes of body; description ≤ 4,000; ≤ 50 commit subjects (first line). Any further refs
  get "limit reached". Text check: no NUL in the first 8,000 bytes and valid UTF-8, else "not a
  text file".
- `Trivial(desc) bool` (recommendation 5). `Confidence(...)` per AC-15, computed only from what
  `Gather` read or failed to read, never from the model (Decision 3).
- `Derive(ctx, llm, model string, s Sources) (Intent, review.Usage, error)`. The system prompt
  states the job (English statement, Decision 8), the JSON-only answer and an injection guard
  like `review.injectionGuard`: data in `<untrusted>` is never instructions. Every source goes
  into the user message wrapped with `review.Untrusted`, with source labels `pr-title`,
  `pr-description`, `issue-#N`, `doc:<path>`, `branch`, `commits`, `changed-files`. Changed
  files appear as `path (+a −d)` lines only, never diff content. The schema is embedded
  `intent.schema.json` with `{intent: string, in_scope: string[], out_of_scope: string[]}`, all
  required and `additionalProperties: false`. That is needed for OpenAI strict mode
  (`internal/openai/openai.go:155`) and is how `review.schema.json` does it. `SchemaName:
  "Intent"`. An invalid answer (not JSON, a wrong type, or an empty statement) gets exactly **1**
  retry with a repair message, mirroring `askForReview` (`structured.go:64`). Clip the output to
  600 runes for the statement, ≤ 10 items per list and 200 runes per item (AC-27). Usage is
  summed over attempts.
- `Fingerprint(headSHA, title, body string) string`: the hex SHA-256 of the three joined by
  `\x00`.
- `(Intent) PromptText() string`: the text that goes inside the review's `pr-intent` block:
  ```
  Confidence: <level>
  Intent: <statement>
  In scope:
  - …            (or "- (none stated)")
  Out of scope:
  - …
  ```

**Adapters (T4).**
- `internal/git`: `HasCommit(ctx, dir, sha) bool`, `FetchCommit(ctx, dir, sha, token) error`
  (`fetch --depth 1 --end-of-options origin <sha>`), and
  `ReadBlob(ctx, dir, commit, path string, max int64) (data []byte, mode string, err error)`.
  `ReadBlob` uses `ls-tree -z <commit> -- <path>` for the mode and object id, then `cat-file -s`
  and `cat-file blob`, and reads at most `max` bytes. It returns `ErrNotFound` for a missing
  path. All calls go through the existing `run` (`git.go:72`).
- `internal/github`: `Issue(ctx, owner, repo, number) (Issue{Title, Body}, error)` via `get`,
  `GET /repos/{o}/{r}/issues/{n}`. It returns a `*StatusError` (404, 403/429 rate limit) for
  `intent.Gather` to map to reasons "not found", "rate limited" and "GitHub error: <msg>".

**Runner and wiring (T6).**
- New fields on `runner.Config` (`runner.go:32`):
  - `IntentLLM func(provider, model string) (review.LLM, error)`
  - `GitHubToken func() (string, error)`
  - `GitHubAPI string`
  - `IntentTimeout time.Duration` (0 means 60 s)
- Go registry default: `intentDefaultProvider = "anthropic"`, `intentDefaultModel = "haiku"`. It
  mirrors `FEATURE_MODELS` in `platform.ts` (T1).
- `FileReader` implementation in the runner: the clone dir is `filepath.Join(cloneDir, owner,
  name)`. It is missing when `repo.ClonePath` is nil or the dir has no `.git` ("repository not
  cloned"). It runs `HasCommit`, then `FetchCommit` once, then `ReadBlob`.
- `IssueReader` implementation: `github.New(GitHubAPI, token)`, or nil when the token is empty.
- `cmd/api/main.go`: add `intentModel(store, apis, claude) func(provider, model string)
  (review.LLM, error)`. If `provider == "anthropic" && claude == nil` and `model` is one of
  `claudecode.Models()` IDs, it returns the AC-17a error ``intent model `<model>` needs
  ANTHROPIC_VIA_CLAUDE_CODE=true; pick another intent model in Settings``. Otherwise it
  delegates to `reviewModel(...)(provider)`, so a missing key yields "`<KEY>` is not
  configured". Wire `GitHubToken: githubToken` (it already exists in `run`) and
  `GitHubAPI: github.DefaultURL`.
- **Live-log lines**, exactly one per run, published through the shared log (AC-24):
  - `intent: derived (confidence <level>, <n> source(s), <m> unresolved)`
  - `intent: reused`
  - `intent: failed — <reason>; reviewing without intent`

  Reasons: the provider/key error text; `timed out after 60s`; `run cancelled` (for the other
  runs when the trigger run was cancelled); `model gave no valid intent in 2 attempts`; the
  claude CLI's own error (`claude: …`).
- **Trace** (`trace.go:15`), additive:
  - `traceJSON.Intent *intentTraceJSON \`json:"intent"\`` with `{status, reason, confidence,
    sources, unresolved, provider, model}`.
  - `assemblyJSON.Intent *string \`json:"intent"\``, filled from `res.Assembly.Intent`.

  Set both in `successTrace` and in `failureTrace`. In `failureTrace` the prompt section stays
  null.
- **Persistence**: `UpsertPullIntent` after a successful derivation, using a context detached
  from the job (like `finishFailed`, `runner.go:296`), so a later cancel can't lose a paid
  answer.

**DB (T3).** `0012_intent_layer.sql` adds these columns to `pr_intent`:
- `confidence text NOT NULL DEFAULT 'low'`
- `sources jsonb NOT NULL DEFAULT '[]'::jsonb`
- `unresolved jsonb NOT NULL DEFAULT '[]'::jsonb`
- `head_sha text`
- `fingerprint text`
- `provider text`
- `model text`
- `tokens_in integer`
- `tokens_out integer`
- `cost_usd double precision`
- `derived_at timestamptz NOT NULL DEFAULT now()`

Queries in `queries/intent.sql`:
- `GetPullIntent :one` (by `pr_id`)
- `UpsertPullIntent :exec` (`INSERT … ON CONFLICT (pr_id) DO UPDATE SET …, derived_at = now()`)

**HTTP (T7).** `GET /pulls/{id}/intent` in a new `internal/httpapi/intent.go`, registered next
to `GET /pulls/{id}` (`server.go:117`). `pathID` gives 422; `pullAndRepo` (`pulls.go:275`) gives
404 and the workspace scoping. It answers 200 with `null` when there is no row, otherwise the
`PrIntentRecord` JSON below, with `derived_at` in JS time format (`jsTimePtr`-style helper).
Seed: in `demo()` (`seed.go:111`), after the commit insert, call `UpsertPullIntent` for #482
with fixed values:
- statement: "Protect the public API from abuse by unauthenticated clients by adding per-client
  rate limiting to the public endpoints."
- in scope: "Token-bucket rate limiting middleware for public /api endpoints", "Rate-limit
  configuration"
- out of scope: "Authenticated and internal endpoints", "Changes to authentication"
- confidence `medium`; sources title, description, branch, commits, files; unresolved `[]`
- `head_sha "a1b2c3d4e5f6"`, provider `anthropic`, model `haiku`, `fingerprint NULL`, so the
  first real review re-derives.

**Contract (T1), the single source of the JSON shapes.** In `brief.ts`, next to `Intent`:
- `IntentConfidence = z.enum(['high','medium','low'])`
- `IntentSource = z.object({ kind: z.enum(['title','description','issue','document','branch','commits','files']), label: z.string(), ref: z.string().nullable(), truncated: z.boolean() })`
- `IntentUnresolved = z.object({ ref: z.string(), reason: z.string() })`

In `review-api.ts`, extend `PrIntentRecord = Intent.extend({ pr_id, confidence:
IntentConfidence, sources: z.array(IntentSource), unresolved: z.array(IntentUnresolved), head_sha:
z.string().nullable(), provider: z.string().nullable(), model: z.string().nullable(), tokens_in:
z.number().int().nullable(), tokens_out: z.number().int().nullable(), cost_usd:
z.number().nullable(), derived_at: z.string() })`, plus `PrIntentResponse =
PrIntentRecord.nullable()`.

In `trace.ts`:
- `RunIntent = z.object({ status: z.enum(['derived','reused','failed']), reason:
  z.string().nullable(), confidence: IntentConfidence.nullable(), sources: z.array(IntentSource),
  unresolved: z.array(IntentUnresolved), provider: z.string().nullable(), model:
  z.string().nullable() })`
- `RunTrace.intent: RunIntent.nullish()` (old traces don't have it)
- `PromptAssembly.intent: z.string().nullish()`

In the `platform.ts` `FEATURE_MODELS` `review_intent` entry and its mirror in
`src/lib/feature-models.ts`: `defaultProvider: 'anthropic'`, `defaultModel: 'haiku'`,
description "Classifies a PR’s intent and scope before every review (Claude Code · haiku by
default)."

**Client UI (T8, T9).**
- New hook `usePrIntent(prId)`, key `["pr-intent", prId]`, `api.get<PrIntentResponse>`.
  `page.tsx` invalidates `["pr-intent", prId]` where it already invalidates `pr-runs` on settle
  (`page.tsx:54-57`).
- `OverviewTab` gets `prId` and renders `IntentPanel` above the description. Copy lives in
  `messages/en/intent.json`, and these strings are **fixed because e2e relies on them**:
  - title "Intent"
  - badge "Confidence: {level}" (the level as text, for accessibility)
  - "In scope", "Out of scope", "Sources", "Unresolved references"
  - empty state "Intent is derived on the next review."

  The panel renders every field and label as React text children only. No Markdown renderer,
  no `dangerouslySetInnerHTML`, and no `<a>` or `<img>` built from intent data (AC-22).
- The trace drawer gets an `IntentTrace` section in `TraceBody`: status (derived / reused /
  "No intent — {reason}"), confidence, sources, unresolved, provider/model. There is also a
  `PromptBlock` for `prompt_assembly.intent` with a new `PROMPT_COLORS.intent`. When the trace
  has no `intent` (an old trace), the section isn't shown.

## Tasks

Waves (run-plan derives them from Depends-on): **T1, T2, T3, T4** → **T5, T7, T8, T9** →
**T6** → **T10**. Precondition for all tasks: `claudecode-adapter` is merged (see Clarifications).

### T1 — Shared contracts and the feature-model registry default
- Action: Add `IntentConfidence`, `IntentSource` and `IntentUnresolved` to `brief.ts`. Extend
  `PrIntentRecord` and add `PrIntentResponse` in `review-api.ts`. Add `RunIntent`,
  `RunTrace.intent` and `PromptAssembly.intent` in `trace.ts`. Set `review_intent` to
  `anthropic`/`haiku` with the new description in `platform.ts` and its mirror
  `src/lib/feature-models.ts`. Re-export the new types from `src/lib/types.ts`. Update
  `client/specs/settings.md` (default and description). All shapes are exactly as in Approach →
  Contract.
- Module: client
- Type: contract
- Skills to use: zod, typescript-expert
- Owned paths: `client/src/vendor/shared/contracts/brief.ts`, `client/src/vendor/shared/contracts/review-api.ts`, `client/src/vendor/shared/contracts/trace.ts`, `client/src/vendor/shared/contracts/platform.ts`, `client/src/lib/feature-models.ts`, `client/src/lib/types.ts`, `client/specs/settings.md`
- Depends-on: —
- Known gotchas: `client/CLAUDE.md` "Do not touch" (types-only import; keep the mirror in
  sync). `client/INSIGHTS.md` 2026-09-30 "Importing a value from `@devdigest/shared` breaks the
  build". New fields must be additive: `RunTrace.intent` is `.nullish()` so the
  `TraceBody.test.tsx` fixtures still typecheck.
- Acceptance: AC-17 (registry default and picker description), AC-29 (response shape),
  AC-23 (trace shape). Proven by `pnpm typecheck` passing with unchanged existing tests.

### T2 — Intent section in the review prompt
- Action: Add `Prompt.Intent` and `Assembly.Intent`, plus the `## PR intent` untrusted section
  right after the PR description. Export `review.Untrusted` and `review.JSONObject` wrappers. Add
  `testdata/intent.user.golden` (Go-generated; document in the test that it isn't a TS golden).
  Add tests: the existing goldens pass unchanged when `Intent == ""`; with an intent the section
  is wrapped in `<untrusted source="pr-intent">` and a `</untrusted>` inside it is escaped; a
  map-reduce `Run` with a fake `LLM` sends the intent in every per-file call.
- Module: api
- Type: domain
- Skills to use: security (untrusted PR text into a prompt); Go conventions: none
  (api/CLAUDE.md, api/docs/go-idioms.md)
- Owned paths: `api/internal/review/prompt.go`, `api/internal/review/prompt_test.go`, `api/internal/review/structured.go`, `api/internal/review/run_test.go`, `api/internal/review/testdata/intent.user.golden`
- Depends-on: —
- Known gotchas: `api/CLAUDE.md` "Do not touch": `full.*.golden` and `minimal.user.golden` must
  not change. `api/specs/review-run.md` "Inputs" (byte-for-byte prompt).
- Acceptance: AC-2, AC-3, AC-4, AC-19. Proven by `go test -race ./internal/review/...`, with
  `TestAssembleMatchesTypeScript` untouched and green.

### T3 — Migration 0012 and intent queries
- Action: Add `0012_intent_layer.sql` (columns as in Approach → DB) and its `_journal.json`
  entry with a later `when`. Add `queries/intent.sql` (`GetPullIntent`, `UpsertPullIntent`). Run
  `make generate`. Point `pgtest_test.go` at 0012's effect (for example, the `pr_intent.fingerprint`
  column exists).
- Module: api
- Type: migration
- Skills to use: postgresql-table-design
- Owned paths: `api/migrations/0012_intent_layer.sql`, `api/migrations/meta/_journal.json`, `api/internal/postgres/queries/intent.sql`, `api/internal/postgres/*.go` (generated only), `api/internal/pgtest/pgtest_test.go`
- Depends-on: —
- Known gotchas: `api/INSIGHTS.md` 2026-10-01 "A new migration fails `pgtest` TestNew";
  2026-10-01 "sqlc makes a computed column non-null"; 2026-09-30 "The first `make generate` takes
  minutes"; `api/migrations/migrations.go` package doc (the journal format). **Needs Docker**
  for `pgtest`.
- Acceptance: AC-5 (every stored field has a column). Proven by `go test -race
  ./internal/pgtest/...` with Docker (check that it isn't SKIPped).

### T4 — Git blob reading at a commit, and GitHub issues
- Action: Add `git.HasCommit`, `git.FetchCommit` and `git.ReadBlob` (with the mode, a byte cap
  and `ErrNotFound`). Add `github.Issue`. Tests: git against a temp repo built in the test
  (a blob, a symlink entry with mode `120000`, a missing path, a size cap); github against
  `httptest` (200, 404, 429 then 200, cancel → `ctx.Err()`).
- Module: api
- Type: domain (adapters)
- Skills to use: security (paths and tokens); Go conventions: none (api/CLAUDE.md)
- Owned paths: `api/internal/git/**`, `api/internal/github/**`
- Depends-on: —
- Known gotchas: `api/INSIGHTS.md` 2026-10-05 "A retry loop must report a cancel as
  ctx.Err()" (reuse `send`, don't add a loop); 2026-09-30 "A GitHub token saved inside an old
  clone" (the token only in the per-command header, `git.go:59`). Use `--end-of-options` as the
  existing calls do.
- Acceptance: these supply AC-7, AC-11 and AC-12. Proven by `go test -race ./internal/git/...
  ./internal/github/...`.

### T5 — Intent domain package
- Action: Create `internal/intent` as in Approach → Domain package: `Extract`, path safety,
  `Gather` with the `FileReader`/`IssueReader` interfaces and every cap, `Trivial`,
  `Confidence`, `Derive` (system prompt with the guard, untrusted sources, embedded
  `intent.schema.json`, 1 retry, clipping), `Fingerprint`, `PromptText`. Table-driven tests with
  fake readers and a fake `review.LLM`:
  - link extraction, including the AC-9 example: `http://169.254.169.254/` and
    `https://evil.example/spec.md` give two unresolved refs and **zero** reader calls;
  - de-duplication (path, `./path`, blob URL at another ref);
  - `..`, absolute paths and an escaping symlink give "outside repository";
  - a binary document gives "not a text file";
  - a 25,000-rune document is truncated;
  - a 6th document and a 4th issue give "limit reached";
  - `other/repo#5` gives "other repository";
  - issue errors map to reasons;
  - the trivial-description cases;
  - the three confidence levels;
  - invalid JSON twice gives an error after exactly 2 calls;
  - over-long fields are clipped;
  - the system prompt contains the guard and every source is inside `<untrusted>`.
- Module: api
- Type: domain
- Skills to use: security (untrusted PR text, SSRF, path traversal); Go conventions: none
  (api/CLAUDE.md, api/docs/go-idioms.md)
- Owned paths: `api/internal/intent/**`
- Depends-on: T2
- Known gotchas: `api/CLAUDE.md` "Conventions" (no SQL, HTTP or SDK imports; interfaces in the
  using package; no `utils`). The intent request must always set `System` (`api/INSIGHTS.md`
  2026-10-06 `claude -p` flags). The schema follows OpenAI strict-mode rules
  (`internal/openai/openai.go:155`).
- Acceptance: AC-6 to AC-15, AC-19, AC-25 (one retry), AC-27, AC-18 (fingerprint). Proven by
  `go test -race ./internal/intent/...`.

### T6 — Runner orchestration, trace, cost and wiring
- Action: Implement `intentFor` in a new `internal/runner/intent.go` (reuse check, model
  resolution from settings with the Go default, `Gather` with runner-side reader
  implementations, `Derive` under a 60 s timeout on the trigger run's ctx, upsert on success,
  exact log lines). Call it once in `execute` after the diff loads. Pass the result into
  `runOne` and set `prompt.Intent`. Add the trigger run's intent usage to its usage on every
  path: success, failure, cancelled-before-turn (`runner.go:184-186` must return the intent
  usage, not `review.Usage{}`), and cancelled during the call. Extend `trace.go`. Add the new
  `Config` fields. In `cmd/api` add `intentModel` with the AC-17a check and wire it.
  Update `api/specs/review-run.md` (a new "Intent" section under Inputs, the log lines and the
  cost attribution), `api/docs/architecture.md` (an `internal/intent` row; runner row mentions
  intent) and `specs/review-flow.md` (§4 Review: intent derived once per request; the
  invariant line unchanged).
- Module: api
- Type: domain (runner) + wiring
- Skills to use: security (untrusted text, secrets never in log or trace); Go conventions: none
  (api/CLAUDE.md, api/docs/go-idioms.md)
- Owned paths: `api/internal/runner/**`, `api/cmd/api/**`, `api/specs/review-run.md`, `api/docs/architecture.md`, `specs/review-flow.md`
- Depends-on: T1, T2, T3, T4, T5
- Known gotchas:
  - `api/INSIGHTS.md` 2026-10-01 "A cancelled run never gets its outcome columns" (use
    `RecordRunUsage`).
  - 2026-10-01 "Long-lived requests end through `Server.CloseStreams`" (no new stream; don't
    block shutdown).
  - 2026-10-06 "Claude Code reroutes the `anthropic` provider" (the AC-17a check lives in
    `cmd/api`; no new provider).
  - 2026-10-06 `claude -p` flags (don't touch `internal/claudecode`).
  - `api/CLAUDE.md` "every goroutine has an owner" (no new goroutine is needed).
  - **Needs Docker** (runner tests use `pgtest`).
- Acceptance:
  - AC-1: "Run all" with 3 agents and a counting fake LLM makes exactly 1 intent call; all 3
    traces have the same `intent`.
  - AC-2: `prompt_assembly.intent` holds the `<untrusted source="pr-intent">` block.
  - AC-5: the stored row has all fields.
  - AC-16: a saved `feature_models.review_intent` choice is used.
  - AC-17a: `cmd/api` test: `intentModel` with `claude == nil` and `anthropic`/`haiku` returns
    the exact message, and the review still runs.
  - AC-18: a second request with an unchanged fingerprint makes 0 calls and logs
    `intent: reused`.
  - AC-24: exactly one intent line per run.
  - AC-25: a provider error, or invalid output twice, makes the run succeed without intent and
    keeps the previous row.
  - AC-26: cancel the trigger run during a blocking fake intent call; that run ends
    `cancelled` and the others review without intent.
  - AC-28: `IntentTimeout` of 50 ms and a blocking fake give a "timed out" failure.
  - AC-30: the trigger run's `tokens_*`, `cost_usd` and trace stats include the intent usage.
  - AC-4: no intent means the runner's existing prompt tests are unchanged.

### T7 — `GET /pulls/{id}/intent` and the seeded intent
- Action: Add the handler in `internal/httpapi/intent.go` and register the route in `server.go`.
  Tests: 200 with the record (all fields, JS time format), 200 `null`, 404 for an unknown PR and
  for another workspace's PR, 422 for a malformed id. Seed #482's intent in `seed.go` `demo()`
  with the fixed values from Approach → HTTP. Extend `seed_test.go` `TestRun` to assert the row
  exists and that re-seeding keeps one row. Update `api/specs/http-api.md`: the route row under
  "Pull requests", the route count 49 → 50, and the seed note.
- Module: api
- Type: route
- Skills to use: security (route input validation, no secrets in responses); Go conventions:
  none (api/CLAUDE.md)
- Owned paths: `api/internal/httpapi/intent.go`, `api/internal/httpapi/intent_test.go`, `api/internal/httpapi/server.go`, `api/internal/seed/seed.go`, `api/internal/seed/seed_test.go`, `api/specs/http-api.md`
- Depends-on: T1, T3
- Known gotchas: `api/specs/http-api.md` "Rules for every route" (the 422 `validation_error`
  shape, workspace scoping, JS times). `api/INSIGHTS.md` 2026-10-02 "Rows made in one
  transaction share `created_at`" (the seed runs in one transaction; don't assert on order).
  `e2e/CLAUDE.md` "Gotchas": the seed text is relied on by e2e, so keep #482's existing values
  unchanged. **Needs Docker.**
- Acceptance: AC-29, AC-31. Proven by `go test -race ./internal/httpapi/... ./internal/seed/...`
  with Docker.

### T8 — Intent panel on the PR Overview tab
- Action: Add `usePrIntent` in `src/lib/hooks/intent.ts` and export it from the hooks barrel.
  Add `OverviewTab/_components/IntentPanel/` (folder convention: `IntentPanel.tsx`, `index.ts`,
  `styles.ts`, `helpers.ts`, `IntentPanel.test.tsx`). Pass `prId` to `OverviewTab` from
  `page.tsx` and invalidate `["pr-intent", prId]` when runs settle. Add the new namespace
  `messages/en/intent.json` with the fixed copy. Tests (with mocked hooks):
  - the full record renders the statement, both lists, "Confidence: medium", sources and
    unresolved;
  - `null` renders the empty-state text and no error;
  - a statement containing `![x](https://evil.example/p.png)` and `<img src=x>` renders
    literally, with no `img` element in the DOM.

  Update `client/docs/data-layer.md` (the query key), `client/docs/route-map.md` (`GET
  /pulls/:id/intent`) and `client/specs/pr-detail.md` (the Overview Intent panel; the trace
  drawer's intent section; e2e flow 08).
- Module: client
- Type: hook + component
- Skills to use: react-best-practices, next-best-practices (page.tsx), react-testing-library,
  security (rendering untrusted text)
- Owned paths: `client/src/lib/hooks/intent.ts`, `client/src/lib/hooks/index.ts`, `client/src/app/repos/[repoId]/pulls/[number]/_components/OverviewTab/**`, `client/src/app/repos/[repoId]/pulls/[number]/page.tsx`, `client/messages/en/intent.json`, `client/docs/data-layer.md`, `client/docs/route-map.md`, `client/specs/pr-detail.md`
- Depends-on: T1
- Known gotchas: `client/CLAUDE.md` "Conventions" (folder layout, `s` styles, no Tailwind, no
  `fetch` in components, tests mock hook modules with real messages). "Gotchas": e2e finds
  elements by English text, so keep the fixed copy. `client/INSIGHTS.md` 2026-10-02 "`pnpm build`
  breaks a running `pnpm dev`" (don't build while dev runs).
- Acceptance: AC-20, AC-21, AC-22 (Overview). Proven by `pnpm exec vitest run
  src/app/repos/\[repoId\]/pulls/\[number\]/_components/OverviewTab`.

### T9 — Intent in the run trace drawer
- Action: Add `RunTraceDrawer/_components/IntentTrace/` (folder convention, with a test) and
  render it in `TraceBody` when `trace.intent` is present. Add a `PromptBlock` for
  `prompt_assembly.intent` and `PROMPT_COLORS.intent` in `constants.ts`. Add keys in
  `messages/en/runs.json`: "Intent", "Derived", "Reused", "No intent — {reason}", "Intent
  model". Tests:
  - derived, reused and failed render correctly;
  - an old trace without `intent` hides the section;
  - an intent prompt block containing `<img>` renders as text.
- Module: client
- Type: component
- Skills to use: react-best-practices, react-testing-library
- Owned paths: `client/src/app/repos/[repoId]/pulls/[number]/_components/RunTraceDrawer/_components/TraceBody/**`, `client/src/app/repos/[repoId]/pulls/[number]/_components/RunTraceDrawer/_components/IntentTrace/**`, `client/src/app/repos/[repoId]/pulls/[number]/_components/RunTraceDrawer/constants.ts`, `client/messages/en/runs.json`
- Depends-on: T1
- Known gotchas: `e2e/docs/writing-flows.md` flow 04 relies on the "COST" stat and "Open run
  trace & logs" in `runs.json`/`prReview.json`, so don't rename existing keys. `client/CLAUDE.md`
  "Conventions".
- Acceptance: AC-23, AC-22 (trace). Proven by `pnpm exec vitest run
  src/app/repos/\[repoId\]/pulls/\[number\]/_components/RunTraceDrawer`.

### T10 — e2e flow for the seeded intent
- Action: Add `e2e/specs/08-pr-intent.flow.json`:
  1. open `{BASE}/`
  2. `wait --url /pulls`
  3. click "Add rate limiting to public API endpoints"
  4. `wait --url /pulls/482`
  5. `wait --load networkidle`
  6. `wait --text "Intent"`
  7. `wait --text "Confidence: medium"`
  8. `wait --text "per-client rate limiting"`
  9. `wait --text "Out of scope"`

  Add a row to the coverage table in `e2e/README.md` and a row in `e2e/docs/writing-flows.md`
  "What each flow depends on" (the seed intent in `seed.go`; the copy in
  `messages/en/intent.json`).
- Module: e2e
- Type: flow
- Skills to use: none (e2e/docs/writing-flows.md)
- Owned paths: `e2e/specs/08-pr-intent.flow.json`, `e2e/README.md`, `e2e/docs/writing-flows.md`
- Depends-on: T7, T8
- Known gotchas: `e2e/CLAUDE.md` "Rules for flows" (deterministic, read-only, never `chat`).
  `e2e/INSIGHTS.md` 2026-09-30 "Flows 02/04/05 land on the wrong repository" (run only via
  `scripts/e2e.sh`). Root `INSIGHTS.md` 2026-09-30 (`agent-browser` must be installed).
- Acceptance: AC-31 and AC-20 end to end, without any API key. Proven by `./scripts/e2e.sh`
  being all green, flows 01–08.

## Verification

Run from each part's folder. **Docker must be running** for the api DB tests (`pgtest`, runner,
httpapi, seed). A SKIP means they didn't run, and that proves nothing (`api/INSIGHTS.md`
2026-09-30).

| Part | Command | Passing looks like |
|---|---|---|
| api, per task while waves run in parallel | `gofmt -l .` · `go vet ./internal/<pkg>/...` · `go test -race ./internal/<pkg>/...` for the task's packages | empty gofmt output; no FAIL; `go test -v` shows no SKIP for DB tests |
| api, after T6/T7 (full: migration + shared contract) | `make check` | gofmt, vet, staticcheck and `go test -race ./...` all pass; `TestAssembleMatchesTypeScript` unchanged |
| client | `pnpm typecheck && pnpm test` | 0 type errors; all vitest suites pass, existing ones unchanged |
| e2e | `./scripts/e2e.sh` (repo root; needs `agent-browser`) | flows 01–08 pass |

Golden check: `git diff --stat -- api/internal/review/testdata/` shows only the **added**
`intent.user.golden`.

Manual checks (needs a human):
1. With `ANTHROPIC_VIA_CLAUDE_CODE=true` and a signed-in `claude` CLI, on a real PR whose
   description links `specs/<x>.md` at its head: "Run all" with 2+ agents. Each live log shows
   one `intent: derived (confidence high, …)` line. The Overview panel shows the intent after
   the runs settle. The trace drawer shows the intent section and the `pr-intent` prompt block.
   Only the first run's tokens include the intent call.
2. Run again with nothing changed: every log shows `intent: reused`.
3. With `ANTHROPIC_VIA_CLAUDE_CODE` unset and no `review_intent` choice saved: the log shows
   ``intent: failed — intent model `haiku` needs ANTHROPIC_VIA_CLAUDE_CODE=true; pick another
   intent model in Settings; reviewing without intent``, and the review completes.
4. Settings → Feature Models shows "PR Review · Intent" with the default `haiku` and the new
   description.
5. In the browser's network tab on the PR page: no request to any host named in the intent text
   (AC-22).
