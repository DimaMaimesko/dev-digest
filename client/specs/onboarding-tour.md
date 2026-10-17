# Spec — Onboarding Tour (`/repos/:repoId/tour`)

## Must
- Shows the repo's generated tour: 5 fixed sections, in this order — **Architecture overview**,
  **Critical paths**, **How to run locally**, **Guided reading path**, **First tasks**.
- **Header**: "Onboarding for {full_name}", "Generated from index of {files_indexed} files",
  the generation's relative time, the short commit sha (7 chars), the provider/model, the cost
  (`formatCost`), and a note when the index is partial or empty (code index empty, Q5).
  **Regenerate**, no Share (AC-42).
- **Generate / Regenerate** calls `POST /repos/:id/onboarding`. Disabled while the repo isn't
  indexed (with the reason shown inline) or a generation is pending/running. Regenerating an
  existing, non-stale tour confirms first with `window.confirm`, naming the last generation's
  cost; a stale tour regenerates without asking.
- **Progress**: while a generation is `queued`/`running`, the page polls
  `GET /repos/:id/onboarding` every 2 s (`useOnboardingTour`) and shows a banner — "Generating
  the tour…" with no previous tour, "Regenerating…" when one exists and stays visible underneath.
- **A section** (`SectionCard`): a `<button aria-expanded>` header, expanded by default, the
  status as text (never color alone), and "{n} unverified references removed" when grounding
  dropped anything. A `failed` section shows its reason instead of content.
- **Architecture**: the summary renders as plain text with grounded paths as inline code + an
  "Open" link; a `null` diagram reads "Diagram unavailable". A diagram renders as CSS boxes
  colored by node kind, plus a text list of every node (with its kind) and edge.
- **Critical paths / Guided reading path**: a list of grounded paths with the model's reason and
  an "Open" link.
- **How to run locally**: numbered steps, each with a `CopyButton` (accessible name
  "Copy command: {command}", `navigator.clipboard.writeText`, "Copied" / "Copy failed"), and the
  fixed note "Generated from repository files — review before running." Commands are never
  truncated.
- **First tasks**: title, description, size, grounded paths with "Open" links, and — only when
  the model's claim matched a scanned TODO — that TODO's own text (never the model's words).
- **"Open"** opens the GitHub `blob` (file) or `tree` (directory) URL at the tour's commit sha,
  in a new tab (`target="_blank" rel="noopener noreferrer"`), built by `githubPathUrl`. The only
  href the page ever builds from model output, and only over a grounded `TourPathRef`.
- **On this page**: a sticky list of the 5 section titles; the one currently in view is
  highlighted (`IntersectionObserver`, guarded for environments without it, e.g. jsdom).
- **Rendering rule**: every piece of model text is a React text child. No Markdown primitive, no
  `dangerouslySetInnerHTML` — a title or description containing `<img>`/`[x](javascript:…)`
  renders literally, with no element created from it.
- **No tour yet**: an empty state inviting generation (AC-2).
- **Stale**: when the repo has moved past the tour's commit, a notice names both short shas; it
  never regenerates on its own.
- **`config_error`**: the `POST` fails with 400 and a message naming the missing key; shown
  inline with a link to Settings → API Keys. `repo_not_indexed` (409) reads the same as the
  index-readiness note.

## States
- Loading → skeleton. Full load failure → `ErrorState` with retry.
- Unknown `:repoId` → `RepoNotFound`, not an error (as the pulls page does).
- Repo not indexed / still indexing → the Generate/Regenerate button is disabled with the
  matching reason ("Repository not indexed yet" / "Repository is still being indexed").

## Relied on by e2e
Flow 09 opens `acme/payments-api`'s tour from the sidebar ("Onboarding Tour") and asserts, on the
seeded tour, the literal header strings "Onboarding for acme/payments-api" and "Generated from
index of 42 files", the five section titles in their rendered case, "Repository not indexed yet"
(the seeded repo has no clone), and a button named "Copy command: pnpm install" (never clicked,
to stay read-only).
