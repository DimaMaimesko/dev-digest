# Spec — a review run

`POST /pulls/{id}/review` → `internal/runner` → `internal/review.Run` → a saved review.
What must hold; the code is the reference for how.

## Starting
- One run per requested agent is created in one transaction, and the request answers at
  once with `[{runId, agentId, agentName}]`.
- The runs of one request execute **one after another** in the background. The diff is
  loaded once for all of them.

## Inputs
- **Diff:** `git diff <base>..<head sha>` in the repository's clone. If that fails or is
  empty, the patches saved from GitHub (files without a patch — binary or too big — are skipped).
- **Prompt:** the agent's system prompt, a task line naming the PR, and the PR's context.
  PR text (title, description, …) is untrusted: it is wrapped in an `<untrusted>` block, and
  any closing tag inside it is escaped in any letter case and spacing. The description is cut
  at 4000 characters, between characters, never inside one.
- **Skills:** the agent's linked skills that are enabled and not blank, in the agent's order,
  each as `## <name>` then its body, under `## Skills / rules`. Trusted (written in the app),
  so not wrapped. The current body is used; the trace keeps the exact text sent. Loading them
  failing fails the run. The log says `skills: N attached (names)` or `skills: none attached`.
- **Repo-intel context**, when on globally (`REPO_INTEL_ENABLED`, default on) and for the agent
  (`repo_intel`): the repository map (1500 tokens), up to 10 callers of the symbols the change
  declares, and a note when changed files are among the most depended-on (95th rank percentile).
  Context made only of whitespace is left out of both the prompt and the trace.
- The assembled prompt must stay byte-for-byte what `internal/review/testdata/*.golden`
  holds. A change there is a deliberate prompt change, reviewed as such.

### Intent
- Before the job loop, `execute` derives the pull request's intent **at most once per
  request** (`internal/runner/intent.go`, `intentFor`): what it's for, what's in scope and
  what's out of scope, derived from its title, description, linked issues, linked plan/spec
  documents, and (when none of those say enough) its branch, commit subjects and changed
  files (`internal/intent`).
- **Reuse:** when the stored `pr_intent` row's fingerprint (`sha256(head_sha + title + body)`)
  still matches, it is reused at no cost. Otherwise the model named by
  `settings.feature_models.review_intent` (default `anthropic`/`haiku`) derives a new one,
  which is then stored, replacing the old row. A failure leaves the stored row untouched.
- **Context:** derivation runs under the request's trigger run — the first not-yet-cancelled
  run — with a 60 s timeout (`Config.IntentTimeout`). Cancelling the trigger run stops the
  call; the request's other runs then review without intent too, so intent is derived at most
  once even on failure.
- **Prompt:** when there is an intent, every job's prompt gets a `## PR intent` section right
  after the PR description, the same untrusted wrapping as the rest of the PR's text
  (`internal/review.Prompt.Intent` / `Assembly.Intent`). No intent, no section — byte-identical
  to a prompt without this feature.
- **Live log**, exactly one line per run, shared across every run of the request:
  `intent: derived (confidence <level>, <n> source(s), <m> unresolved)`, `intent: reused`, or
  `intent: failed — <reason>; reviewing without intent` (reasons: the provider/key error; `run
  cancelled`; `timed out after <d>`; `model gave no valid intent in 2 attempts`; the model's
  own error).
- **Trace:** every run's trace has an `intent` object (`status`, `reason`, `confidence`,
  `sources`, `unresolved`, `provider`, `model`), even a failed run's, and `prompt_assembly.intent`
  when one was sent (null on failure, since no prompt for the next call was assembled).
- A missing `Config.IntentLLM` (only in tests) skips intent entirely, without a log line, so
  the rest of this spec holds unchanged.

## The model call
- **Strategy:** `auto` (default) sends the whole diff in one call, unless it changes more than
  400 lines across several files; then one call per file. `single-pass` and `map-reduce` force
  one or the other. Per-file calls run one after another.
- The model must answer the review JSON schema (`internal/review/review.schema.json`). An
  invalid answer is retried up to 2 more times, by `review`, not by the provider adapters.
- Anthropic: the review is the input of a tool the model must call. Models that refuse a
  temperature or a forced tool don't get them.

## The grounding gate
- A finding is kept only if its file is in the diff and its line range overlaps a line one of
  that file's hunks shows. **No exception for any `kind`.**
- In per-file mode, a finding about a file other than the one that call reviewed is dropped.
- Dropped findings and their reasons go to the run trace ("3/4 passed").

## Scoring
- The score is computed from the kept findings, never taken from the model:
  100 − 35 per critical − 12 per warning − 3 per suggestion, floored at 0.
- Blockers are the findings at or above the agent's `ci_fail_on` (`any`, `warning`,
  `critical`; `never` means none).

## Saving
- The review, its findings and the trace are saved in one transaction, **only if the run is
  still running**, checked with its row locked. A run cancelled or deleted meanwhile keeps nothing.

## Cost
- A run's cost (`agent_runs.cost_usd`, the trace's `stats.cost_usd`) is the sum, in US dollars,
  of what the provider billed for **every answered model call**: per-file calls and repair
  retries included. Only OpenRouter reports it (`usage.cost`, asked for with `usage.include`);
  for other providers it is `NULL`, shown as "—". `0` is a free model, not unknown.
- **Failed and cancelled runs keep what they spent**: tokens and cost of the calls answered
  before the failure. A call that errors has no answer, so its cost can't be counted.
- `FinishRun` skips a run already marked cancelled, so its usage is saved by `RecordRunUsage`,
  as is the usage of a run cancelled while its model answered (its review is discarded).
- A PR's cost in the list is the sum over all its runs, `NULL` when no run's cost is known.
- **Intent's cost is the trigger run's**: its tokens and cost (when derived, not reused) are
  added to that one run's own usage, in `agent_runs` and in its trace's `stats`, on every exit
  path — success, failure, cancelled before its turn, and cancelled during the intent call
  itself. The request's other runs carry none of it.

## Live log and cancel
- `GET /runs/{id}/events` streams events `{runId, seq, kind, msg, t}` as SSE, earlier ones
  first. Kinds: `info`, `tool` (a call to git or a model), `result`, `error`.
- The log lives in memory; a finished run's stays 10 minutes, then only the trace has it.
  An unknown run's stream ends at once. Every open stream ends as shutdown starts
  (`Server.CloseStreams`, registered with `RegisterOnShutdown`): `Shutdown` waits for every
  request, and a stream would otherwise last until its run ends.
- Cancel stops the model call at once through `ctx`. Only the workspace's own runs can be cancelled.
- When the server stops, its running runs are marked failed ("the server stopped during the
  run"). At start, runs left "running" by a crashed server are marked failed too.

## Not in the starter
The lethal-trifecta fields (`trifecta_components`, `evidence`) stay in the schema but are
ignored; they come back in a later course lesson. Cost is only what the provider reports:
no price table, no estimate (`../../specs/run-cost.md`).
