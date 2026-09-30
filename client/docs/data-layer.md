# Data layer

How the studio talks to the Go API. Code: `src/lib/api.ts`, `src/lib/hooks/*`,
`src/lib/providers.tsx`, `src/lib/toast.tsx`.

## One fetch client
`apiFetch` in `src/lib/api.ts` is the only place that calls `fetch`:
- The base URL is `NEXT_PUBLIC_API_BASE` (default `http://localhost:3001`), baked in at build time.
- It sets `content-type: application/json` only when a body is sent. A body-less POST
  (refresh, cancel) sends no content type.
- Every failure becomes an `ApiError {status, code, message, details}`:
  - API down → `status: 0`, `code: "network_error"`
  - an error response → the API's envelope `{"error": {code, message, details}}` unpacked
- `api.get/post/put/del` are thin wrappers around it.

## Hooks
Every query and mutation is a hook in `src/lib/hooks/`, grouped by domain:

| File | What |
|---|---|
| `core.ts` | settings, secrets status, test connection, repos, pulls, PR detail |
| `agents.ts` | agents CRUD, provider model lists |
| `reviews.ts` | start a review, runs, active runs, reviews, finding actions, PR comments, the SSE live log |
| `trace.ts` | a run's trace |
| `repo-intel.ts` | index state and resync (not used by any screen yet) |

Import from `@/lib/hooks` (the barrel) or a domain file directly.

### Query keys
`["settings"]`, `["secrets-status"]`, `["repos"]`, `["pulls", repoId]`, `["pull", prId]`,
`["agents"]`, `["agent", id]`, `["provider-models", provider]`, `["reviews", prId]`,
`["pr-runs", prId]`, `["pr-active-runs", prId]`, `["run-trace", runId]`,
`["repo-intel-state", repoId]`. A mutation invalidates the keys it changes in `onSuccess`.
For example, deleting a run invalidates both `pr-runs` and `reviews`, because the server deletes
the run's review too.

### Polling
- `usePulls`: every 60 s.
- `usePrRuns` and `usePrActiveRuns`: every 4 s **while a run is running**, then stop. Whether a
  review is live comes from the server (`/pulls/{id}/runs/active`), so it survives reloads.

### Live log (SSE)
`useRunEvents(runIds)` opens one `EventSource` per run on `/runs/{id}/events`. It listens to the
`info`, `tool`, `result` and `error` event names and the default message. An `error` event
raises a toast. The stream closing (the server ends it when the run is done) sets
`running: false`.

## Errors in the UI
`providers.tsx` sets up the QueryClient (`retry: 1`, `staleTime: 30 s`, no refetch on focus):
- Mutation errors always toast (they're user actions).
- Query errors toast only for network errors and 5xx. An expected 4xx stays silent, so the
  screen can show an inline empty state.
- A page that can't load at all shows `ErrorState fullScreen` with a retry.

## Contracts
Response types come from `@devdigest/shared` (`src/vendor/shared/contracts/*.ts`), which are
Zod schemas used through `z.infer` only. Nothing parses responses at runtime, so a shape
mismatch with the API shows up as a UI bug, not an error. Import types only (see `../CLAUDE.md`).
