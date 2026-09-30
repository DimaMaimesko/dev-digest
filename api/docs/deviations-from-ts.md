# Deviations from the TS code

The TS backend is at tag `ts-final`; the file names below refer to it.

Bugs in the TypeScript code, fixed in Go and covered by tests. Each one was
reproduced against the TS code before fixing.

**Diff parser** (`diff-parser.ts`). The TS parser decides what a line is from
its first characters. The Go parser counts the lines each `@@` header announces,
which fixes these:

| Input | TypeScript | Go |
|---|---|---|
| Diff text ending with a newline | Adds a line number that doesn't exist | Correct |
| `\ No newline at end of file` | Counted as a line, adding a line number that doesn't exist | Ignored |
| Removed line whose content starts with `-- ` (e.g. a SQL comment) | Skipped as a file header, so the deletion isn't counted | Counted as a deletion |
| Added line whose content starts with `++` | Counted as a context line, so the addition isn't counted | Counted as an addition |
| Added line whose content starts with `++ ` | **Renames the file** to the rest of the line, so every finding on it is dropped | Correct |

The Go parser also returns an error for a malformed hunk header; the TS parser
silently ignored it.

**Grounding** (`grounding.ts`, `review/run.ts`):

| Case | TypeScript | Go |
|---|---|---|
| Model labels a finding `"kind": "hook"` (or `secret_leak`, `phantom`, `lethal_trifecta`) | **Skips the line check**: the finding only needs its file in the diff, so a made-up line passes | Line check for every kind. A kind is a label the model writes, not a permission. Scanners that read whole files (later lessons) will need their own check. |
| Map-reduce: the call for file A reports a finding about file B | **Kept** if B's line is in the whole diff, though the model never saw B. Also repeats findings across calls. | Dropped, with the reporting call in the reason |

The TS gate also checked a finding's range by looping
over every line number in it until one was in the diff. A finding with a huge
range that misses the diff (say lines 100 to 999,999,999) takes about 2 seconds
per finding, measured. Go loops over the lines the diff shows instead, so the
cost doesn't depend on the range.

**Prompt** (`prompt.ts`). The prompt text is unchanged; these fixes only
affect unusual input:

| Input | TypeScript | Go |
|---|---|---|
| Untrusted text containing `</UNTRUSTED>`, `</Untrusted>`, `</untrusted >` or `</ untrusted>` | **Passes through unescaped.** A model can read it as the end of the untrusted block, so text after it looks like instructions. Only the exact `</untrusted>` was escaped. | Escaped in any letter case and spacing |
| PR description with an emoji at the 4000-character limit | Cuts the emoji in half, leaving invalid text | Cuts between characters |
| Callers or repo map made only of whitespace | Left out of the prompt, but still recorded in the run trace | Left out of both |

**Structured output** (`llm/structured.ts`):

| Case | TypeScript | Go |
|---|---|---|
| JSON schema sent with every call | 8.9 KB: includes an unused copy of the whole schema, added by the Zod converter | 4.1 KB: the same schema without the copy (checked to be semantically equal) |
| Fenced answer with a code block inside a string value | Cut at the inner ```` ``` ````, so parsing fails and a retry is spent | Parsed. A JSON decoder reads from the first `{` and understands strings. |
| A required string field is missing | Rejected by Zod | Decodes as `""`. Grounding drops a finding with no `file`. Enums, ranges and types are checked as before. |

**Review run** (`review/run.ts`, `review/reduce.ts`):

| Case | TypeScript | Go |
|---|---|---|
| Map-reduce with files `x.ts` and `x.ts.bak` | `sliceDiff` finds a file's section by substring, so the `x.ts` call also gets `x.ts.bak`, which is reviewed twice | The parser records each file's own section (`diff.File.Text`) |
| Cancelling a run | The caller passes a `checkCancelled` callback that throws | `ctx`, checked before each model call and passed to the provider |
| Score of merged file reviews | Averaged, then overwritten by the score from the grounded findings | Not computed; only the final score exists |

File calls in map-reduce mode still run one after another, as in TS. Running
them concurrently is easy in Go, but it would change how many requests hit the
provider at once, so it waits for a real need.

**HTTP API:**

| Case | TypeScript | Go |
|---|---|---|
| Order of `GET /repos` and `GET /agents` | No `ORDER BY`, so whatever order Postgres returns | Oldest first |
| Unknown route | Fastify's own body: `{"message", "error", "statusCode"}` | The API's error envelope: `{"error": {"code": "not_found", "message": ...}}` |
| No default workspace in the database | Starts; every request fails | Refuses to start and says to run the seed |
| Order of a PR's files and commits, and of a review's findings | Whatever order Postgres returns (the tables have no column to sort by) | Files by path, commits by time, findings by location |
| `GET /runs/{id}/trace` and `GET /repos/{id}/index-state` | Not limited to the workspace: they read any run's trace or repository's state | Limited to the workspace. A repository outside it gets the same "no data" state as an unknown one, so nothing leaks. |
| Database error in `GET /repos/{id}/index-state` | Reported as "degraded, no_data", like a repository that was never indexed | 500 |
| Agent names in `GET /pulls/{id}/reviews` | One query per agent | One query for everything (a join) |
| Times in `GET /pulls/{id}` | GitHub's format (`…41Z`) right after a sync, JavaScript's (`…41.000Z`) otherwise | Always JavaScript's |
| `details` of a 422 for a bad path value (ID, version number) | Zod's issue objects | `[{"path": ["id"], "message": "Invalid uuid"}]`; same code and message |
| Listening address | Every network interface (changed in `c477e5a`: both servers now listen on 127.0.0.1 only) | 127.0.0.1 |

| Secrets file changed by hand | Not seen until a restart (read once and cached) | Seen at once (read on each use; it's tiny) |
| Secrets file that isn't valid JSON | Treated as empty: every key looks "Not set", with no hint why | An error naming the file; the request fails with 500 |
| `GITHUB_TOKEN=` empty (as `.env.example` ships it) and `GITHUB_PAT` set | **Returns `""`**: `"" ?? GITHUB_PAT` doesn't fall back, so the documented `GITHUB_PAT` fallback never works after copying `.env.example` | Uses `GITHUB_PAT` |
| A setting stored both workspace-wide and for the user | Whichever row Postgres returns last | The user's own value |
| `PUT /settings` failing halfway | The keys before the failure stay saved | Nothing is saved: one transaction |
| Body errors (`PUT` and future writes) | Empty or broken JSON: 400 with code `internal_error`; a `text/plain` body is read as a JSON string (then 422) | 400 `bad_request` or `invalid_json`; 415 `unsupported_media_type` for anything but JSON. Over 1 MB is 413 in both. |

| Agents updated at the same time | **Loses history.** Each update reads the version and writes version + 1, so updates at the same time get the same number, and the duplicate snapshot is dropped silently (`onConflictDoNothing`). Reproduced on a throwaway database: 10 concurrent updates left version 4 with 4 snapshots instead of 11, and the latest snapshot named a different model than the agent had. | The agent row is locked (`SELECT … FOR UPDATE`) for the update, so each one gets its own version and snapshot. Tested with 10 concurrent updates. |
| Creating an agent; replacing its skills | Separate statements: a failure can leave an agent without its version 1, or with no skills | One transaction each |
| Linking a skill that doesn't exist, or twice | 500, with the database's foreign key error in the message | 422 naming the field |
| Linking another workspace's skill | Linked | 422 |
| `DELETE /runs/{id}` | Two statements, not in a transaction: a failure between them deletes the review but keeps the run | One statement |

| GitHub sync | TypeScript | Go |
|---|---|---|
| `POST /repos/{id}/poll` finds a new pull request | **Saves it without `opened_at`**, and no later sync fills it in. Reproduced on a throwaway database: the list then shows `"opened_at": null`. | Saved. A sync also fills it in where TS left it empty. |
| `GET /pulls/{id}` with a GitHub token | **Answers with GitHub's title, head commit and state, but doesn't save them.** Reproduced: the answer said head `a1b2c3d4`, the database kept `oldsha`. The list and the review runner use the old values until the list is read again. | Saves them with the description, stats, files and commits, then answers from the database |
| Two `GET /pulls/{id}` of one pull request at the same time | **Duplicates its files and commits.** Each read deletes and re-inserts them, outside a transaction. Reproduced: 5 concurrent reads left 2 copies of each. | One transaction. Updating the pull request's row comes first and locks it, so refreshes take turns. Tested with 10 at once. |
| List sync failing halfway | The pull requests before the failure stay saved | One transaction |
| Diff stats missing from the list | Fetches the whole detail of each pull request (it, its files, its commits and the linked issue: 4 or 5 requests) and keeps only the stats. Whichever 10 Postgres returns first. | One request each, for the 10 newest |
| `linked_issue` in `GET /pulls/{id}` | Sent right after a GitHub fetch (one more request, for the first `#123` in the description); never when served from the database | Not sent. Nothing in the web app or the server reads it. |
| `POST /repos/{id}/poll` when GitHub fails | 500 `internal_error` with Octokit's message | 502 `github_error` with GitHub's status and message |
| `created_at` of a review comment | GitHub's format (`…00Z`) | JavaScript's (`…00.000Z`), like every time the Go API sends |
| Posting a comment when GitHub answers with a server error, or the connection drops | Retried up to 3 times, though GitHub may have posted it already | Not retried. Only a rate limit is, since GitHub did nothing then. |
| `github_comment_failed` error | Octokit's message, with the error again in `details.cause` | GitHub's status and message, no details |
| Anthropic's model list | **Only the first page, 20 models**: the code reads the page's `.data` instead of iterating it. Reproduced against a fake API with 25 models on two pages: TS listed 20. The key sees 13 models today, so none are missing yet. | Every page |
| Fields of a listed model | Only the ones its provider fills: `created` for OpenAI, `label` for Anthropic, `label`, `pricing` and `contextLength` for OpenRouter | All of them, null when unknown |
| A signature cut at 120 characters | Can cut an emoji in half, leaving invalid text | Cuts between characters |
| Order the callers search reads a clone's files | The file system's (sorted by name on macOS, not on Linux) | Sorted by name |
| Anthropic: a temperature, and forcing the tool call | Sent to every model. Per Anthropic's docs, Claude Opus 4.7 and later, Sonnet 5, Fable and Mythos refuse a temperature, and Opus 5.5, Fable 5.1 and Mythos 5.1 a forced tool, with a 400. Not reproduced, since that needs a paid call. | Left out for those models. Claude Haiku 4.5, which the agents use, gets both, as before. |
| Anthropic: asking again after an invalid answer | Sends back the model's `tool_use` block followed by a plain text message. Per the API docs a `tool_use` must be answered with a `tool_result`, so this retry fails with a 400. Not reproduced, for the same reason. | Sends the earlier answer back as text, as for the other providers |
| Cancelling a run while its model answers | **The review is saved anyway, and the run goes from cancelled back to done.** The cancel is only checked before each model call. Reproduced on a throwaway database. | The model call stops at once. A review is saved only if its run is still running, checked with the run's row locked. |
| Cancelling, then deleting, a run while its model answers | **Saves a review for a run that no longer exists**, shown in the reviews with no run. Reproduced. | Nothing saved |
| `GET /runs/{id}/events` for a run the server doesn't know | The stream stays open forever, and the page shows the run as running | The stream ends at once |
| A finished run's live log | Kept in memory until the server restarts | Kept for 10 minutes; the trace has it after that |
| `agentId` that isn't a UUID in `POST /pulls/{id}/review` | 500, with Postgres's error message | 404 "Agent not found" |
| `POST /runs/{id}/cancel` of another workspace's run | Cancelled | Not |
| The server stops during a review | The run stays "running" until the next start marks it failed | Marked failed at once: "the server stopped during the run" |
| Rate limits (for example 10 reviews a minute) | Per route, per client | None yet |
| Adding `https://github.com/../victim` | **Adds it, and the clone job deletes the directory `victim` next to the clone directory.** Reproduced in a temporary directory. With the clones in `server/clones`, `https://github.com/../src` would delete `server/src`. | 400: owner and name must follow GitHub's rules |
| Adding `https://github.com/vercel/next.js` | 400: names with a dot are refused (reproduced) | Added |
| Adding `https://evil.example/github.com/acme/widgets` | Read as `acme/widgets` (reproduced), and the clone job clones the URL as typed, from that host | 400: only github.com |
| The GitHub token when cloning | **Saved in the clone**, in the remote's URL in `.git/config` (found in the dev-digest clone) | Sent as a header for each git command, not saved |
| Two adds of one repository at once | The second is a 500 with Postgres's error (reproduced) | 201, then 200 |
| A clone that needs a password and has no token | git may ask for one on the server's terminal | Fails at once |
| `attempts` of a failed job | 0 | 1 |
| Testing an OpenRouter key | **Reports any key as working:** it lists models, and OpenRouter's model list answers without a valid key (reproduced: `sk-or-dummy` got 460 models) | Checks the key with `GET /key` first, which refuses a wrong one |
| Saving a key | Rewrites the file in place: a crash can leave it half-written, and two saves at once can lose a key. Readable only by its owner when created. | Written to a new file, then renamed over it, one save at a time; always readable only by its owner |
| `POST /repos/{id}/resync` of an unknown repository | 202, and a job that does nothing | 404 |
| Writing an index | Statement by statement: the index can be read half-written, and a failure leaves it so | One transaction, and one run per repository at a time |
| A file that takes long to parse | Given up after 2 s | No limit: tree-sitter parses in linear time; the run's 110 s budget still applies |
| Symbols the repository map ranks equal (same rank, export, line and name) | In the order Postgres returns | Also ordered by path |
| Two migrators at once | Both apply the pending migrations: Drizzle takes no lock | They take turns (an advisory lock) |
| Seeding | Statement by statement: a failure leaves part of it, and two seeds at once can make two default workspaces | One transaction, one seed at a time |
| A GitHub request times out | Not retried. The 30 s limit covers the whole detail fetch (3 to 4 requests). | Retried, like a server error. The limit is 30 s per request. |

Kept as in TS, though odd:

- The PR list shows a review status (`needs_review`, `reviewed`, `stale`), but
  `GET /pulls/{id}` shows GitHub's state (`open`) for the same pull request.
- Renaming an agent or changing its description gives it a new version,
  though snapshots hold neither. Sending an `output_schema` always does, even
  an unchanged one. Changing its skills doesn't, though snapshots hold them.
- `DELETE /runs/{id}` answers 200 with `{"ok": false}` for an unknown run,
  while `DELETE /reviews/{id}` answers 404 for an unknown review.
- Only the 50 most recently updated pull requests are read from GitHub, and
  only the first 100 files and commits of one. A sync never changes a pull
  request's author or branches, even when the base branch changes.
- A pull request with no changes at all has its diff stats fetched again on
  every read of the list.
- `POST /repos/{id}/poll` without a GitHub token is a 500 `config_error`.
- The OpenAI model list keeps GPT models and names containing `o1` or `o3`,
  so `o4-mini` isn't offered.
- An incremental index of a commit that deletes a file is `partial`: the
  file can't be read. Refreshing also runs a full index, after the fetch.
- Deleting a repository leaves its clone on disk; adding it again fetches
  into it. Refreshing fetches, but doesn't move the checked-out commit.
- `POST /runs/{id}/cancel` answers `{"ok": true}` for any run, even one that
  doesn't exist.
- A reply (`in_reply_to`) still needs `path` and `line`, which GitHub
  ignores for a reply. Only the first 100 comments of a pull request are read.

Not ported yet: the lethal-trifecta fields (`trifecta_components`,
`evidence`) stay in the schema, so the model's answer has the same shape, but
Go ignores them until the lesson that uses them. Run cost isn't tracked,
matching commit `d45ab0d`, which removed it from the product.
