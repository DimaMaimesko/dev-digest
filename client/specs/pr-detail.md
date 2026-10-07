# Spec — PR detail (`/repos/:repoId/pulls/:number`)

The main screen of the product: read a PR, run agents on it, act on their findings.

## Loading
- `:number` is resolved to the PR's UUID through the cached PR list, then
  `GET /pulls/:id` loads the detail. Failure → full-screen `ErrorState` with a retry.
- The breadcrumb is repo → Pull Requests → `#number`. The header links to the PR on GitHub.

## Tabs (`?tab=`)
- **Overview** (`overview`, default): the Intent panel, then the PR description rendered as
  Markdown.
- **Agent runs** (`findings`): the reviews, the live runs and the run history.
- **Files changed** (`diff`): a GitHub-like diff per file. Inline review comments can be read
  and, **only while the PR is open**, posted (`POST /pulls/:id/comments`).

## Running a review
- The run dropdown lists **every** agent, disabled ones marked "disabled"; any of them can be
  run on its own. "Run all" runs only the **enabled** ones.
- Starting a run switches to the Agent runs tab. Whether runs are live comes from the server
  (`/runs/active`), so a reload or another tab still shows them.
- A live run streams its log over SSE (`RunStatus`). An `error` event raises a toast.
- A live run can be cancelled. When runs settle, the reviews and the run history reload
  without a page reload.

## Reviews and findings
- One accordion per review, newest first; the newest is open by default. Its header shows the
  agent, the verdict and the finding count.
- **Verdict banner**: request changes · approve · comment, with the summary, the finding and
  blocker counts and the 0–100 score.
- **Findings** sorted by severity: critical, warning, suggestion, info. "Hide low confidence"
  hides findings with confidence below 0.65.
- Each finding shows severity, category, `file:line` (linked to GitHub at the head commit),
  confidence, the rationale and a suggestion as Markdown, and **Accept** / **Dismiss**.
  The state shown comes from `accepted_at` / `dismissed_at` saved on the server.
- Keyboard: `j` / `k` move between findings; `a` accepts and `d` dismisses the focused one.
- A review can be deleted, after a confirmation (its run stays in the history).

## Intent panel (Overview tab)
- `GET /pulls/:id/intent` (`usePrIntent`, key `["pr-intent", prId]`) loads the PR's stored
  intent (Intent Layer). The panel sits above the description, always shown.
- With a stored intent: the statement, "In scope" / "Out of scope" lists, a "Confidence: {level}"
  badge (high / medium / low, the level always carried as text), "Sources" and, when any,
  "Unresolved references".
- With no stored intent: "Intent is derived on the next review." — no error.
- Every field and source label is plain React text: no Markdown rendering, no
  `dangerouslySetInnerHTML`, no `<a>`/`<img>` built from intent data — the intent is untrusted,
  model-produced content.
- A run settling invalidates `pr-intent` alongside `pr-runs`, since that run may have derived or
  reused the intent.

## Intent in the run trace drawer
- When the trace has an `intent` object, the drawer shows an Intent section in `TraceBody`:
  whether it was derived or reused for this run (or "No intent — {reason}" on failure), its
  confidence, sources and unresolved references, and the provider/model used.
- The prompt assembly view gets a `PromptBlock` for `prompt_assembly.intent` (the untrusted
  `pr-intent` section sent to the review model), rendered as text like the other prompt blocks.
- An older trace with no `intent` field hides the section entirely.

## Run history and trace
- The history lists every run (done, failed with its error, cancelled, running) interleaved
  with the PR's commits, newest first, so it's clear which commit each run reviewed.
- An ended run (done, failed or cancelled) shows its cost beside its time; a running run, or
  one whose cost is unknown, shows none.
- Deleting a run asks for confirmation and deletes its review too.
- Clicking a run opens the trace drawer (`?trace=<runId>`): configuration, stats (duration,
  tokens, findings, cost — "—" when unknown), prompt assembly, tool calls, raw output, and the
  live log with a filter.

## Relied on by e2e
Flow 04 (Agent runs tab: "request changes", "2 findings", the finding
"Hardcoded Stripe secret key in commit", the seeded run's cost "$0.0021" and the trace's
"COST" stat), flow 05 (Files changed tab: `src/config.ts`), and flow 08 (Overview tab's Intent
panel on the seeded PR #482: "Intent", "Confidence: medium", "per-client rate limiting" and
"Out of scope").
