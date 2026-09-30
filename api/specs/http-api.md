# Spec — HTTP API

The contract between `api/` and `client/`. The source of truth for the routes is
`internal/httpapi/server.go` (`Handler`); for the JSON shapes it is the client's Zod
contracts in `client/src/vendor/shared/contracts/`. This file holds what neither states:
status codes, side effects and the rules a change must keep.

## Rules for every route
- Listens on `127.0.0.1:$API_PORT` (3001). CORS allows only the web app's origin (from `WEB_PORT`).
- Everything is scoped to the default workspace and the local user. A resource of another
  workspace answers exactly like an unknown one (404, or "no data"): nothing leaks.
- Path IDs are UUIDs. A malformed one is a 422 `validation_error` with
  `details: [{"path": ["id"], "message": "Invalid uuid"}]`.
- Request bodies: JSON only (`Content-Type: application/json`, else 415
  `unsupported_media_type`), at most 1 MB (413 `body_too_large`). Broken JSON is 400
  `invalid_json`; a wrong field is 422 `validation_error` with the issues in `details`.
- Every error has one shape: `{"error": {"code": "…", "message": "…", "details"?: …}}`.
  An unknown route is 404 `not_found` in that shape. Unexpected failures are 500
  `internal_error` with a generic message; the cause goes to the log only.
- Times are sent in JavaScript's format (`2026-09-30T12:00:41.000Z`), always.
- Lists without a natural order are sorted: repos and agents oldest first, a PR's files by
  path, commits by time, findings by location.
- Multi-statement writes are one transaction.

## Routes (40)

### Health and workspace
| Route | Notes |
|---|---|
| `GET /health` | Liveness |
| `GET /health/ready` | 503 when the database can't be reached |
| `GET /workspace` | Workspace ID, clone directory, its repos. Top-level keys are camelCase, unlike the rest (kept from TS) |

### Repositories
| Route | Notes |
|---|---|
| `GET /repos` | |
| `POST /repos` | 201 when added, 200 when the workspace had it. Clones in the background. 400 `invalid_repo_url` for anything but a valid `github.com/<owner>/<name>` |
| `POST /repos/{id}/refresh` | Fetches into the clone in the background |
| `POST /repos/{id}/resync` | 202 with the job. Moves the clone to the default branch's latest commit, then indexes |
| `DELETE /repos/{id}` | Removes the repo with its PRs and reviews. The clone stays on disk |
| `GET /repos/{id}/index-state` | The index state (the **Indexed** badge, not rendered by the starter UI yet). Unknown repo → the "no data" state, not 404 |
| `GET /repos/{id}/pulls` | With a GitHub token, syncs the 50 most recently updated PRs first; without one, or when GitHub fails, serves the saved ones |
| `POST /repos/{id}/poll` | Like the list sync, but fails: 502 `github_error`, 500 `config_error` without a token. The web app doesn't call it |

### Pull requests
| Route | Notes |
|---|---|
| `GET /pulls/{id}` | With a token, syncs the PR (first 100 files and commits) and saves it, then answers from the database |
| `GET /pulls/{id}/comments` | Passes through to GitHub (first 100); nothing is saved |
| `POST /pulls/{id}/comments` | Posts a comment or a reply (`in_reply_to`, still needs `path` and `line`). Not retried except on a rate limit |

### Reviews and runs
| Route | Notes |
|---|---|
| `POST /pulls/{id}/review` | Body `{"agentId": "…"}` or `{"all": true}` (enabled agents); neither is 400 `invalid_run_request`. Answers at once with one run per agent → `review-run.md` |
| `GET /pulls/{id}/reviews` | |
| `GET /pulls/{id}/runs`, `GET /pulls/{id}/runs/active` | |
| `GET /runs/{id}/events` | Server-sent events, the live log. An unknown run ends the stream at once |
| `GET /runs/{id}/trace` | |
| `POST /runs/{id}/cancel` | Always `{"ok": true}`, even for an unknown run (kept from TS) |
| `DELETE /runs/{id}` | The run, its trace and its review. Unknown run → 200 `{"ok": false}` (kept from TS) |
| `DELETE /reviews/{id}` | The review and its findings; the run stays. Unknown → 404 |
| `POST /findings/{id}/accept`, `/dismiss` | A decision replaces the previous one |

### Agents
| Route | Notes |
|---|---|
| `GET /agents`, `GET /agents/{id}` | |
| `POST /agents` | 201. Creates version 1 with its snapshot, in one transaction |
| `PUT /agents/{id}` | A config change → a new version and snapshot (row locked, so concurrent updates each get one). Turning it on or off doesn't. Renaming, a new description, or sending any `output_schema` does (kept from TS) |
| `DELETE /agents/{id}` | |
| `GET /agents/{id}/versions`, `/versions/{version}` | Snapshots |
| `GET /agents/{id}/skills`, `POST /agents/{id}/skills` | Replaces the skill list. An unknown or another workspace's skill → 422 |
| `GET /agents/{id}/models` | The models of the agent's provider |

### Settings and providers
| Route | Notes |
|---|---|
| `GET /settings`, `PUT /settings` | Known preferences are validated; all keys saved in one transaction. A user's own value wins over the workspace's |
| `GET /settings/secrets-status` | Which keys are set; never the values |
| `POST /settings/test-connection` | → `secrets.md` |
| `GET /providers/{id}/models` | Every page of the provider's list; every field, null when unknown |

## Registered only when configured
The repo writes (`POST /repos`, refresh, resync, delete) need `repos.Store`; review, events
and cancel need `runner.Runner`. `cmd/api` always wires both; tests may leave them out,
and then those routes are 404.

## Changing this contract
- A new route: register it in `Handler`, add a test next to the handler, update this file
  and the matching contract in `client/src/vendor/shared/contracts/`.
- Changing an existing shape breaks the client unless both change in the same commit.
- Keep the "kept from TS" oddities unless the change fixes them on purpose; then record it
  in `docs/deviations-from-ts.md`.
