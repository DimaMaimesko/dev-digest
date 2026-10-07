# Spec — Smart Diff
Status: draft
Changes: client/specs/pr-detail.md (Files changed tab, Running a review, "Relied on by e2e"); e2e coverage of the Files changed tab (flow 05 or a new flow)

## Problem & Motivation
The Files changed tab of a pull request lists files in one flat order (today the order the API
serves them: **sorted by path**, not the order GitHub returned them). A lock file or a generated
snapshot sits between pieces of business logic, so the reviewer has to work out for themselves
what deserves close reading and what can be skimmed.

Agent findings live only on the Agent runs tab. To match a finding with the code it talks about,
the reviewer jumps between tabs, finds the file, expands it and scrolls to the line.

Smart Diff fixes both: it puts the files in reviewer reading order, grouped by role, and shows the
review result where the code is, so the reviewer reads code and explanation in one place.

## Goals / Non-goals
**Goals**
- Group the PR's files by role in a fixed reading order: core, tests, wiring, docs, boilerplate.
  Classification is deterministic, path-based, and the first matching rule wins.
- Show the latest review result of each agent on the Files changed tab in three places: the group
  header (how many files in the group have open findings), the file card (an open-findings dot),
  and the code line (the finding card under it, with Accept / Dismiss).
- Let a review started from Files changed run without leaving the tab. Its result appears there
  when it finishes.
- Let the reviewer switch back to the original order.
- Pin the classification order with a table of path → role cases (below) that the tests reproduce.

**Non-goals**
- No change to the Agent runs tab. It keeps showing whole runs, older reruns included.
- No LLM call. Classification is path rules only. The "summary" button and the "What this does:"
  per-file pseudocode line in screenshot 3 are **out of scope**. They need a model call, so they
  get their own spec if they're wanted.
- Screenshots are illustrative. Where a screenshot places a file in a different group than the
  rules do (e.g. `src/config.ts`, `src/server.ts`, `package.json` in screenshot 1), the written
  rules win.
- No change to how reviews are produced, grounded or stored, and no new finding fields.
- No user-editable or per-repo classification rules.
- No PR-split suggestion. An unused "Smart Diff" shape already exists in the shared contracts
  (roles core/wiring/boilerplate only, plus a split suggestion), and no route serves it. This
  spec does not depend on it.
- No keyboard navigation (`j`/`k`/`a`/`d`) for findings on the Files changed tab.
- No change to GitHub human review comments (their counter, threads, show/hide toggle, posting).

## User stories
- **US-1** As a reviewer, I open a PR's Files changed tab and see files grouped core → tests →
  wiring → docs → boilerplate, each group with a role label and a file count. → AC-1, AC-2, AC-3,
  AC-4, AC-5, AC-6
- **US-2** As a reviewer, I see the docs and boilerplate groups collapsed, and a lock file sits in
  boilerplate. → AC-7, AC-8, AC-2
- **US-3** As a reviewer, I click Run review on Files changed, stay on that tab while it runs, and
  once it finishes I see on each group header how many of its files have open findings, and a dot
  on those files' cards. → AC-9, AC-10, AC-11, AC-12, AC-13, AC-27, AC-28, AC-29
- **US-4** As a reviewer, I expand a file with findings and see each finding's card under the line
  it points at, looking and acting like the finding card on the Agent runs tab. Findings I've
  already accepted or dismissed stay there, muted. → AC-14, AC-15, AC-16, AC-17, AC-18, AC-19,
  AC-20, AC-30, AC-31
- **US-5** As a reviewer, I switch to "Original order" when I need the usual flat order, and back.
  → AC-21, AC-22, AC-23

## Classification rules
Applied to each file's repo-relative path, using `/` as the separator. Rules are checked top to
bottom and the **first match wins**:

| # | Role | Patterns |
|---|---|---|
| 1 | boilerplate | `*.lock`, `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`, `go.sum`, `dist/**`, `build/**`, `**/__snapshots__/**`, `*.snap`, `*.generated.*`, `*.min.js` |
| 2 | tests | `**/*.test.ts`, `**/*.test.tsx`, `**/*.it.test.ts`, `**/*.spec.ts`, `*_test.go`, `**/test/**`, `**/tests/**`, `**/__tests__/**`, `e2e/**` |
| 3 | wiring | `index.ts`, `index.js` (barrel files), `*.config.*`, `tsconfig*.json`, `.eslintrc*`, `.env*`, `docker-compose*.yml`, `.github/**`, `.claude/**` |
| 4 | docs | `**/*.md`, `docs/**`, `README*`, `CHANGELOG*`, `LICENSE` |
| 5 | core | everything else |

Pattern semantics:
- A pattern **without** `/` matches the file's **basename** at any depth. So `client/pnpm-lock.yaml`
  and `api/go.sum` are boilerplate, `api/internal/foo/foo_test.go` is tests, and
  `src/api/public/index.ts` is wiring.
- A pattern **with** `/` matches from the repository root. A leading `**/` means "at any depth".
  So `dist/**` matches `dist/app.js` but not `client/dist/app.js`.
  [NEEDS CLARIFICATION: should the root-anchored directory patterns (`dist/**`, `build/**`,
  `docs/**`, `e2e/**`) also match at any depth? This repo is a multi-folder layout (`client/`,
  `api/`, `e2e/`), so `client/dist/**` or `api/docs/**` would otherwise fall through.]
- `*` matches within one path segment and `**` matches across segments. Matching is
  case-sensitive. [NEEDS CLARIFICATION: should `README*`, `LICENSE`, `CHANGELOG*` match
  case-insensitively, e.g. `readme.md`, `License.txt`? `LICENSE` as written matches only the
  exact basename `LICENSE`.]

### Pinned classification cases (the tests reproduce this table)
| Path | Role | Why |
|---|---|---|
| `src/__tests__/__snapshots__/x.snap` | boilerplate | Rule 1 (snapshot) is above rule 2 (`__tests__`) |
| `.claude/skills/security/SKILL.md` | wiring | Markdown under `.claude/` defines agent behavior, so rule 3 (`.claude/**`) is above rule 4 (`*.md`) |
| `e2e/README.md` | tests | Rule 2 (`e2e/**`) is above rule 4. Kept on purpose: it documents the test flows and is read with them |
| `pnpm-lock.yaml` | boilerplate | Rule 1 |
| `client/pnpm-lock.yaml` | boilerplate | Rule 1, basename match |
| `package-lock.json` | boilerplate | Rule 1 |
| `go.sum` | boilerplate | Rule 1 |
| `api/go.sum` | boilerplate | Rule 1, basename match |
| `dist/app.min.js` | boilerplate | Rule 1 |
| `src/api/x.generated.ts` | boilerplate | Rule 1 |
| `src/foo.test.tsx` | tests | Rule 2 |
| `api/internal/review/run_test.go` | tests | Rule 2 (`*_test.go`), basename match |
| `tests/README.md` | tests | Rule 2 is above rule 4 |
| `src/__tests__/index.ts` | tests | Rule 2 is above rule 3 (barrel) |
| `e2e/specs/05-pr-diff.flow.json` | tests | Rule 2 |
| `src/api/public/index.ts` | wiring | Rule 3, barrel |
| `vite.config.ts` | wiring | Rule 3, `*.config.*` |
| `tsconfig.build.json` | wiring | Rule 3 |
| `.env.example` | wiring | Rule 3 |
| `.github/workflows/api.yml` | wiring | Rule 3 |
| `docs/index.ts` | wiring | Rule 3 is above rule 4 |
| `docs/guide.md` | docs | Rule 4 |
| `README.md` | docs | Rule 4 |
| `LICENSE` | docs | Rule 4 |
| `src/middleware/ratelimit.ts` | core | No rule matches |
| `src/config.ts` | core | `*.config.*` needs text before `.config.`. Rules win over screenshot 1 |
| `src/server.ts` | core | No rule matches. Rules win over screenshot 1 |
| `package.json` | core | No rule matches. Rules win over screenshot 1 |
| `go.mod` | core | No rule matches (only `go.sum` is boilerplate) |
| `api/internal/foo/foo.go` | core | No rule matches |

## Acceptance criteria (EARS)

### Grouping (Smart order)
- **AC-1** WHILE Smart order is active, the system shall show the PR's files in role groups in the
  order core, tests, wiring, docs, boilerplate, and each file in exactly one group, assigned by the
  Classification rules (first match wins).
  *Verify: a unit table test over the "Pinned classification cases" table, run through the
  classifier.*
- **AC-2** The system shall classify a file only from its path, with no network or model call, so
  the same path always gets the same role.
- **AC-3** WHILE Smart order is active, the system shall keep each file within its group in the
  same relative order as the Original order.
- **AC-4** WHILE Smart order is active, each group header shall show the role's label, a one-line
  description of the role, and the number of files in that group as "N files" ("1 file" for one).
  *Verify: PR #482 seeded → the "Core logic" header shows "4 files".*
- **AC-5** IF a role group has no files, THEN the system shall not render that group.
- **AC-6** The system shall show above the groups the total number of listed files with their
  total additions and deletions, and a Smart order / Original order switch.
- **AC-7** WHEN the Files changed tab opens in Smart order, the system shall render the docs and
  boilerplate groups collapsed, and the core, tests and wiring groups expanded.
- **AC-8** WHEN the user clicks a group header, the system shall toggle that group between
  collapsed and expanded without changing any other group.

### Which findings the tab shows
Definitions used below:
- **Shown findings** are the findings of the **latest review of each agent** on the PR whose file
  path equals a listed file's path. "Latest" means the most recently created review.
- An **open** finding is a shown finding that is neither accepted nor dismissed.

- **AC-27** The system shall take the findings on the Files changed tab only from each agent's
  latest review on the PR. Findings of that agent's older reviews (earlier reruns) shall not be
  shown, counted or marked there.
  *Verify: an agent with two reviews, an older one with a finding on `a.ts` and a newer one with
  none, shows no dot on `a.ts`.*
- **AC-28** WHEN an agent's newer review is stored, the system shall replace that agent's
  previously shown findings with the new review's findings, leaving other agents' findings
  unchanged.

### Findings on the diff
- **AC-9** WHILE Smart order is active and at least one file in a group has an open finding, the
  group header shall show a dot with the number of **files** in that group with at least one open
  finding (not the number of findings), placed before "N files".
  *Verify: a group whose two files have five open findings between them shows "2".*
- **AC-10** IF no file in a group has an open finding, THEN the group header shall show no finding
  counter, even when the group's files have accepted or dismissed findings.
- **AC-11** WHILE a file has at least one open finding, its file card header shall show a dot next
  to the path, with no number, in both Smart and Original order. A file whose shown findings are
  all accepted or dismissed shall show no dot.
- **AC-12** The system shall keep the file card's GitHub review-comment counter (message icon and
  count) as it is today, separate from the findings dot. Neither marker shall count the other's
  items.
- **AC-13** WHEN a review run settles (done, failed or cancelled) while the Files changed tab is
  shown, the system shall update the group counters, file dots and inline findings without a page
  reload or a tab switch.
- **AC-14** WHILE a file card is expanded, the system shall render each shown finding of that file
  as a finding card directly under its anchor line. The anchor line is the diff line whose
  new-file line number equals the finding's `end_line`. If that line isn't rendered, it is the
  last rendered line within `start_line`–`end_line`.
- **AC-15** The inline finding card shall show the same content as the finding card on the Agent
  runs tab: severity, title, category, the line or line range, confidence, the rationale, the
  suggested fix when there is one, and Accept / Dismiss.
- **AC-16** WHILE a line falls within the `start_line`–`end_line` range of a shown finding, the
  system shall mark that line with a left stripe in the severity's color. The anchor line shall
  also carry a right-aligned label: CRITICAL → "blocker", WARNING → "warning", SUGGESTION →
  "suggestion".
- **AC-17** IF more than one shown finding anchors to the same line, THEN the system shall stack
  their cards under that line in the Agent runs order (severity CRITICAL, WARNING, SUGGESTION),
  and the line's stripe and label shall use the highest severity among them.
- **AC-18** The system shall match finding lines only against **new-file** line numbers (added and
  context lines). A deleted line shall never anchor a finding.
- **AC-19** WHEN the user clicks Accept or Dismiss on an inline finding card, the system shall
  record the decision through the same server action as the Agent runs tab, and both tabs shall
  show the same accepted/dismissed state without a reload.
  *Verify: accept inline → switch to Agent runs → the finding shows "accepted".*
- **AC-30** WHILE a shown finding is accepted or dismissed, the system shall keep its inline card
  in place, rendered muted with its "accepted" or "dismissed" state shown, as on the Agent runs
  tab.
- **AC-31** WHEN a finding becomes accepted or dismissed (on either tab), the system shall update
  the group counter and file dot without a reload. When a file's last open finding is triaged, its
  dot disappears and its group's counter drops by one.
  *Verify: PR #482 seeded → dismiss "Hardcoded Stripe secret key in commit" → the Core logic
  counter goes from 2 to 1 and `src/config.ts` loses its dot.*
- **AC-20** IF a shown finding has no rendered line in its range (the line isn't in the diff hunks,
  or the file has no diff text, e.g. binary or too large), THEN the system shall list it in a
  "Not in the diff" section at the bottom of that file card, still rendered as a finding card.
  If it is open, the file shall still count for AC-9 and AC-11.

### Running a review from Files changed
- **AC-29** WHEN the user starts a review run from the Files changed tab, the system shall stay on
  the Files changed tab and show the run's progress there, instead of switching to Agent runs.
  Starting a run from any other tab keeps today's behavior.
  *Verify: on `?tab=diff`, start a run → the address still has `tab=diff` and a progress
  indicator is visible.*

### Order switch
- **AC-21** WHEN the user selects "Original order", the system shall show all listed files as one
  flat list, with no group headers, in the order the API serves them.
- **AC-22** WHEN the user selects "Smart order", the system shall restore the grouped view.
- **AC-23** The system shall default to Smart order. The chosen order shall survive a page reload
  and be part of the page address, so a shared link opens in the same order.

### Unchanged behavior
- **AC-24** The system shall keep the existing Files changed behavior for GitHub review comments:
  showing/hiding threads, posting inline comments only while the PR is open, and outdated threads.
  Inline findings shall show regardless of the GitHub comments show/hide switch.
- **AC-25** IF the PR has no changed files, THEN the system shall show the existing "no changed
  files" empty state and no groups or order switch.
- **AC-26** The system shall make no new API route and no change to an API response shape for
  this feature.

## Edge cases
- **Finding outside the diff hunks / file-level finding.** The grounding gate drops findings that
  cite lines the diff doesn't show. But a range may run partly outside a hunk, and an agent's
  latest review can predate the PR's current head, so its lines may no longer be in the diff.
  → AC-14 (fall back to the last rendered line in range), AC-20 ("Not in the diff" section).
- **Finding on a file not in the listed files** (the path isn't in the PR's files, or it lies past
  the first 100 files the API syncs). It counts in no group and has no card to sit in.
  [NEEDS CLARIFICATION: hide it silently, or show a note such as "N findings on files not shown
  here", linking to Agent runs?]
- **Listed files fewer than the PR's file count.** The API keeps the first 100 files; the seeded
  PR says 9 files but stores 4. Group counts sum to the **listed** files (AC-4).
  [NEEDS CLARIFICATION: should the header total show the listed count, the PR's file count, or
  "N of M"?]
- **Multiple runs / multiple agents.** Only each agent's latest review feeds this tab (AC-27,
  AC-28). Agent runs still lists every review.
- **Reviews with no agent.** The contract allows a review with no agent (an agent can be gone, and
  the seeded PR #482 review is stored that way). "Latest per agent" doesn't say what happens to
  these. [NEEDS CLARIFICATION: are agent-less reviews shown, and if so, is each one treated as its
  own "agent" or are only the latest of them shown? The seeded e2e data depends on the answer.]
- **Triaged findings.** Accepted and dismissed findings stay inline, muted (AC-30). They count for
  neither the group counter nor the file dot (AC-9, AC-10, AC-11, AC-31).
  [NEEDS CLARIFICATION: should a muted (triaged) finding still put its severity stripe and label on
  the code line (AC-16), and still count toward the highest-severity choice in AC-17?]
- **Low-confidence findings.** Agent runs has a "Hide low confidence" switch (< 0.65). This tab
  has none. [NEEDS CLARIFICATION: show all findings regardless of confidence?]
- **Review in progress.** While a run is live, the tab shows its progress (AC-29) and keeps showing
  the shown findings as they were. The run's findings appear when it settles (AC-13). A cancelled
  or failed run stores no review, so it changes nothing.
- **Renamed files.** The file list carries only the current path, so a renamed file is classified
  by its new path. **Deleted files** are classified by their path. They can carry a finding only
  through AC-20, because AC-18 forbids anchoring to deleted lines.
- **Binary or no-patch files.** These show the existing "no diff text" body. Their findings go to
  "Not in the diff" (AC-20).
- **Path separators.** File paths from GitHub and finding paths are repo-relative with `/`.
  [NEEDS CLARIFICATION: should a finding path be normalized before matching (backslashes →
  `/`, a leading `./` stripped), or must it match exactly?]
- **Collapsed state.** Group and file collapse state is view state.
  [NEEDS CLARIFICATION: should a group's collapsed/expanded choice persist across reloads, or reset
  to the AC-7 defaults? Recommendation: reset.]
- **Collapsed group with findings.** A collapsed group still shows its finding counter (AC-9). This
  matches screenshot 2, where every group is collapsed.
- **Large files.** Files over the existing auto-expand threshold start collapsed, as today. Their
  dot still shows (AC-11).
- **Lethal-trifecta findings.** Their extra evidence locations in other files are not marked in
  the diff. Only the finding's own `file`/`start_line`–`end_line` is used.

## Non-functional
- **Cost:** zero LLM calls and zero tokens. Classification is deterministic (AC-2).
- **Network:** opening the Files changed tab makes no request beyond the PR detail, the PR's
  reviews and the GitHub comments the page already loads, plus the live-run status it already
  polls (AC-26).
- **Performance:** for a PR with 100 files and 200 findings, grouping and placing the indicators
  shall not visibly delay the first render of the tab compared with today's flat list.
- **Accessibility:** the findings dot and the group counter shall carry a text alternative (e.g.
  "has open findings", "2 files with open findings"), so meaning isn't carried by color alone. The
  order switch shall expose which option is selected to assistive technology. Group headers shall
  be operable from the keyboard (AC-8). Line severity labels are text, not color only (AC-16), and
  a muted card's state is shown as text (AC-30).
- **Security:** see Untrusted inputs. Path matching shall run in time linear in the path length.
  No pattern is ever built from PR data.

## Inputs (provenance)
- PR file list: path, additions, deletions, patch. `[reused: the stored PR detail served by the
  PR detail endpoint]`
- Findings: file, start_line, end_line, severity, category, title, rationale, suggestion,
  confidence, accepted/dismissed timestamps, plus each review's agent and creation time.
  `[reused: stored reviews served by the PR's reviews endpoint]`
- Latest review per agent. `[deterministic: picked from the stored reviews by agent and creation
  time]`
- File role. `[deterministic: path rules in this spec]`
- Finding-to-line placement. `[deterministic: finding line range matched against the parsed
  patch's new-file line numbers]`
- Live run progress. `[reused: the existing live-run status and run log stream]`
- GitHub review-comment counts. `[reused: existing GitHub comments pass-through, unchanged]`
- Accept / Dismiss. `[reused: the existing finding accept/dismiss actions]`
- New LLM calls: none. The review run itself is the existing run, started by the user.

## Untrusted inputs
- **File paths and patch text** come from the PR author on GitHub. They are data: matched against
  fixed patterns and rendered as text, never interpreted as markup, a pattern, or a link target
  beyond what the diff viewer already does.
- **Finding title, rationale and suggestion** are model output and may echo PR text. They are
  rendered exactly as the Agent runs finding card renders them today (the same Markdown handling).
  No new rendering path, no raw HTML.
- **Finding file paths and line numbers** are model output. They are used only to look up a listed
  file and a rendered line. A path or line that matches nothing is handled by AC-20 or the "file
  not in the listed files" edge case, never by building a URL or route from it.

## [NEEDS CLARIFICATION: …]
Resolved by the user: findings source (latest review per agent), triage state (only open findings
count; triaged ones stay inline, muted), Run review from Files changed (stays on the tab), rules vs
screenshot 1 (rules win; `*_test.go` → tests, `go.sum` → boilerplate added).

Still open:
1. **Generated Go code**: sqlc-generated files (e.g. `*.sql.go`, `db.go`, `models.go`) are core by
   the current rules. Are they recognizable by path at all, and should a rule cover them?
2. **Agent-less reviews**: the contract allows a review with no agent, and the seeded PR #482
   review is one. Are such reviews shown on Files changed, and how does "latest per agent" treat
   them?
3. **Triaged findings and line marking**: does a muted (accepted/dismissed) finding still mark the
   code line with its severity stripe and label, and count toward the highest severity in AC-17?
4. **Pattern anchoring**: should `dist/**`, `build/**`, `docs/**` and `e2e/**` match at any depth
   (`client/dist/**`), or only at the root as written?
5. **Case sensitivity**: should `README*`, `CHANGELOG*` and `LICENSE` match case-insensitively,
   and should `LICENSE` also cover `LICENSE.txt`? Should `docker-compose*.yml` also cover `.yaml`?
6. **Tests and Docs group descriptions**: the screenshots give "Core logic: The substance of the
   change — review closely", "Wiring: Hooks the core into the app" and "Boilerplate: Generated /
   mechanical — skim", but nothing for Tests and Docs. The existing UI strings label Core as "Core",
   while the screenshot says "Core logic". What copy should these use?
7. **Findings dot color**: is the dot one fixed color, or the file's highest open severity?
8. **Inline card close (×)**: screenshot 3 shows a × on each inline card. Does it only collapse the
   card locally (not a Dismiss)? Does a collapsed card leave a marker to re-open it?
9. **Range findings**: should the right-aligned severity label appear only on the anchor line (as
   in screenshot 3), or on every line in the range (as in screenshot 1, lines 46–47)?
10. **Original order details**: is "the order the API serves" (by path) acceptable as "Original
    order", given the request calls it "the order GitHub returned"? Making it GitHub's order would
    be an API change. AC-11 and AC-14 assume dots and inline findings also show in Original order.
    Confirm.
11. **Low-confidence findings**: show all findings regardless of confidence, or respect a
    < 0.65 cut-off?
12. **Findings on files that aren't listed**: hide them, or show an "N findings on files not shown
    here" note?
13. **Header total when the API lists fewer files than the PR has**: show the listed count, the
    PR's file count, or "N of M"?
14. **Finding path normalization**: should finding paths be normalized (`\` → `/`, leading `./`
    stripped) before matching file paths?
15. **Collapsed-state persistence**: should a group's collapse state persist across reloads?
    (Recommendation: no.)
16. **e2e coverage**: the seeded PR #482 has 4 files (all core) and two findings, but **no patch
    text**. An e2e flow can check the grouping and counters ("Core logic", "4 files", the counter
    "2"), but not an inline finding card. Should the seed gain patch text for `src/config.ts` and
    `src/api/users.ts`? That would make this feature touch `api/` too. The answer to question 2
    also decides whether the seeded findings show at all.
