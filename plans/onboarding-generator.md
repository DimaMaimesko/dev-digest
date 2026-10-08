# Onboarding Generator (Onboarding Tour) — Implementation Plan
Status: approved

## Source requirements
`specs/onboarding-generator.md`, committed on branch `feat/onboarding-generator`. The spec is
being set to **approved**. Q1–Q4 are resolved inside the spec. For Q5–Q13 the user accepted the
defaults below as confirmed decisions, together with the other judgment calls in this section
(2026-10-08).

ACs covered: all of them. That is AC-1 … AC-42: AC-1–AC-30, AC-31, AC-32–AC-37, AC-38, AC-39,
AC-40, AC-41 and AC-42.

Living specs the feature changes (the spec's `Changes:` line):
- `api/specs/http-api.md` and `api/specs/repos-jobs.md` are updated in T8. `api/specs/http-api.md`
  "Changing this contract" requires that in the same change.
- `client/docs/route-map.md` and a new `client/specs/onboarding-tour.md` are updated in T7.
- `e2e/docs/writing-flows.md` and `e2e/README.md` are updated in T9 (`e2e/CLAUDE.md` "Rules for flows").
- `specs/review-flow.md` gets a new step, and its invariant "No step needs an LLM key except
  running a review" changes to "…except running a review or generating a tour". That edit belongs
  to `doc-writer` after the feature ships, not to a task here.

## Clarifications & recommendations

**Decisions on the open questions.** I proposed them; the user confirmed all of them
(2026-10-08).
1. **Q5, empty code index (no TS/JS)** (confirmed): **allow.** A Go-only repository indexes as
   `status = partial`, `files_indexed = 0`, `stats.reason = "no_files"`
   (`api/internal/repointel/indexer.go:85-90`), with `last_indexed_sha` set. The plan treats
   that as a completed index. The tour then uses the file tree, docs, manifests and TODOs, and
   the header says "Code index is empty" instead of the partial-index note. Refusing it would
   make the feature useless on this repo's own `api/`.
2. **Q6, regenerate one section**: **later.** It stays out of scope, as the spec drafts it.
3. **Q7, flag risky commands** (confirmed): **no.** The fixed note (AC-27) is the only warning. A
   heuristic flag would give false confidence that unflagged commands are safe. It can be a
   follow-up.
4. **Q8, secret exclusion list** (confirmed): never read the **contents** of the following files
   (their paths may still appear in the tree):
   - `.env` and `.env.*`, except `.env.example`, `.env.sample` and `.env.template`
   - `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`, `*.keystore`, `*.kdbx`
   - `id_rsa*`, `id_dsa*`, `id_ecdsa*`, `id_ed25519*`
   - `.npmrc`, `.pypirc`, `.netrc`, `.git-credentials`, `*.tfvars`, `*.tfstate*`
   - any file under `.ssh/` or `.aws/`
   - any basename containing `secret`, `credential` or `serviceaccount`, compared
     case-insensitively

   The same list filters the TODO scan. On top of that denylist, files are read only from an
   **allowlist**, at the repo root and in top-level directories (depth ≤ 2), at most 20 files:
   - `README*`, `CONTRIBUTING*`, `CLAUDE.md`, `AGENTS.md`
   - `package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `requirements*.txt`, `pom.xml`,
     `build.gradle*`
   - `Makefile`, `justfile`, `Taskfile.y*ml`, `Procfile`, `Dockerfile*`
   - `docker-compose*.y*ml`, `compose*.y*ml`
   - `.env.example`, `.env.sample`, `.env.template`, `.nvmrc`, `.tool-versions`
5. **Q9, limits** (confirmed):
   - **Input budget**: each call's user message is at most **48,000 runes** (about 12k
     tokens). The budget is counted in runes, so `internal/tour` doesn't need a tokenizer.
   - **Time limit**: **90 s per section**, covering all 3 attempts.
   - **Job limit**: the 5 sections run **in parallel inside one background job**, so a
     generation fits in today's **2-minute job limit**. That limit doesn't change.
   - **Content caps**: AC-20/21/24/25/28/29 as drafted.
6. **Q10, cancel**: **no** for v1. A generation is bounded at 2 minutes and at most 15 calls.
7. **Q11, Regenerate confirmation** (confirmed): **yes**, but only when the tour isn't stale. Use
   `window.confirm` with the last generation's cost, formatted by `formatCost`. That is the
   same pattern the shell uses for removing a repo (`useShellContext.ts:44`).
8. **Q12, "generated <relative time>"**: this is the tour's `generated_at`, as AC-13 says. It is
   not the repo's last sync.
9. **Q13, seed and e2e** (confirmed): **yes.** Seed a tour for `acme/payments-api` (T8) and add a
   read-only flow 09 (T9). The seeded repo has no clone and no index. The page therefore shows
   the tour with "Repository not indexed yet" and a disabled Regenerate, which is the spec's
   edge case.

**Other judgment calls (mine; confirmed by the user, 2026-10-08):**
- **Wire shape vs the old generic contract.** The `Onboarding` / `OnboardingSection` /
  `OnboardingLink` block in `contracts/knowledge.ts:28-47` is unused and uses Markdown and Mermaid
  bodies, which the spec forbids (AC-22, rendering rule). It is **left as is**: the barrel says
  "feature agents EXTEND with new files, they do not edit existing ones"
  (`client/src/vendor/shared/index.ts:13-14`). The new shapes go in a new
  `contracts/onboarding.ts`, under names that don't collide (`OnboardingTour…`, `Tour…`).
- **"Indexing in progress"** has no signal today. `GET /repos/{id}/index-state` only reports
  the last finished index. The API derives it from the `jobs` table: a job of kind `clone`,
  `repo-intel-index`, `repo-intel-refresh` or `repo-intel-resync` whose `payload->>'repoId'`
  is the repo, whose status is `queued`/`running`, and whose `scheduled_at` is less than 10
  minutes ago. The window keeps rows left behind by a crash from blocking forever, because
  `jobs` has no stale-job sweep (`api/internal/jobs/jobs.go`).
- **What counts as a "completed index"** (AC-3/AC-4): all of the following hold.
  - a `repo_index_state` row exists with status `full` or `partial` and a non-empty
    `last_indexed_sha`
  - `repos.clone_path` is set and the directory has a `.git`
  - `git.HasCommit(last_indexed_sha)` is true

  A `degraded` or `failed` index answers like "not indexed".
- **Progress by polling, not SSE.** `GET /repos/{id}/onboarding` is polled every 2 s while a
  generation is `queued`/`running`. That matches `usePrRuns`, `client/docs/data-layer.md`
  "Polling". A new stream would also have to honour `Server.CloseStreams` (`api/INSIGHTS.md`
  2026-10-01).
- **Over-long model output**: reasons, titles and descriptions are clipped with "…", and lists
  are cut to their caps. **Commands are never clipped.** A run-locally step that is too long or
  spans several lines is **dropped** and counted as removed, because a truncated command is
  dangerous to copy.
- **Reading path with fewer than 3 entries.** The schema asks the model for 3–12 entries (fewer
  is invalid and goes to repair). If grounding then leaves 1 or 2, they are still shown. Only 0
  fails the section (AC-19).
- **AC-11 at shutdown**: a generation whose job context is *cancelled* (`jobs.Runner.Close`)
  stores **no** tour, even when some sections finished. Its run is marked failed with "the server
  stopped during generation", and the previous tour is kept.
- **Sidebar icon**: reuse `FileText`. No new icon is added to the design system.
- **Mutation errors**: `providers.tsx` toasts every mutation error. The page also shows
  `config_error` / `repo_not_indexed` inline (AC-33, AC-3). The duplicate toast is accepted.

## Execution mode
- Recommended: **multi**. After four independent foundation tasks (migration and queries, git
  adapter, the `tour` domain package, the client contract), the API and client tracks run in
  parallel and meet only at the contract (T4) and the e2e flow (T9).
- **User's choice: `multi`** (confirmed 2026-10-08). Run the waves in Tasks; parallel tasks
  have disjoint owned paths.

## Modules affected
- **api/**:
  - migration 0013 and the new queries
  - `internal/git`: list the tree at a commit, grep TODO/FIXME at a commit
  - a new domain package `internal/tour`: section schemas, prompts, validation, grounding, the
    secret filter, the 5-section generation
  - a new package `internal/onboarding`: one job, which is to start, track and store
    generations
  - two routes, the wiring in `cmd/api`, and the seed
- **client/**:
  - the new contract file
  - the sidebar item and the active-key fix
  - hooks
  - the `/repos/:repoId/tour` page and its components
  - strings
  - the screen spec and the route map
- **e2e/**: new flow 09, the docs and the coverage row.
- **Out of scope**:
  - regenerating one section, and cancelling a generation
  - risky-command flags, a file viewer, Share link or export
  - auto-generation, GitHub issues, new indexer languages
  - the old generic `Onboarding` contract
  - `specs/review-flow.md`, which belongs to `doc-writer`
  - the `/onboarding` add-repo screen itself: only the sidebar's active-key rule changes
  - `internal/runner`, `internal/repointel` and `internal/jobs`, which are read or used
    unchanged

## Architectural constraints
- `api/CLAUDE.md` "Conventions that differ from habit":
  - No DI container and no package-level mutable state.
  - Small interfaces live in the user's package.
  - **Domain packages never import HTTP, SQL or SDK packages.** `internal/tour` imports only
    `internal/review` (for `LLM`, `JSONRequest`, `Usage`, `Untrusted`, `JSONObject`) and the
    standard library, the same way `internal/intent` does (`api/docs/architecture.md`,
    `internal/intent` row).
  - `context.Context` comes first on I/O.
  - **Every goroutine has an owner and a way to stop.** The 5 section goroutines are owned by
    the job function and stop on its context.
  - Tests are table-driven with hand-written fakes and no mock libraries.
  - No `utils` packages, and no stutter (`tour.Generate`, `tour.Ground`,
    `onboarding.Store.Start`).
- `api/docs/go-idioms.md` "Clean architecture, kept light" and "Other rules": port behaviour,
  not structure.
- `api/CLAUDE.md` "Do not touch":
  - never hand-edit `internal/postgres/*.go`; run `make generate`
  - **migrations are append-only**: add `0013_onboarding_tour.sql` plus its `_journal.json`
    entry, and never edit `0000_init.sql`
  - existing route JSON shapes don't change
  - `internal/review/testdata/*.golden` stays untouched: this plan doesn't edit `internal/review`
- `api/specs/http-api.md` "Rules for every route":
  - workspace scoping: another workspace's repo answers 404
  - a malformed id answers 422
  - the one error envelope
  - JS time format
  - multi-statement writes are one transaction

  "Changing this contract": register the route in `Handler`, add a test next to it, update the
  spec and the Zod contract in the same change.
- `api/specs/repos-jobs.md` "Jobs": 3 at once, 2 minutes each, every job a `jobs` row under the
  runner's context.
- `api/INSIGHTS.md` entries that apply:
  - 2026-10-06 "`claude -p` flags…": every section call sets a non-empty `JSONRequest.System`
    (the anthropic provider may route through `claudecode`).
  - 2026-10-05 "A retry loop must report a cancel as ctx.Err()": the 3-attempt loop in `tour`
    checks `ctx.Err()` right after a failed attempt.
  - 2026-10-01 "A new migration fails `pgtest` TestNew": point `pgtest_test.go` at 0013.
  - 2026-10-01 "sqlc makes a computed column non-null": the index-job check returns a
    `count(*)`, and nullable values come from plain or LEFT JOINed columns only.
  - 2026-10-02 "Rows made in one transaction share `created_at`": "latest run" orders by
    `created_at DESC, id DESC`, and tests set distinct `created_at`.
  - 2026-09-30 "`make check` passes, but the database tests never ran": T1, T5 and T8 **need
    Docker**. Check that no test is `SKIP`ped.
  - 2026-09-30 "The first `make generate` takes minutes".
- `client/CLAUDE.md`:
  - "Do not touch": `@devdigest/shared` is **types only**. See also `client/INSIGHTS.md`
    2026-09-30 "Importing a value from `@devdigest/shared` breaks the build".
  - "Conventions":
    - component folder layout
    - `s` style objects with CSS variables
    - `@devdigest/ui` barrel
    - hooks only in `src/lib/hooks`
    - a new `messages/en/<feature>.json`
    - tests mock hook modules and wrap in `NextIntlClientProvider`
  - "Change with care": `src/vendor/ui/` (the sidebar nav lives in `nav.ts`).
  - "Gotchas": e2e finds elements by English text.
  - "Some copy is still hardcoded… onboarding": the existing `messages/en/onboarding.json` is a
    leftover for "later lessons". Its copy doesn't match this spec, so **leave it untouched** and
    use a new `messages/en/onboardingTour.json`.
- `client/INSIGHTS.md`:
  - 2026-09-30 "A test that passes without the API isn't testing fetch": e2e (T9) is part of
    verification.
  - 2026-10-02 "`pnpm build` breaks a running `pnpm dev`": verify with typecheck and tests.
- `e2e/CLAUDE.md`:
  - "Rules for flows": deterministic locators only, read-only, seeded data only, the next `NN-`
    number, a description, the README coverage row.
  - "Gotchas": flows depend on seed text and client copy.
- `e2e/docs/writing-flows.md` "So:": CSS-uppercased headings render in upper case for
  `wait --text`.
- Root `CLAUDE.md` "Cross-part rules": the contract and the Go change land in the same change.
  "Gotchas": the API doesn't migrate on boot.

## Approach

### Flow
1. `POST /repos/{id}/onboarding` calls `onboarding.Store.Start`. Synchronously, Start:
   1. checks the repo and the index (AC-4 → `ErrNotIndexed`)
   2. resolves the `onboarding` feature model and its client (AC-33 → `*ConfigError`)
   3. inserts a `queued` run, or finds the one in progress (AC-6)
   4. enqueues **one** background job (`jobs.Runner.Enqueue`, kind `onboarding-tour`)

   It answers 202 with the generation (AC-5).
2. The job:
   1. marks the run `running`
   2. **gathers every input up front**: the tree, docs, TODOs, map, ranks and facts at the
      run's commit, so a refresh later doesn't change what is described (spec edge case)
   3. calls `tour.Generate`, which runs the 5 sections in parallel
   4. persists each section's status as it changes (AC-8)
   5. finishes in one transaction (AC-35/36/37)
3. `GET /repos/{id}/onboarding` returns index readiness, the stored tour and the latest
   generation. The client polls it while a generation is live (AC-8/9/10).

### DB — T1 (`0013_onboarding_tour.sql`)
- `ALTER TABLE onboarding ADD COLUMN` …
  - `commit_sha text`
  - `files_indexed integer NOT NULL DEFAULT 0`
  - `index_status text`
  - `code_index_empty boolean NOT NULL DEFAULT false`
  - `provider text`, `model text`
  - `tokens_in integer`, `tokens_out integer`, `cost_usd double precision`

  `json` keeps the sections and `generated_at` keeps its meaning. The table already has
  `repo_id` as PK with an `ON DELETE cascade` FK (`0000_init.sql:205-209,385`), which gives
  AC-12's "at most one" and AC-37's delete.
- `CREATE TABLE onboarding_runs`:
  - `id uuid PK DEFAULT gen_random_uuid()`
  - `repo_id uuid NOT NULL`, FK `repos(id)` `ON DELETE cascade`
  - `workspace_id uuid NOT NULL`
  - `status text NOT NULL DEFAULT 'queued'` (`queued|running|done|failed`)
  - `error text`
  - `commit_sha text NOT NULL`
  - `files_indexed integer NOT NULL`, `index_status text NOT NULL`,
    `code_index_empty boolean NOT NULL`
  - `provider text NOT NULL`, `model text NOT NULL`
  - `sections jsonb NOT NULL`: an array of 5 `{kind, status, reason}`
  - `tokens_in integer`, `tokens_out integer`, `cost_usd double precision`
  - `created_at timestamptz NOT NULL DEFAULT now()`, `started_at timestamptz`,
    `finished_at timestamptz`

  Indexes:
  - **unique partial index** `onboarding_runs_one_live_idx ON (repo_id) WHERE status IN
    ('queued','running')`, the database-level guard for AC-6
  - btree `(repo_id, created_at)`
- Queries, in `queries/onboarding.sql`:
  - `OnboardingRepoState :one`: the repo, scoped to the workspace, LEFT JOIN
    `repo_index_state` (status, sha, files_indexed).
  - `CountActiveIndexJobs :one`: the jobs-table rule above, returning `count(*)`.
  - `InsertOnboardingRun :one`: `… ON CONFLICT (repo_id) WHERE status IN ('queued','running')
    DO NOTHING RETURNING *`. Zero rows means a run is already in progress.
  - `LiveOnboardingRun :one`, `LatestOnboardingRun :one`
  - `StartOnboardingRun :execrows`: `queued → running`
  - `SetOnboardingSection :exec`: `jsonb_set(sections, ARRAY[@idx::text], @val)`. Row-level,
    so 5 concurrent writers don't lose updates.
  - `FinishOnboardingRun :one`: `… WHERE id = @id AND status IN ('queued','running') RETURNING
    repo_id`. No row means the run was deleted (repo removed) or already failed.
  - `UpsertOnboardingTour :exec`: `INSERT … ON CONFLICT (repo_id) DO UPDATE …, generated_at =
    now()`
  - `GetOnboardingTour :one`, scoped to the workspace through `repos`.
  - `DeleteOtherOnboardingRuns :exec`: keeps only the latest run per repo.
  - `FailStaleOnboardingRuns :execrows`: `queued/running → failed`, with error "the server
    stopped during generation" (AC-11 at the next start).
  - `TopRankedFiles :many`: `file_rank` by `rank DESC`, with a limit.
  - `FileFactsWithRoutes :many`: `file_facts` rows whose `endpoints` or `crons` is non-empty,
    with a limit.

### Git — T2 (`internal/git`, through the existing `run`, `git.go:78`)
- `ListTree(ctx, dir, commit string) ([]TreeEntry, error)`, where `TreeEntry{Path string; Dir
  bool}`. It runs `git ls-tree -r -t -z --full-tree <commit>`, uses the mode/type column for
  `Dir`, and skips submodule (`commit`) entries.
- `GrepTodos(ctx, dir, commit string, max int) ([]Todo, error)`, where `Todo{Path string; Line
  int; Text string}`. It runs `git grep -n -I -z -E "\b(TODO|FIXME)\b" <commit> --`, strips
  the `<commit>:` prefix, and stops reading after `max` matches by cancelling the command. Exit
  code 1 (no matches) returns an empty slice, not an error.
- Both are read-only, so the clone's work tree never changes (AC-27). Existing `ReadBlob`
  (`git.go:153`) and `HasCommit` (`git.go:136`) are reused unchanged.

### Domain — T3 (`internal/tour`)
- **Kinds, in this order**:
  `architecture, critical_paths, run_locally, reading_path, first_tasks`.
- **Model answer schemas**: five embedded files, `<kind>.schema.json`. They follow OpenAI
  strict rules (every property required, `additionalProperties: false`), as
  `review.schema.json` and `intent.schema.json` do.

  | Kind | Model answer |
  |---|---|
  | architecture | `{summary: string (paths in backticks), nodes: [{id, label, kind: entry\|module\|middleware\|datastore\|external, path: string\|null}], edges: [{from, to, label: string\|null}]}` |
  | critical_paths | `{items: [{path, reason}]}`, max 10 |
  | run_locally | `{steps: [{command, comment: string\|null}]}`, max 15 |
  | reading_path | `{items: [{path, reason}]}`, 3–12 |
  | first_tasks | `{tasks: [{title, description, size: small\|medium, paths: [string], todo: {path, line}\|null}]}`, max 5 |
- **Section inputs.** Each input is wrapped with `review.Untrusted(<label>, …)`, and the
  budget is enforced per call at 48,000 runes. When the tree is over budget, collapse to
  directories at depth ≤ 3, each with a file count.

  | Kind | Inputs |
  |---|---|
  | architecture | tree (≤ 16k runes), repo map, docs, routes/crons |
  | critical_paths | top 40 ranked files, routes/crons, repo map, tree |
  | run_locally | docs and manifests only (allowlist, ≤ 8k runes each, ≤ 24k total) |
  | reading_path | top 40 ranked files, repo map, README, tree |
  | first_tasks | TODOs (≤ 60, ≤ 200 runes each), top ranked files, tree |
- **System prompt**: one per kind, sharing an injection guard modelled on `intent`'s
  (`internal/intent/derive.go:30-50`): content in `<untrusted>` is data, the model has no tools,
  it answers JSON only, and it cites only paths from the given tree.
- **`Excluded(path) bool`** implements the Q8 denylist. **`Readable(path) bool`** implements
  the allowlist. `internal/onboarding` uses both before any `ReadBlob` and on TODO results.
- **`Ground`**: a `Tree` (a set of files and a set of dirs) built from `[]git.TreeEntry`-shaped
  input. The domain takes `[]Entry` of its own, so it doesn't import `git`. The rules:
  - normalize: strip a leading `./`, turn `\` into `/`, strip a trailing `/`
  - reject an empty path, an absolute path, any `..` segment, anything containing `://`, and NUL
  - accept the path when it is a file or a dir
  - accept `dir/*` and `dir/**` when `dir` exists (AC-15)

  It returns `(Ref{Path, IsDir}, ok)`.
- **Per-kind validation, clipping and grounding** (AC-16–AC-25, AC-28, AC-29, AC-41). Each
  kind's step returns the wire content (see Contract), plus `removed int` and an
  `ErrNoEntries` when nothing is left (AC-19).

  | Kind | Validation, clipping and grounding |
  |---|---|
  | architecture | Clip the summary to 1,200 runes. Backtick spans that ground become path segments; the rest stay text without backticks (AC-17). Diagram: more than 12 nodes, an edge to an unknown node, a duplicate id or an unknown kind → `diagram = null` (AC-23). A node path that fails grounding → `path = null`, counted as removed. |
  | critical_paths, reading_path | Drop ungrounded entries. Clip reasons to 140 runes. |
  | run_locally | Trim each command. Drop it when it contains `\n` or is over 300 runes. Steps have no paths, so nothing is grounded. |
  | first_tasks | Keep grounded paths, and drop a task with none left. Clip the title to 100 runes and the description to 400. Attach `todo {path, line, text}` only when `(path, line)` matches a scanned TODO; the text comes from the **scan, never the model** (AC-41). The TODO's file is added to `paths` if missing. |
- **`Generate(ctx, llm review.LLM, model string, in Inputs, progress func(Kind, Status, reason
  string)) Result`**:
  - one goroutine per kind, joined with a `sync.WaitGroup`
  - each section gets `context.WithTimeout(ctx, SectionTimeout)` (90 s)
  - **at most 3 attempts**: an invalid answer gets a repair message, mirroring
    `askForReview` (`internal/review/structured.go:64-99`)
  - usage is summed over every answered attempt, failed sections included (AC-32)
  - `progress` is called with `generating` before the first attempt, and with `done` or
    `failed` at the end

  Failure reasons (AC-39):
  - `"timed out after 90s"` on `DeadlineExceeded`
  - `"model gave no valid answer in 3 attempts"`
  - `"no verifiable entries"` (AC-19)
  - the provider's error text
  - `"the server stopped during generation"` on `Canceled`

  `Result{Sections [5]SectionResult; Usage review.Usage}`.
- **AC-27 guard**: a test parses the package's non-test files with `go/parser` and fails if
  any imports `os/exec`, `net/http` or `os`. The package can't run anything.

### Orchestration — T5 (`internal/onboarding`, mirrors `repos.Store`)
- `Config{DB *pgxpool.Pool; Jobs *jobs.Runner; LLM func(provider string) (review.LLM, error);
  Log *slog.Logger; SectionTimeout time.Duration}`, and `New(cfg) *Store`.
- `Start(ctx, workspace, repoID) (Generation, error)`. Errors:
  - `ErrNotFound`
  - `ErrNotIndexed`
  - `*ConfigError{Provider string; Err error}`

  Model choice: read `settings.feature_models.onboarding` the way `runner.intentModelChoice`
  does (`internal/runner/intent.go:142-166`): workspace rows first, then per-user, a later
  match wins. Fall back to `openrouter` / `deepseek/deepseek-v4-flash`, which mirrors
  `FEATURE_MODELS` (`client/src/vendor/shared/contracts/platform.ts:44-50`). Copy those ~20
  lines; don't import `runner`. Then call `cfg.LLM(provider)` **before** inserting anything
  (AC-33).
- `Get(ctx, workspace, repoID) (View, error)`: the index readiness, the tour and the latest
  run.
- `FailStale(ctx) (int64, error)`, called from `cmd/api` next to `reviews.FailStale`.
- **Job function**:
  - Gather the inputs with `git.ListTree`, `git.ReadBlob` (capped at 64 KiB, only for
    `tour.Readable && !tour.Excluded`), `git.GrepTodos` (max 200, then filtered by
    `Excluded`), `repointel.New(db).Map`, `TopRankedFiles` and `FileFactsWithRoutes`.
  - Call `tour.Generate`, with `progress` writing `SetOnboardingSection` through a detached
    10 s context.
  - Finish in **one transaction**:
    1. `FinishOnboardingRun`; no row → stop, store nothing (AC-37).
    2. If the job context was cancelled → status `failed`, error "the server stopped during
       generation", tour untouched (AC-11).
    3. Else if ≥ 1 section is done → `done`, `UpsertOnboardingTour` with the run's
       commit/index/model/usage (AC-12, AC-35).
    4. Else → `failed`, error "all sections failed", previous tour kept (AC-36).
    5. Then `DeleteOtherOnboardingRuns`.

  Use `context.WithoutCancel` for the final write, as `jobs.finish` does
  (`internal/jobs/jobs.go:82`).

### HTTP — T8 (`internal/httpapi/onboarding.go`)
- `GET /repos/{id}/onboarding` answers 200 with `OnboardingTourResponse`. It answers 404
  `not_found` for an unknown repo or one in another workspace, and 422 for a bad id (`pathID`).
- `POST /repos/{id}/onboarding` (no body):
  - 202 `OnboardingGeneration`, for a new or an in-progress generation (AC-5, AC-6)
  - 404 for an unknown repo
  - 409 `repo_not_indexed` (AC-4)
  - 400 `config_error`, with the message `"<KEY> is not configured for provider <provider>"`
    (AC-33)
- Both routes are registered only when `Config.Onboarding != nil`, like `Repos`/`Runner`
  (`server.go:151-160`).
- `cmd/api/main.go`: build `onboarding.New(...)` with the existing `background` jobs runner and
  `reviewModel(store, modelAPIs, claude)` (`main.go:175`). Call `FailStale` at start and pass
  the store to `httpapi.Config.Onboarding`.
- **Seed**: add `demoTour` to `internal/seed/seed.go`, called from `demo()` **before** the
  `SeedPullExists` early return (`seed.go:124`), so it's idempotent on its own
  (`UpsertOnboardingTour` only when `GetOnboardingTour` finds no row). Content is built from
  `tour` wire types and marshalled:
  - commit `a1b2c3d4e5f6`, files_indexed **42**, index_status `full`
  - provider `openrouter`, model `deepseek/deepseek-v4-flash`, cost `0.0042`
  - 5 done sections citing `README.md`, `package.json`, `src/middleware/ratelimit.ts`,
    `src/config.ts`, `src/api/users.ts`, `src/api/public/webhooks.ts`
  - run_locally steps: `pnpm install`, `cp .env.example .env` (comment "then fill in the
    keys"), `docker compose up -d postgres`, `pnpm dev`
  - one first task with a TODO in `src/middleware/ratelimit.ts`
  - a 5-node diagram

### Contract — T4 (`client/src/vendor/shared/contracts/onboarding.ts`, the single source of the JSON)
- Enums:
  - `TourSectionKind = z.enum(['architecture','critical_paths','run_locally','reading_path','first_tasks'])`
  - `TourSectionStatus = z.enum(['pending','generating','done','failed'])`
  - `TourDiagramNodeKind = z.enum(['entry','module','middleware','datastore','external'])`
- `TourPathRef = z.object({ path: z.string(), is_dir: z.boolean() })`
- `TourSummarySegment = z.discriminatedUnion('type', [z.object({type: z.literal('text'), text:
  z.string()}), TourPathRef.extend({type: z.literal('path')})])`
- `TourDiagram = z.object({ nodes: z.array(z.object({ id, label: z.string(), kind:
  TourDiagramNodeKind, path: z.string().nullable(), is_dir: z.boolean() })), edges:
  z.array(z.object({ from, to: z.string(), label: z.string().nullable() })) })`
- Section content, one per kind:
  - `ArchitectureContent = { summary: TourSummarySegment[], diagram: TourDiagram.nullable() }`.
    `null` renders "Diagram unavailable".
  - `PathListContent = { items: (TourPathRef & {reason: string})[] }`, for critical_paths and
    reading_path.
  - `RunLocallyContent = { steps: {command: string, comment: string|null}[] }`
  - `FirstTasksContent = { tasks: {title, description: string, size: 'small'|'medium', paths:
    TourPathRef[], todo: {path: string, line: number, text: string}|null}[] }`
- `TourSection = z.discriminatedUnion('kind', [...])`. Each entry is `{kind: literal, status:
  'done'|'failed', reason: string|null, removed_refs: number, content: <Content>.nullable()}`.
- `OnboardingTour = { repo_id, commit_sha: string, files_indexed: number, index_status:
  'full'|'partial', code_index_empty: boolean, generated_at: string, provider: string, model:
  string, cost_usd: number|null, sections: TourSection[] }`
- `OnboardingGeneration = { id, repo_id, status: 'queued'|'running'|'done'|'failed', error:
  string|null, commit_sha: string, created_at: string, finished_at: string|null, sections:
  {kind: TourSectionKind, status: TourSectionStatus, reason: string|null}[] }`
- `RepoIndexReadiness = { state: 'ready'|'indexing'|'not_indexed', commit_sha: string|null,
  files_indexed: number, partial: boolean, code_index_empty: boolean }`
- `OnboardingTourResponse = { index: RepoIndexReadiness, tour: OnboardingTour.nullable(),
  generation: OnboardingGeneration.nullable() }`

The Go JSON structs in T3/T5/T8 match these field for field. T3 owns the content and section
types (`json` tags), T5 the generation/view, T8 the response envelope.

### Client — T6 (sidebar) and T7 (page)
- **T6**:
  - `nav.ts` adds `{ key: "onboarding-tour", label: "Onboarding Tour", icon: "FileText",
    href: "/repos/:repoId/tour" }` to WORKSPACE.
  - In `activeKeyFor` (`client/src/components/app-shell/helpers.ts:29`), replace
    `pathname.includes("/onboarding")` with `/^\/repos\/[^/]+\/tour(\/|$)/`, so `/onboarding`
    (add repo) returns `""` (AC-1).
- **T7, hooks**: in `src/lib/hooks/onboarding.ts`:
  - `useOnboardingTour(repoId)`: key `["onboarding-tour", repoId]`; `refetchInterval` 2000
    while `generation?.status` is `queued`/`running`, else `false`.
  - `useGenerateTour(repoId)`: `POST`. `onSuccess` invalidates `["onboarding-tour", repoId]`.
    `isPending` disables the buttons (AC-34).
- **T7, page**: `src/app/repos/[repoId]/tour/page.tsx` stays thin, with `RepoNotFound` for an
  unknown repo, as the pulls page does. Below it, `_components/TourView/` with sub-components:
  - `TourHeader`:
    - "Onboarding for {full_name}"
    - "Generated from index of {N} files · generated {relativeTime}", reusing `relativeTime`
      from `src/app/repos/[repoId]/pulls/helpers.ts:11`
    - the short SHA (7 characters), the model
    - the cost via `formatCost` (`src/lib/format-cost.ts`)
    - the partial or empty-index note (AC-31, Q5)
    - Regenerate, and no Share (AC-42)
  - `StaleNotice` (AC-14), `IndexNotice` (AC-3), `GenerationBanner` (AC-9, AC-36, AC-33 with a
    link to `/settings/api-keys`), `EmptyState` (AC-2), `OnThisPage` (AC-38, using
    `IntersectionObserver` with a guard for jsdom)
  - `SectionCard`: a `<button aria-expanded>` header, expanded by default, with the status as
    text (AC-8, accessibility) and "{n} unverified references removed" (AC-18)
  - `ArchitectureSection` with `ArchitectureDiagram`: an SVG/CSS box layout in columns by
    kind, with a color per kind from CSS variables and a text list of nodes (with their kind)
    and edges (AC-21, accessibility)
  - `PathListSection`, for critical paths and the reading path
  - `RunLocallySection`: a `CopyButton` using `navigator.clipboard.writeText`, with
    "Copied" / "Copy failed" and the accessible name "Copy command: {command}"; plus the fixed
    note (AC-25–AC-27)
  - `FirstTasksSection`
- **"Open"** (AC-30): add `githubPathUrl(fullName, sha, path, isDir)` to `src/lib/github-urls.ts`.
  It builds `blob|tree/<sha>/<encPath>`, with `target="_blank" rel="noopener noreferrer"`.
- **Rendering rule**: all model text is React text children. No `Markdown` primitive, no
  `dangerouslySetInnerHTML`, and no href except `githubPathUrl` over a grounded `TourPathRef`.
- **Copy**: `messages/en/onboardingTour.json`. These strings are fixed because e2e relies on
  them:
  - "Onboarding Tour"
  - "Onboarding for {name}", "Generated from index of {count} files"
  - "Architecture overview", "Critical paths", "How to run locally", "Guided reading path",
    "First tasks", "On this page"
  - "Generate tour", "Regenerate"
  - "Repository not indexed yet", "Repository is still being indexed"
  - "Generated from repository files — review before running."

## Tasks

The waves, which run-plan derives from Depends-on:
1. **T1, T2, T3, T4, T6**
2. **T5, T7**
3. **T8**
4. **T9**

### T1 — Migration 0013 and onboarding queries
- Action: Add `0013_onboarding_tour.sql` (Approach → DB) and its `_journal.json` entry (idx 13,
  a `when` later than 0012's). Add `queries/onboarding.sql` with every query listed, then run
  `make generate`. Point `pgtest_test.go` at 0013's effect: the `onboarding_runs` table exists.
- Module: api
- Type: migration
- Skills to use: postgresql-table-design
- Owned paths: `api/migrations/0013_onboarding_tour.sql`, `api/migrations/meta/_journal.json`, `api/internal/postgres/queries/onboarding.sql`, `api/internal/postgres/*.go` (generated only), `api/internal/pgtest/pgtest_test.go`
- Depends-on: —
- Known gotchas:
  - `api/CLAUDE.md` "Do not touch" (append-only migrations; generated Go).
  - `api/INSIGHTS.md` 2026-10-01 "A new migration fails `pgtest` TestNew".
  - `api/INSIGHTS.md` 2026-10-01 "sqlc makes a computed column non-null".
  - `api/INSIGHTS.md` 2026-09-30 "The first `make generate` takes minutes".
  - The partial-index `ON CONFLICT` target must repeat the index predicate exactly.
  - **Needs Docker.**
- Acceptance: AC-6 (DB guard), AC-12 (every stored field has a column), AC-37 (cascade).
  Proven by `go test -race ./internal/pgtest/...` with Docker, not SKIPped, plus `go build
  ./...`.

### T2 — Git: tree listing and TODO/FIXME grep at a commit
- Action: Add `git.ListTree` and `git.GrepTodos` (Approach → Git). Tests build a temp repo in
  the test with nested dirs, a submodule-free tree, files containing `TODO`/`FIXME`, a binary
  file, and more matches than `max`. They assert that a commit other than HEAD is read from the
  object store, and that the work tree is unchanged afterwards (`git status --porcelain`
  empty).
- Module: api
- Type: domain (adapter)
- Skills to use: security (paths read from an untrusted repo). Go conventions: none
  (api/CLAUDE.md, api/docs/go-idioms.md).
- Owned paths: `api/internal/git/**`
- Depends-on: —
- Known gotchas:
  - `api/INSIGHTS.md` 2026-09-30 "A GitHub token saved inside an old clone": never pass a token
    to these read-only commands.
  - Use `--end-of-options`/`--` so a path or commit can't be read as a flag (as `FetchCommit`
    does).
- Acceptance: AC-15 (the tree that grounding uses lists files and directories), AC-27 (no
  change in the clone), AC-40 (TODOs come from the clone, with no GitHub call). Proven by
  `go test -race ./internal/git/...`.

### T3 — Domain package `internal/tour`
- Action: Implement Approach → Domain in full:
  - the kinds, the five embedded schemas and the system prompts
  - `Excluded`/`Readable`, the input builders with the budget, `Ground`
  - per-kind parse, validate, clip and ground
  - `Generate` with parallel sections, the time limit, 3 attempts, progress and summed usage
  - the wire content types with `json` tags matching T4
  - the AC-27 import guard test

  Tests are table-driven with a hand-written fake `review.LLM` (scripted answers per
  `SchemaName`, a blocking one for timeouts, `CostUSD` nil/non-nil). They cover:
  - grounding: `./README.md`, `a\b.go`, `dir/**`, `../x`, `/etc/passwd`, `https://x`, made-up
    paths
  - AC-16/17/18/19 per kind
  - diagram invalidation (AC-23)
  - dropping long or multi-line commands
  - TODO matching from the scan only (AC-41)
  - a repair succeeding on attempt 3, and failing after 3 (AC-39)
  - one section's error not stopping the others (AC-7)
  - usage summing over failed sections, and null when none reported (AC-32)
  - every request has a non-empty `System`, and repo text sits inside `<untrusted …>`
- Module: api
- Type: domain
- Skills to use: security (untrusted repository text into a prompt; model output rendered later).
  Go conventions: none (api/CLAUDE.md, api/docs/go-idioms.md).
- Owned paths: `api/internal/tour/**`
- Depends-on: —
- Known gotchas:
  - `api/CLAUDE.md` "Conventions": imports only `internal/review` and the stdlib, owned
    goroutines.
  - `api/INSIGHTS.md` 2026-10-06 "`claude -p` flags…" (always `System`).
  - `api/INSIGHTS.md` 2026-10-05 "A retry loop must report a cancel as ctx.Err()".
  - `internal/openai/openai.go` strict mode needs every property required, so nullable fields
    are `["string","null"]`, not optional.
  - Uses the exported `review.Untrusted` / `review.JSONObject` (`internal/review/prompt.go:148`,
    `structured.go:120`) without editing `internal/review`.
- Acceptance: AC-7, AC-15–AC-25, AC-27 (no exec), AC-28, AC-29, AC-32 (usage sum), AC-39,
  AC-40, AC-41. Proven by `go test -race ./internal/tour/...`, with no DB.

### T4 — Shared contract for the tour
- Action: Create `contracts/onboarding.ts` exactly as in Approach → Contract. Add `export * from
  './contracts/onboarding.js';` to the barrel, and the index comment line. Re-export the new
  types (`OnboardingTourResponse`, `OnboardingTour`, `OnboardingGeneration`, `TourSection`,
  `TourSectionKind`, `TourSectionStatus`, `TourPathRef`, `TourSummarySegment`, `TourDiagram`,
  `RepoIndexReadiness`, the content types) from `src/lib/types.ts`. Don't touch `knowledge.ts`.
- Module: client
- Type: contract
- Skills to use: zod, typescript-expert
- Owned paths: `client/src/vendor/shared/contracts/onboarding.ts`, `client/src/vendor/shared/index.ts`, `client/src/lib/types.ts`
- Depends-on: —
- Known gotchas:
  - `client/CLAUDE.md` "Do not touch" (types only; the contract changes with the Go API).
  - `client/INSIGHTS.md` 2026-09-30 "Importing a value from `@devdigest/shared` breaks the build".
  - The `.js` import suffix in the barrel.
  - No export name may collide with `knowledge.ts` (`Onboarding*` without `Tour` is taken).
- Acceptance: AC-12, AC-21, AC-22 (diagram as structured nodes and edges), AC-5 (generation
  shape). Proven by `pnpm typecheck`.

### T5 — Orchestration package `internal/onboarding`
- Action: Implement Approach → Orchestration: `Config`, `New`, `Start`, `Get`, `FailStale`, the
  job function (gather → `tour.Generate` → per-section progress → finish transaction) and the
  typed errors.

  Tests run on `pgtest` with a fake LLM and a real temp git repo as the clone. They cover:
  - AC-4: no clone, no index row, a degraded index, an index job running → `ErrNotIndexed`
    without inserting
  - AC-33: the LLM resolver errors → `*ConfigError`, with no run row and no model call
  - AC-6: two concurrent `Start`s → one run, one job, and exactly one set of LLM calls,
    counted in the fake
  - AC-35/36: partial success replaces the tour, while all-failed keeps the old one
  - AC-37: delete the repo while the fake LLM blocks → no tour row afterwards
  - AC-11: close the jobs runner mid-generation → the run fails with "the server stopped
    during generation" and the old tour is kept; `FailStale` flips a leftover `running` row
  - AC-40: `.env` contents and `id_rsa` never appear in any request (Q8)
  - AC-27: the clone's `git status --porcelain` is unchanged
- Module: api
- Type: domain (orchestration)
- Skills to use: security (secret files, untrusted repo content). Go conventions: none
  (api/CLAUDE.md, api/docs/go-idioms.md).
- Owned paths: `api/internal/onboarding/**`
- Depends-on: T1, T2, T3
- Known gotchas:
  - `api/specs/repos-jobs.md` "Jobs": a 2-minute cap, and every job runs under the runner's
    context.
  - `api/INSIGHTS.md` 2026-10-02 "Rows made in one transaction share `created_at`".
  - `api/INSIGHTS.md` 2026-10-01 "A cancelled run never gets its outcome columns": the analogous
    trap is that the finish UPDATE filters on live status, so a stale-failed run is never
    overwritten, by design.
  - `api/INSIGHTS.md` 2026-09-30 "`make check` passes, but the database tests never ran":
    **needs Docker**.
- Acceptance: AC-4, AC-5, AC-6, AC-7, AC-8 (persisted statuses), AC-10, AC-11, AC-12, AC-33
  (API side), AC-35, AC-36, AC-37, AC-40. Proven by `go test -race ./internal/onboarding/...`
  with Docker, not SKIPped.

### T6 — Sidebar item and active-key fix
- Action: Add the "Onboarding Tour" item to `NAV` WORKSPACE (Approach → Client). Fix
  `activeKeyFor`. Add `helpers.test.ts` with cases:
  - `/onboarding` → `""`
  - `/repos/x/tour` → `"onboarding-tour"`
  - `/repos/x/pulls` → `"pulls"`
  - `/repos/x/pulls/482` → `"pulls"`
- Module: client
- Type: component
- Skills to use: react-best-practices, react-testing-library
- Owned paths: `client/src/vendor/ui/nav.ts`, `client/src/components/app-shell/helpers.ts`, `client/src/components/app-shell/helpers.test.ts`
- Depends-on: —
- Known gotchas:
  - `client/CLAUDE.md` "Change with care" (`src/vendor/ui/`; the nav lives in `nav.ts`).
  - `resolveHref` fills `:repoId` with `"_"` when no repo is active (`nav.ts:66-69`), and the
    page must render `RepoNotFound` for it.
- Acceptance: AC-1. Proven by `pnpm exec vitest run src/components/app-shell` and
  `pnpm typecheck`.

### T7 — Tour page, hooks and copy
- Action: Add `src/lib/hooks/onboarding.ts` and its barrel export, and `githubPathUrl` in
  `src/lib/github-urls.ts`. Build the page and the components in Approach → Client, with
  `messages/en/onboardingTour.json`. Add `client/specs/onboarding-tour.md` (the Must list,
  plus "Relied on by e2e") and the route-map row and diagram node.

  Component tests mock `@/lib/hooks/onboarding` and wrap in `NextIntlClientProvider`. They
  cover:
  - the empty state (AC-2)
  - disabled buttons with the reason (AC-3) and while pending or running (AC-34)
  - per-section statuses (AC-8)
  - the previous tour shown with a "being regenerated" marker (AC-9)
  - the stale notice with both short SHAs (AC-14)
  - the partial and empty-index notes (AC-31)
  - the header text (AC-13)
  - the cost "—" / "$0.00" (AC-32)
  - `config_error` with the Settings link (AC-33)
  - "Generation failed" with reasons (AC-36)
  - section failure reasons (AC-35, AC-39) and the removed count (AC-18)
  - text vs chip segments (AC-17, AC-20)
  - "Diagram unavailable" (AC-23) and the node kinds in text (AC-21)
  - Open hrefs for blob and tree with encoding (AC-30)
  - copy calling `clipboard.writeText` with the exact text, "Copied", and "Copy failed" on
    rejection (AC-26)
  - the fixed note (AC-27)
  - collapse with `aria-expanded` (AC-38)
  - "On this page" listing 5 entries (AC-38)
  - no Share link (AC-42)
  - model text containing `<b>`/`[x](javascript:…)` rendering literally
- Module: client
- Type: component
- Skills to use: next-best-practices, react-best-practices, react-testing-library, typescript-expert
- Owned paths: `client/src/lib/hooks/onboarding.ts`, `client/src/lib/hooks/index.ts`, `client/src/lib/github-urls.ts`, `client/src/app/repos/[repoId]/tour/**`, `client/messages/en/onboardingTour.json`, `client/specs/onboarding-tour.md`, `client/docs/route-map.md`
- Depends-on: T4
- Known gotchas:
  - `client/CLAUDE.md` "Conventions" (folder layout, `s` styles, URL vs React state: collapse
    state is React state, as in `plans/smart-diff.md` q15).
  - "Gotchas" (e2e relies on the copy).
  - `client/docs/data-layer.md` "Polling" and "Errors in the UI".
  - Don't wire in or edit `messages/en/onboarding.json` (`client/CLAUDE.md` "Gotchas").
  - `client/INSIGHTS.md` 2026-10-02 (no `pnpm build`).
- Acceptance: AC-2, AC-3, AC-8, AC-9, AC-10 (reads the server state on mount), AC-13, AC-14,
  AC-17, AC-18, AC-20–AC-21, AC-23–AC-30, AC-31–AC-36 (UI side), AC-38, AC-39 (shown), AC-42.
  Proven by `pnpm typecheck && pnpm exec vitest run src/app/repos/\[repoId\]/tour src/lib`.

### T8 — HTTP routes, wiring, seed and API specs
- Action: Add `internal/httpapi/onboarding.go` with both routes (Approach → HTTP) and
  `onboarding_test.go`. Register the routes in `Handler` behind `Config.Onboarding`, and add the
  field to the test fixture. Wire `cmd/api` (`onboarding.New`, `FailStale` at start, the
  config). Add `demoTour` to the seed, with a seed test that it's idempotent across two `Run`s
  and parses as `tour` types.

  Update `api/specs/http-api.md`:
  - the route rows under "Repositories", including 409 `repo_not_indexed` and 400
    `config_error`
  - the route count
  - "Registered only when configured"

  Update `api/specs/repos-jobs.md` "Jobs" (the `onboarding-tour` kind: 5 parallel calls in one
  slot, failed at shutdown and at start).

  Handler tests cover:
  - 404 for an unknown or other-workspace repo, 422 for a bad id
  - 409 on the seeded repo
  - 400 `config_error` naming the provider
  - 202 twice → the same generation id
  - GET → `null`/`null` and `not_indexed` on a fresh repo
  - GET on the seed → the seeded tour JSON
- Module: api
- Type: route
- Skills to use: security (httpapi). Go conventions: none (api/CLAUDE.md).
- Owned paths: `api/internal/httpapi/onboarding.go`, `api/internal/httpapi/onboarding_test.go`, `api/internal/httpapi/server.go`, `api/internal/httpapi/httpapi_test.go`, `api/cmd/api/main.go`, `api/internal/seed/seed.go`, `api/internal/seed/seed_test.go`, `api/specs/http-api.md`, `api/specs/repos-jobs.md`
- Depends-on: T4, T5
- Known gotchas:
  - `api/specs/http-api.md` "Rules for every route" and "Changing this contract".
  - `api/INSIGHTS.md` 2026-09-30 "The API won't start by hand" (smoke testing).
  - `api/INSIGHTS.md` 2026-10-02 "Rows made in one transaction share `created_at`" (the seed is
    one transaction).
  - `e2e/CLAUDE.md` "Gotchas": the seed text is relied on by flow 09.
  - **Needs Docker.**
- Acceptance: AC-4, AC-5, AC-6 (HTTP), AC-11 (start-up sweep wired), AC-12/AC-13 (seeded file
  count and SHA served), AC-33. Proven by `go test -race ./internal/httpapi/...
  ./internal/seed/... ./cmd/api/...` with Docker, then `make check`.

### T9 — e2e flow 09: the seeded onboarding tour
- Action: Add `e2e/specs/09-onboarding-tour.flow.json`. Steps:
  1. open `{BASE}/`, then `wait --url /pulls`
  2. `find text "Onboarding Tour" click`, then `wait --url /tour`
  3. `wait --load networkidle`
  4. `wait --text "Onboarding for acme/payments-api"` and `"Generated from index of 42 files"`
  5. wait for each of the five section titles, **in their rendered case**
  6. `"Repository not indexed yet"`
  7. `find role button --name "Copy command: pnpm install"` exists, without clicking it, so the
     flow stays read-only and doesn't depend on the clipboard

  Add the row to `docs/writing-flows.md`'s table and to the README coverage table.
- Module: e2e
- Type: flow
- Skills to use: none (e2e/docs/writing-flows.md)
- Owned paths: `e2e/specs/09-onboarding-tour.flow.json`, `e2e/docs/writing-flows.md`, `e2e/README.md`
- Depends-on: T6, T7, T8
- Known gotchas:
  - `e2e/CLAUDE.md` "Rules for flows" (deterministic locators, read-only, never the `chat`
    command).
  - `e2e/docs/writing-flows.md` "So:" (CSS-uppercased titles render upper case).
  - `e2e/INSIGHTS.md` 2026-09-30 "A flow breaks after a change outside e2e/".
  - Root `INSIGHTS.md` 2026-09-30 "e2e.sh brings the stack up, then every flow fails"
    (`agent-browser` must be installed).
- Acceptance: AC-1 (sidebar navigation), AC-13 (seeded header), AC-38 (five sections
  rendered), AC-42 (indirectly, by the header rendering), plus the seed working without any
  key. Proven by `./scripts/e2e.sh`, all flows green.

## Verification
| Part | Command | Passing looks like |
|---|---|---|
| api (scoped, per task) | from `api/`: `gofmt -l .` (empty) · `go build ./... && go vet ./...` · `go test -race ./internal/git/... ./internal/tour/...` (no DB) · `go test -race -v ./internal/pgtest/... ./internal/onboarding/... ./internal/httpapi/... ./internal/seed/...` (**Docker running**; grep the output for `SKIP`: there must be none) | all green, no SKIP |
| api (full, needed: a migration) | `make check` with Docker running | gofmt, vet, staticcheck and `go test -race` green |
| client | from `client/`: `pnpm typecheck && pnpm test` | green |
| e2e | from the repo root: `./scripts/e2e.sh` | flows 01–09 green |

Manual checks, which `plan-verifier` marks "needs a human". They use a real imported and
indexed repo with an OpenRouter key:
1. Generate a tour. Watch the 5 sections go pending → generating → done/failed with no reload,
   reload mid-way, and see the same state (AC-8, AC-10).
2. Double-click Generate: one row appears in `onboarding_runs` and one job in `jobs` (AC-6,
   AC-34).
3. Click "Open" on a file and on a directory: the GitHub `blob`/`tree` URL opens at the tour's
   SHA (AC-30).
4. Copy a command, then paste it: the exact text, and "Copied" shows (AC-26).
5. Resync the repo to a new commit: the stale notice names both SHAs, and nothing regenerates
   on its own (AC-14).
6. Remove the OpenRouter key: Generate shows `config_error` with the Settings → API Keys link
   (AC-33).
7. Stop the API (Ctrl-C) mid-generation and restart: the generation shows as failed with "the
   server stopped during generation", and the previous tour is still there (AC-11).
8. Generate on a Go-only repository (Q5): the "Code index is empty" note shows, and the
   sections cite real files.
9. Keyboard: Tab to a section header, then Enter/Space toggles it, and `aria-expanded`
   changes (accessibility).
