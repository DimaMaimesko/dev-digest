# Smart Diff — Implementation Plan
Status: approved

## Source requirements
`specs/smart-diff.md` (**Status: draft**). The user agreed to plan against the draft, with the
remaining open questions settled by the assumptions below. This plan covers AC-1 … AC-31: every
AC in the spec (AC-1–AC-26, then AC-27–AC-31). Living spec the feature changes (the spec's
`Changes:` line): `client/specs/pr-detail.md`. That update belongs to `doc-writer` after the
feature ships, **not** to a task here. The e2e flow docs are updated in T4, because
`e2e/CLAUDE.md` "Rules for flows" ties them to the flow change.

## Clarifications & recommendations

**Answered by the user (2026-10-08)**
- Plan against the draft spec: **yes**, recording the assumptions below.
- Agent-less reviews (spec open q2): all reviews with `agent_id === null` form **one shared
  bucket**, and only the newest of them is shown. Evidence: `reviews.agent_id` has no foreign key
  (`api/migrations/0000_init.sql`), so deleting an agent keeps its id. `ListReviews` only
  returns `agent_name` NULL (`api/internal/postgres/queries/reviews.sql:5-12`). In practice a null
  `agent_id` comes only from the seed (`api/internal/seed/seed.go:157`). So the seeded PR #482
  review **is** shown, and the spec's verify steps for AC-4 and AC-31 hold as written.
- Seed patch text (spec open q16): **no**. `api/` stays out of scope. The seeded findings land in
  "Not in the diff" (AC-20), because the seeded files have no patch. Vitest proves line anchoring.

**Stated assumptions for the other open questions (my judgment; the user accepted them as a set)**
1. q1 Generated Go (sqlc) files: no new rule. They stay core.
2. q3 Triaged findings: only **open** findings put a severity stripe and label on code lines and
   count toward AC-17's highest severity. Triaged cards still sit under their anchor line, muted
   (AC-30).
3. q4 Anchoring: as written. `dist/**`, `build/**`, `docs/**` and `e2e/**` match from the repo
   root only.
4. q5 Case: case-sensitive, as written. `LICENSE` matches the exact basename only;
   `docker-compose*.yml` only `.yml`.
5. q6 Copy, in a new `messages/en/smartDiff.json`, with labels and one-line descriptions:
   - Core logic: "The substance of the change — review closely"
   - Tests: "Checks the change — read with the core"
   - Wiring: "Hooks the core into the app"
   - Docs: "Explains the change — skim"
   - Boilerplate: "Generated / mechanical — skim"

   The unused `prReview.smartDiff` block (`client/messages/en/prReview.json:53-62`) stays as it is.
6. q7 Findings dot: one fixed color (`var(--crit)`) plus a text alternative. It is not
   severity-colored.
7. q8 No × on inline cards. `FindingCard`'s own expand/collapse is enough. Inline cards start
   expanded when open and collapsed when triaged (my choice).
8. q9 The right-aligned severity label goes on the **anchor line only**. The stripe goes on every
   rendered line in range.
9. q10 "Original order" is the order the API serves (by path; `api/internal/postgres/queries/pulls.sql:35`).
   Dots and inline findings show in both orders.
10. q11 All findings are shown regardless of confidence. There is no low-confidence cut-off on
    this tab.
11. q12 Findings on files that aren't listed are hidden silently.
12. q13 The header total is the **listed** count (AC-6). The "Files changed" tab badge keeps
    `pr.files_count`, unchanged.
13. q14 Finding paths match file paths exactly, with no normalization.
14. q15 Group collapse state is React state, reset to the AC-7 defaults on reload. The order goes
    in the URL as `?order=original`; no parameter means Smart (AC-23).

**Recommendation (mine):** AC-31's verify step (dismiss in the browser) can't be an e2e step,
because flows are read-only (`e2e/CLAUDE.md` "Rules for flows"). It is covered by a T3 unit test
and a manual check.

## Execution mode
- Recommended: **single**. The change is client-only, and only T1 and T2 can run in parallel;
  both are small.
- **User's choice: `multi`.** T1 and T2 run in parallel with disjoint owned paths, then T3,
  then T4.

## Modules affected
- **client/** (all logic):
  - path classifier (`src/lib/`)
  - findings support in the shared diff viewer (`src/components/diff-viewer/`)
  - Files changed tab wiring (`DiffTab`, `page.tsx`)
  - new strings
- **e2e/**: flow 05 is extended. The flow docs and coverage table are updated.
- **Out of scope:**
  - `api/` (no route, no response shape, no seed change; AC-26)
  - the `@devdigest/shared` contracts, including the unused `SmartDiff` shape
  - the Agent runs tab and its components (`FindingsTab`, `FindingsPanel`, `FindingCard` are reused, not edited)
  - LLM summaries
  - keyboard navigation
  - GitHub comment behavior
  - `client/specs/pr-detail.md` (that's `doc-writer`'s job)

## Architectural constraints
- `client/CLAUDE.md` "Do not touch": import **types only** from `@devdigest/shared`. Mirror
  runtime values (severity order, colors) locally. See also `client/INSIGHTS.md`
  2026-09-30 "Importing a value from `@devdigest/shared` breaks the build".
- `client/CLAUDE.md` "Conventions that differ from habit":
  - a component folder is `<Name>/{Name.tsx, index.ts, constants.ts, helpers.ts, styles.ts, Name.test.tsx}`, with sub-components in `_components/`
  - styles are typed `CSSProperties` objects in `styles.ts` using CSS variables, with no Tailwind classes
  - UI primitives come from the `@devdigest/ui` barrel
  - tab, filter and drawer state lives in the URL
  - strings use `useTranslations`, and a new feature gets its own `messages/en/<feature>.json`
  - tests mock the hook modules and wrap in `NextIntlClientProvider` with the real messages
- `client/CLAUDE.md` "Map": `src/components/` is cross-page chrome. The diff viewer must not
  import from `src/app/**`, so findings are rendered through a callback that the tab supplies.
- `client/CLAUDE.md` "Gotchas": e2e flows find elements by visible English text. Changing copy
  on the Files changed screen means running `../scripts/e2e.sh`.
- `client/INSIGHTS.md` 2026-09-30 "A test that passes without the API isn't testing fetch": hook
  mocks don't exercise real data, so the e2e run is part of verification.
- `client/INSIGHTS.md` 2026-10-02 "`pnpm build` breaks a running `pnpm dev`": verify with
  typecheck and tests, not `pnpm build`.
- `client/docs/data-layer.md` "Query keys": a mutation invalidates the keys it changes.
  `useFindingAction` already invalidates `["reviews", prId]` (`client/src/lib/hooks/reviews.ts:157-159`).
- `e2e/CLAUDE.md` "Rules for flows": deterministic locators only (`wait --url`, `wait --text`,
  `find text|role|label …`). Flows are read-only. A flow change updates `docs/writing-flows.md`
  and the README coverage table.
- `e2e/INSIGHTS.md` 2026-09-30 "A flow breaks after a change outside e2e/": fix the flow or the
  text, not the timeout.
- Spec "Non-functional / Security": path matching runs in time linear in the path length, and
  no pattern is ever built from PR data.

## Approach
**Data flow (no new requests, AC-26).**
- `page.tsx` already loads `usePrReviews(prId)` (`page.tsx:40`), plus active runs and runs. The
  diff tab gets `reviews`, `liveRunIds`, `onRunDone`, `order`/`onSetOrder`, `repoFullName` and
  `headSha` from the page.
- **Shown findings** are computed in `DiffTab/helpers.ts`:
  - `latestReviewPerAgent(reviews)` groups by `agent_id ?? null`, with null as one shared bucket.
  - It keeps the review with the greatest `created_at` per bucket (ties by `id`), then flattens
    its findings and keeps only those whose `file` equals a listed path.
  - This covers AC-27 and AC-28: when a newer review arrives, the refetch replaces that agent's
    findings.

**Classification (T1).** A new pure module `client/src/lib/file-role.ts` exports
`type FileRole = "core" | "tests" | "wiring" | "docs" | "boilerplate"`, `ROLE_ORDER`, and
`classifyPath(path): FileRole`.
- It is implemented as fixed predicates over the basename and the `/`-split segments
  (`endsWith`, `startsWith`, equality, `includes`), checked rule 1 → 4 with the first match
  winning, and otherwise `core`.
- There is no `RegExp` derived from data. Any fixed regex used has no nested quantifiers.
- Pattern semantics are exactly those of spec "Classification rules":
  - a pattern without `/` matches the basename
  - a root-anchored pattern matches a path prefix
  - `**/x/**` matches any non-final segment
  - `*.config.*` and `*.generated.*` need text both before and after the infix
- It lives in `src/lib/` next to `format-cost.ts` and its test (`client/src/lib/format-cost.test.ts`).

**Diff viewer findings (T2).**
- New `client/src/components/diff-viewer/findings.ts`, the counterpart of `comments.ts`
  (`comments.ts:9-22` `DiffCommentApi`), exports:
  - `interface DiffFindingApi { findings: FindingRecord[]; renderFinding: (f: FindingRecord) => ReactNode }`
    (`import type` only)
  - `isOpen(f)`, true when `!accepted_at && !dismissed_at`
  - `pathsWithOpenFindings(findings): Set<string>`
  - `placeFindings(lines: Line[], findings): { byLine: Map<number, FindingRecord[]>; stripe: Map<number, Severity>; label: Map<number, Severity>; notInDiff: FindingRecord[] }`.
    The map keys are indexes into `parsePatch` output (`helpers.ts:12-38`).
- How `placeFindings` places findings:
  - Only `add`/`ctx` lines with `newNo` are candidates (AC-18).
  - The anchor is the line whose `newNo === end_line`, or else the last candidate with `newNo` in
    `[start_line, end_line]`, or else `notInDiff` (AC-14, AC-20).
  - Stacks are sorted by a local severity order mirrored from
    `FindingsPanel/constants.ts:4-9`: CRITICAL, WARNING, SUGGESTION, INFO (AC-17).
  - The stripe covers every candidate line in range of an **open** finding, using the highest
    severity.
  - The label goes on the anchor line of open findings: CRITICAL → "blocker", WARNING →
    "warning", SUGGESTION → "suggestion". INFO gets no label, since the spec defines none. (AC-16)
- Severity color tokens are mirrored from `FindingCard/constants.ts:4-12` into
  `diff-viewer/constants.ts`.
- `DiffViewer` (`DiffViewer/DiffViewer.tsx:14-32`) gets an optional `findings?: DiffFindingApi`
  and passes it to `FileCard`.
- `FileCard` (`FileCard/FileCard.tsx:33-96`) changes:
  - It filters findings by `file.path` and memoizes `placeFindings`.
  - It renders the open-findings dot after the path, with `role="img"` and an aria-label/title of
    "has open findings", in both orders. It stays separate from the existing comment counter at
    lines 67-74 (AC-11, AC-12).
  - When expanded, it passes per-line stripe, label and cards to `CodeLine` and renders a "Not in
    the diff" section after the lines, including when `lines.length === 0` (AC-20).
- `CodeLine` (`CodeLine/CodeLine.tsx`) renders the left stripe, the right-aligned label text, and
  the finding cards directly under the row. These render **independently of
  `commenting.showComments`** (AC-24).
- All new props are optional, so `src/test/smoke.test.tsx` (`DiffViewer files=…` with no extras)
  keeps passing.

**Tab wiring (T3).**
- `DiffTab` (`DiffTab/DiffTab.tsx`) is restructured. It keeps the comments logic (lines 19-41)
  as it is (AC-24).
- AC-25: when `files.length === 0`, it renders only `<DiffViewer files={[]}/>` (the existing
  "No changed files." empty state), with no switch and no groups.
- Header row (AC-6): "Files changed · {listed} files · +A −D", the existing comments toggle, and
  an **order switch** of two buttons, "Smart order" and "Original order".
  - The buttons carry `aria-pressed`. Use a native `<button>` styled in `styles.ts` if `Button`
    (`src/vendor/ui/primitives/Button.tsx`) doesn't forward `aria-pressed`.
  - The switch calls `onSetOrder`, and the page writes `?order=original` or removes the parameter
    (AC-21–AC-23), using `setParam` (`page.tsx:65-70`).
- **Smart order** groups files with `classifyPath`, keeping the API's relative order within each
  group (AC-3). It renders a `RoleGroup` per non-empty role in `ROLE_ORDER` (AC-1, AC-5).
  - `RoleGroup` is a sub-component in `DiffTab/_components/RoleGroup/`.
  - Its header is a `<button aria-expanded>` with the label, the description, the open-findings
    counter, then "N files" via ICU plural (AC-4, AC-8, AC-9, AC-10).
  - The counter is a dot plus the number of files with ≥1 open finding, placed before "N files",
    with aria-label/title "{n} files with open findings".
  - When expanded, it renders `<DiffViewer files={groupFiles} commenting={…} findings={…}/>`.
  - Collapse state is `useState`, defaulting to docs and boilerplate collapsed (AC-7).
- **Original order** renders one `<DiffViewer files={files} …/>` with no headers (AC-21).
- `renderFinding` returns the existing `FindingCard` (`FindingCard/FindingCard.tsx:26-117`):
  `defaultExpanded={isOpen(f)}`, `repoFullName`/`headSha`, and `onAction` →
  `useFindingAction().mutate({findingId, action, prId})`. That is the same action as
  `FindingsPanel.tsx:70`, so AC-15, AC-19, AC-30 and AC-31 come from the shared `["reviews", prId]`
  invalidation.
- **Live runs (AC-13, AC-29).** Today only `FindingsTab`'s `RunStatus` calls `onRunDone`
  (`FindingsTab.tsx:99`, wired at `page.tsx:159-163`), so on the diff tab nothing refetches
  reviews when a run settles.
  - When `liveRunIds.length > 0`, `DiffTab` renders `<RunStatus runIds={liveRunIds} onDone={onRunDone}/>`
    (`RunStatus/RunStatus.tsx:12-46`) as its progress indicator, above the groups.
  - `page.tsx` extracts the existing `onRunDone` closure (lines 159-163) into one `handleRunDone`
    that both tabs use.
  - It changes `onRunStart` (`page.tsx:135`) to switch to `findings` **only when `tab !== "diff"`**.

**e2e (T4).** Extend `e2e/specs/05-pr-diff.flow.json`, because the spec's `Changes:` line allows
flow 05. After `wait --url tab=diff`, the flow:
- waits for "Core logic", "4 files" and "src/config.ts"
- asserts the counter by its accessible name "2 files with open findings", using a non-mutating
  `find label …` / `find role img … --name …` form; check the exact action with
  `agent-browser find --help`, and never click it
- waits for "Not in the diff" and "Hardcoded Stripe secret key in commit"
- clicks `find role button click --name "Original order"`, then waits for `wait --url order=original`
  and "src/config.ts"

## Tasks

### T1 — Path classifier with the pinned-case table test
- Action:
  - Create `client/src/lib/file-role.ts` (`FileRole`, `ROLE_ORDER`, `classifyPath`), implementing
    spec "Classification rules" as fixed linear predicates with the first match winning.
  - Add a table test reproducing every row of spec "Pinned classification cases", plus
    determinism (the same path gives the same role, with no I/O).
- Module: client
- Type: domain
- Skills to use: typescript-expert
- Owned paths: `client/src/lib/file-role.ts`, `client/src/lib/file-role.test.ts`
- Depends-on: —
- Known gotchas:
  - `client/CLAUDE.md` "Do not touch": no runtime import from `@devdigest/shared`.
  - Spec "Non-functional / Security": linear time, no pattern built from PR data.
  - Assumptions 3–4: root-anchored, case-sensitive.
- Acceptance: AC-1 (classification part) and AC-2. Proven by `pnpm exec vitest run src/lib/file-role.test.ts`,
  whose `it.each` covers all 30 pinned rows.

### T2 — Findings on the diff viewer (placement, dot, stripe/label, inline cards, "Not in the diff")
- Action:
  - Add `findings.ts` (`DiffFindingApi`, `isOpen`, `pathsWithOpenFindings`, `placeFindings`) and
    mirrored severity constants.
  - Thread the optional `findings` prop through `DiffViewer` → `FileCard` → `CodeLine`. Render
    the file dot, line stripe and label, stacked cards via `renderFinding`, and the "Not in the
    diff" section, independent of the comments show/hide switch.
  - Add new strings under `shell.diffViewer`: "Not in the diff", "has open findings", and
    "blocker" / "warning" / "suggestion".
  - Write unit tests for `placeFindings` over real `parsePatch` output:
    - an `end_line` anchor
    - the fallback to the last rendered line in range
    - deleted lines never anchoring
    - a no-patch file going to `notInDiff`
    - stacking order and highest severity
    - triaged findings not striping
  - Write an RTL test for `FileCard`/`DiffViewer`: the dot present only with open findings, the
    comment counter unaffected, cards rendered with comments hidden, and the "Not in the diff"
    section.
- Module: client
- Type: component
- Skills to use: react-best-practices, react-testing-library, typescript-expert
- Owned paths: `client/src/components/diff-viewer/**` (including new
  `findings.ts`, `findings.test.ts`, `FileCard/FileCard.test.tsx`), `client/messages/en/shell.json`
  (`diffViewer` keys only)
- Depends-on: —
- Known gotchas:
  - `client/CLAUDE.md` "Map": `src/components/` must not import from `src/app/**`. That's why
    `renderFinding` is a callback.
  - `client/CLAUDE.md` "Do not touch": `import type` for `FindingRecord`; mirror the severity
    order and colors locally.
  - `client/CLAUDE.md` "Conventions": styles in `styles.ts`, CSS variables only.
  - Keep `src/test/smoke.test.tsx` green, since all new props are optional.
  - Assumptions 2, 6, 8.
- Acceptance: AC-11, AC-12, AC-14, AC-16, AC-17, AC-18, AC-20, AC-24 (findings show regardless
  of the switch) and AC-30 (triaged cards still render). Proven by
  `pnpm exec vitest run src/components/diff-viewer src/test/smoke.test.tsx`.

### T3 — Files changed tab: Smart/Original order, role groups, counters, live run, page wiring
- Action:
  - Restructure `DiffTab`:
    - header total and order switch
    - `RoleGroup` sub-component with the collapse defaults and counter
    - Original order as a flat list
    - `latestReviewPerAgent` / shown-findings helpers, with null `agent_id` as one bucket
    - `renderFinding` → `FindingCard` + `useFindingAction`
    - `RunStatus` while runs are live
  - Create `messages/en/smartDiff.json` with the group labels and descriptions (assumption 5),
    "N files" plurals, "Smart order", "Original order", and "{n} files with open findings".
  - In `page.tsx`:
    - pass `reviews`, `liveRunIds`, `handleRunDone`, `order`/`onSetOrder` (`?order=`) and
      `repoFullName`/`headSha` to `DiffTab`
    - stop switching to Agent runs when a run starts from `tab=diff`
  - Write tests:
    - helpers: latest-per-agent (an older review with a finding on `a.ts` and a newer one with
      none → no finding; null-agent bucket; other agents untouched), grouping order and stability
    - `DiffTab` RTL with `lib/hooks/reviews` mocked: group order and empty groups hidden, docs
      and boilerplate collapsed, header click toggles one group, counter = files not findings
      (two files with five findings → "2"), no counter when all are triaged, the order switch
      calls `onSetOrder`, the empty state hides the switch, and triaging a finding (re-render
      with `dismissed_at` set) drops the counter and the dot
    - `page`-level behavior: `onRunStart` keeps `tab=diff`, either through a small test or by
      exposing the predicate as a helper
- Module: client
- Type: component
- Skills to use: next-best-practices, react-best-practices, react-testing-library
- Owned paths: `client/src/app/repos/[repoId]/pulls/[number]/_components/DiffTab/**`,
  `client/src/app/repos/[repoId]/pulls/[number]/page.tsx`, `client/messages/en/smartDiff.json`
- Depends-on: T1, T2
- Known gotchas:
  - `client/CLAUDE.md` "Conventions": URL state for the order; tests mock hook modules and wrap
    in `NextIntlClientProvider` with the real `smartDiff.json`, `prReview.json` and `shell.json`.
  - Don't edit `FindingCard`, `FindingsPanel`, `FindingsTab` or `RunStatus`; reuse them. The
    Agent runs tab is a non-goal.
  - `client/CLAUDE.md` "Gotchas": the copy on this screen is what e2e flow 05 waits for.
  - `client/INSIGHTS.md` 2026-10-02: no `pnpm build` while dev runs.
- Acceptance:
  - AC-1, AC-3, AC-4, AC-5, AC-6, AC-7, AC-8, AC-9, AC-10, AC-15 (reuses `FindingCard`), AC-19,
    AC-21, AC-22, AC-23, AC-25, AC-27, AC-28, AC-29 and AC-31: proven by
    `pnpm exec vitest run "src/app/repos/[repoId]/pulls/[number]/_components/DiffTab"`.
  - AC-13: `RunStatus` `onDone` → `handleRunDone`.
  - AC-26: no new hook and no new `api.*` call. Check with `git diff -- client/src/lib` empty
    apart from T1.

### T4 — e2e flow 05: grouping, counter, finding in "Not in the diff", order=original
- Action:
  - Extend `e2e/specs/05-pr-diff.flow.json` with the steps in Approach "e2e (T4)", and update its
    `description`.
  - Update the flow 05 row in `e2e/docs/writing-flows.md`, covering:
    - the seed it relies on: PR #482's two findings on files without a patch
    - the client copy: "Core logic", "N files", "Smart order"/"Original order", "Not in the diff",
      "{n} files with open findings"
    - the URL state `order=original`
  - Update the README coverage row.
- Module: e2e
- Type: flow
- Skills to use: none (`e2e/docs/writing-flows.md`)
- Owned paths: `e2e/specs/05-pr-diff.flow.json`, `e2e/docs/writing-flows.md`, `e2e/README.md`
- Depends-on: T3
- Known gotchas:
  - `e2e/CLAUDE.md` "Rules for flows": deterministic locators, never `chat`, read-only. Clicking
    the order switch is fine; Accept/Dismiss is not.
  - `e2e/CLAUDE.md` "Gotchas": never point `npm test` at the dev DB.
  - `e2e/INSIGHTS.md` 2026-09-30: needs the `agent-browser` CLI installed, and a failing wait
    means fix the text, not the timeout.
- Acceptance: browser coverage of AC-4 ("Core logic", "4 files"), AC-9 (counter "2"), AC-20
  (seeded finding in "Not in the diff") and AC-21/AC-23 (`order=original` in the URL). Proven by
  `./scripts/e2e.sh` passing all 8 flows.

## Verification
| Task | Command (from `client/` unless noted) | Passing looks like |
|---|---|---|
| T1 | `pnpm exec vitest run src/lib/file-role.test.ts` | all pinned rows pass |
| T2 | `pnpm exec vitest run src/components/diff-viewer src/test/smoke.test.tsx` | green |
| T3 | `pnpm exec vitest run "src/app/repos/[repoId]/pulls/[number]"` | green (existing PR-detail tests too) |
| all client | `pnpm typecheck && pnpm test` | no type errors; the whole suite is green |
| T4 | `./scripts/e2e.sh` (repo root) | all flows pass, including the extended 05 |

- Vitest is hermetic and needs no Postgres or API.
- `./scripts/e2e.sh` **needs Docker running**: it starts its own Postgres on :5434, the API on
  :3101 and the web app on :3100. It also needs the `agent-browser` CLI
  (`npm i -g agent-browser && agent-browser install`).
- No `api/` command runs, because `api/` is untouched. If `git diff --stat -- api/` is non-empty,
  that's a plan violation.

**Manual checks (needs a human)**
1. On `./scripts/dev.sh`, open PR #482 → Files changed and check:
   - "Core logic" shows "4 files" and counter 2
   - `src/config.ts` and `src/api/users.ts` have dots
   - docs and boilerplate are collapsed (on a PR that has such files)
2. Dismiss "Hardcoded Stripe secret key in commit" inline and check:
   - the counter goes from 2 to 1 and `src/config.ts` loses its dot, with no reload
   - on Agent runs the finding shows "dismissed"
   - then Accept it again to restore it, if desired (AC-31, AC-19)
3. On `?tab=diff`, run one agent and check:
   - the URL keeps `tab=diff`
   - the live log shows
   - when the run finishes, the counters and inline cards update without a reload (AC-29, AC-13)
4. On a real PR with a patch, expand a file with a finding and check:
   - the card sits under its `end_line`
   - the stripe spans the range
   - the label reads "blocker", "warning" or "suggestion" (AC-14, AC-16)
5. Switch to Original order, reload, and check that the order is kept. Copy the URL to a new tab
   and check that it opens in Original order (AC-23).
6. Screen reader or accessibility tree checks:
   - the dot reads "has open findings"
   - the counter reads "N files with open findings"
   - the order buttons expose `aria-pressed`
   - group headers toggle with Enter and Space
