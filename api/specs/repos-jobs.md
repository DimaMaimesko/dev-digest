# Spec — repositories and background jobs

`internal/repos` (the repositories a workspace reviews, and their clones) and
`internal/jobs` (slow work in the background).

## Adding a repository — `POST /repos`
- Accepts only `http(s)://github.com/<owner>/<name>` (or `www.github.com`), optionally
  ending in `.git`. The owner is letters, digits and hyphens, not starting with a hyphen; the
  name is letters, digits, `.`, `-` and `_`, but not `.` or `..`. Anything else is 400
  `invalid_repo_url`. This check is what keeps `../` out of the clone path.
- 201 when added, 200 when the workspace already has it (two adds at once give 201, then 200).
- The clone happens in a job: shallow (depth 1), into `$DEVDIGEST_CLONE_DIR/<owner>/<name>`
  (default `~/.devdigest/workspace`; `dev.sh` sets `./clones`, i.e. `api/clones`).
- After the clone, an index job runs. → `repo-intel.md`

## Refresh, resync, delete
- `POST /repos/{id}/refresh`: fetches into the clone (or clones again if it's missing), then
  indexes. It doesn't move the checked-out commit (kept from TS).
- `POST /repos/{id}/resync`: 202 with the job; moves the clone to the default branch's latest
  commit, then indexes what changed. Unknown repo → 404.
- `DELETE /repos/{id}`: removes the repository with its pull requests and reviews. The clone
  stays on disk; adding the repo again fetches into it (kept from TS).

## Jobs
- At most **3 jobs at once**, each for at most **2 minutes**. Every job is a row in the `jobs`
  table with its kind, status and attempts (a failed job has `attempts` = 1).
- Every job runs under the runner's context: closing the runner stops them.
- Cloning and indexing are separate jobs, each with its own time limit.

## GitHub
- The token is sent as a header per git command, never saved in the clone (`.git/config`).
- `internal/github` retries rate limits, and server errors and timeouts on reads; 30 s per
  request. Posting a comment is retried only on a rate limit.
