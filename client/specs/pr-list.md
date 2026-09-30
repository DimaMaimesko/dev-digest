# Spec — PR list (`/repos/:repoId/pulls`)

## Must
- Shows the repository's pull requests with number, title, author, size, score, cost, status and
  last update. The header summarises "N open · M need review".
- **Cost** is what all the PR's runs cost (`cost_usd`), formatted by `lib/format-cost.ts`:
  "—" when unknown, "$0.0042" under a cent, else "$0.12"; the exact value on hover.
- **Status filter** chips: all · needs review · reviewed · stale. The default is
  `needs_review`. The choice is kept in `?status=`, set explicitly even for "all".
- **Search** matches the title (case-insensitive) or the PR number. It is not kept in the URL.
- **Sort**: newest (default) or oldest, by `updated_at`.
- **Size** bucket from additions + deletions: S < 100 ≤ M < 400 ≤ L.
- **Refresh** calls `POST /repos/:id/refresh` and reloads the list; the button is disabled
  while it runs. The list also refreshes itself every 60 s.
- Clicking a row opens `/repos/:repoId/pulls/:number`.

## States
- Loading → skeleton rows. Error → `ErrorState` with the API's message and a retry.
- No match → an empty state that names the active status filter.
- Unknown `:repoId` → `RepoNotFound`, not an error.

## Relied on by e2e
Flows 01, 02, 04, 05 land here and click the seeded row
"Add rate limiting to public API endpoints" (`acme/payments-api` #482).
