# `api/` — DevDigest backend in Go

A Go rewrite of [`server/`](../server/README.md) and
[`reviewer-core/`](../reviewer-core/README.md), built to serve the existing
Next.js client unchanged. Rules for the code are in [`CLAUDE.md`](CLAUDE.md).

**Status:** phases 1 to 5 of 6 are done. The review engine runs from the
command line, and the HTTP API serves all 22 of the TS server's `GET` routes,
runs reviews, and adds, clones, indexes and deletes repositories: all 40 of
its routes are ported, and match the TS server on the dev database (see the
parity test below). The database is migrated and seeded from Go too
(`cmd/db`), and `./scripts/dev.sh`, `./scripts/e2e.sh` and the `e2e web`
workflow run the web app on the Go server alone. The TS server stays in the
repository for now, as the reference the parity tests compare with
(`./scripts/dev.sh --ts-api` runs it instead of the Go server). With a GitHub token, reading pull
requests first syncs them from GitHub, as in TS; without one, or when GitHub
can't be reached, the saved ones are served.

## Migrate and seed the database

```sh
cd api
go run ./cmd/db migrate   # apply the migrations the database hasn't had
go run ./cmd/db seed      # default workspace and user, settings, demo data, agents
```

Both use `DATABASE_URL` (default: the docker-compose database). The
migrations are still the SQL files Drizzle generates in
`server/src/db/migrations` (`-dir` or `MIGRATIONS_DIR` point elsewhere), and
`internal/migrate` records them the way Drizzle's migrator does, in
`drizzle.__drizzle_migrations`, with the same SHA-256 and time: a database
either one migrated goes on with the other. The seed is idempotent, like the
TS one: it adds only what isn't there, and never overwrites a setting.

Checked on throwaway databases: one migrated and seeded by the TS server,
one by Go, and one by TS then Go. The schemas (`pg_dump --schema-only`),
Drizzle's records and every seeded row were identical, and Go found nothing
to do on the TS one. The test databases (`internal/pgtest`) are migrated
with `internal/migrate`.

## Run the API

`./scripts/dev.sh` builds it, migrates and seeds the database, and runs it
on :3001 with the web app on :3000.

It uses the same database, environment variables and secrets file
(`~/.devdigest/secrets.json`) as the TS server. The database must be migrated
and seeded, as `./scripts/dev.sh` does.

Unlike the TS server, it doesn't read `server/.env`: `dev.sh` loads that file
for it (a variable already set in the shell wins, as with dotenv) and starts
it from `server/`, so relative paths such as `DEVDIGEST_CLONE_DIR=./clones`
resolve the same way. To run it by hand, next to the TS server, on another
port:

```sh
cd api && make build   # writes bin/api and bin/review
cd ../server
(set -a; . ./.env; set +a; API_PORT=3002 ../api/bin/api)
curl localhost:3002/workspace
```

## Writes ported so far

The agent writes: `POST /agents`, `PUT /agents/{id}`, `DELETE /agents/{id}`
and `POST /agents/{id}/skills`. A config change gives the agent a new version
and saves a snapshot of the config; turning it on or off doesn't. The rules
are the TS server's, in `internal/agents`.

The review actions: `POST /findings/{id}/accept` and `/dismiss` (a decision
replaces the one before it), `DELETE /reviews/{id}` (a review and its
findings; its run stays in the history) and `DELETE /runs/{id}` (a run, its
trace and the review it produced).

`POST /repos/{id}/poll` saves GitHub's list of a repository's pull requests,
like reading the list does, but fails when GitHub can't be reached. The web
app doesn't call it.

`POST /pulls/{id}/comments` posts a review comment, or a reply, to GitHub.
Like reading the comments, it passes through to GitHub; nothing is saved.

`PUT /settings`, checked by the parity test with requests that change
nothing: an empty update and invalid ones. It validates the known preferences like the TS server's Zod
schema, and saves all keys in one transaction.

`POST /settings/test-connection` saves an API key or GitHub token to
`~/.devdigest/secrets.json`, when the request has one, then tests the saved
one with a cheap call: the model list, or GitHub's `GET /user`. It moved
last: the TS server reads that file once and caches it, so a key the Go
server saves only reaches it after a restart, and it had to wait until no
route left on the TS server read that cache.

**The repo-intel index.** After each clone, a job indexes the repository
(`repointel.Indexer`): the symbols and references of its TypeScript and
JavaScript files, parsed with tree-sitter; the import graph; each file's
PageRank over it; the repository map, 1500 tokens of the top-ranked
signatures; and the HTTP routes and cron schedules each file names.
Refreshing indexes the files that changed, and `POST /repos/{id}/resync`
moves the clone to the latest commit of the default branch first. Every
write of an index run is one transaction, one run per repository at a time.

The TS indexer used two JavaScript libraries. dependency-cruiser built the
import graph; Go resolves the imports itself: relative imports only (a
`.js` import names the `.ts` file, a directory its index), and, as in the
JavaScript the TypeScript compiler emits, not `import type` nor an import
only used as a type. graphology computed the PageRank; Go repeats its
arithmetic in the same order, so the scores are the same to the last bit.
Token counts come from `tiktoken-go/tokenizer`, the same `cl100k_base`.

**Repositories.** `POST /repos` adds a GitHub repository and clones it in the
background (`internal/repos`, with the job runner `internal/jobs`);
`POST /repos/{id}/refresh` fetches into the clone, and `DELETE /repos/{id}`
removes the repository with its pull requests and reviews. The GitHub token
goes to git as a header for each command and is never saved in a clone. A
clone the TS server made has it saved in its remote's URL
(`.git/config`); `git remote set-url origin https://github.com/<owner>/<name>.git`
in the clone removes it.

**Reviews.** `POST /pulls/{id}/review` starts a run per agent and answers at
once; `internal/runner` runs them in the background, one after another, and
saves each review, its findings and a trace. `GET /runs/{id}/events` streams a
run's live log as server-sent events, and `POST /runs/{id}/cancel` stops one.
The three moved together: the TS server keeps a run's live log in memory, so a
run it started can't be followed on the Go server. When the Go server starts,
it marks runs left running as failed, as TS does; so does the TS server when
it starts, including runs the Go server is running, so don't restart the TS
server during a review.

## Use the web app with the Go server

`./scripts/dev.sh` does this: the web app on :3000 uses the Go server on
:3001, alone.

The Go server can still forward a request it doesn't handle to the TS server
(`TS_API_URL`), which was how the web app kept working while routes moved
over; without it, such a request is a 404. The steps below run both, as
during the move:

1. Keep the TS server running on :3001 (`./scripts/dev.sh --ts-api`).
2. Start the Go server on :3002, forwarding to it:

   ```sh
   cd api && make build
   cd ../server && (set -a; . ./.env; set +a; API_PORT=3002 TS_API_URL=http://localhost:3001 ../api/bin/api)
   ```

3. Restart the web app pointed at the Go server (a shell variable overrides
   `client/.env`):

   ```sh
   cd client && NEXT_PUBLIC_API_BASE=http://localhost:3002 pnpm dev
   ```

Every response carries `X-Served-By: go` or `X-Served-By: ts`, visible in the
browser's network tab, and the Go server's request log shows the same as
`by=go` / `by=ts`. Without `TS_API_URL`, unported routes answer 404.

The Go server answers CORS preflights itself and removes the TS server's CORS
and security headers from forwarded responses, so each header appears once.
Streamed responses, such as a run's live events, are passed on as they arrive.

## Checking it matches the TS server

`TestParityWithTypeScript` calls a running TS server and the Go handlers on
the same database, and requires the same status and JSON. It walks the real
data: every repository, its index state and pull requests; each pull
request's detail, reviews and runs, and each run's trace; every agent, its
skills and each saved version; plus the not-found cases. Lists are compared in any order, and times as
instants:

```sh
./scripts/dev.sh --ts-api --no-client   # the TS server on :3001
PARITY_TS_URL=http://localhost:3001 go test ./internal/httpapi -run Parity -v
```

The Go handler in this test has GitHub turned off, so it never writes to the
dev database. `TestGitHubClientWithTypeScript`, in the same run, checks the Go
GitHub client instead: for up to 3 pull requests of each repository, what the
TS server has just fetched from GitHub must equal what the Go client reads. It
only reads, and skips repositories GitHub doesn't know, such as the seed data.

The review routes are compared only with requests that start nothing. To
check the reviews themselves, the dev database was copied into a throwaway
one and both servers reviewed the same pull requests with a recording fake
model, one of them a change to a copy of the dev-digest clone: the prompts
they sent, from the diff to the callers and the repository map, were
identical to the byte.

Add each newly ported route to the walk in `internal/httpapi/parity_test.go`.

## Review a diff from the command line

```sh
cd api
export OPENROUTER_API_KEY=sk-or-...
git diff main | go run ./cmd/review \
    -model deepseek/deepseek-v4-flash \
    -prompt ../docs/agent-prompts/general-reviewer.md
```

Progress goes to standard error and the review to standard output. Useful flags:

| Flag | Meaning |
|---|---|
| `-provider openai` | Call OpenAI instead, with `OPENAI_API_KEY` |
| `-provider anthropic` | Call Anthropic instead, with `ANTHROPIC_API_KEY` |
| `-base-url URL` | Call any OpenAI-compatible API, such as Ollama at `http://localhost:11434/v1` |
| `-strategy` | `auto` (default), `single-pass` or `map-reduce` |
| `-task "Review PR #482"` | A line framing the review |
| `-diff FILE` | Read the diff from a file instead of standard input |
| `-json` | Print the result as JSON |
| `-quiet` | Don't print progress |

Run `go run ./cmd/review -h` for the full list.

## Packages

| Package | What it does | Ported from |
|---|---|---|
| `internal/diff` | Parses `git diff` output into files, each with its own section of the text; answers "does this file's diff show lines N–M?" | `server/src/adapters/git/diff-parser.ts`, `sliceDiff` from `reviewer-core/src/review/reduce.ts` |
| `internal/review` | Review domain: `Finding` and `Review`; `Ground`, the gate that drops findings citing lines outside the diff; `Prompt.Assemble`, which builds the model's messages; the `LLM` interface, and asking a model for a `Review` with a retry when its answer is invalid; `Run`, the whole review: one call or one per file, merge, ground, score | `reviewer-core/src/grounding.ts`, `prompt.ts`, `llm/structured.ts`, `review/run.ts`, `review/reduce.ts`, the retry loop from the three LLM providers, and contracts from `shared/` |

**Where the retry loop lives.** In the TS code, each of the three LLM providers
(OpenAI, Anthropic, OpenRouter) has its own copy of the loop that re-asks the
model when its JSON is invalid. In Go, the `LLM` interface makes one call per
request, and `review` owns the loop, once. Provider adapters stay thin.

The prompt must stay byte-for-byte what the TS engine sends. The files in
`internal/review/testdata/*.golden` were produced by running `assemblePrompt`
from `reviewer-core` on the same input as the Go test. Once the TS server is
removed, they become plain regression fixtures.

| `internal/openai` | Client for OpenAI-compatible chat completions APIs (OpenAI, OpenRouter, Ollama, vLLM), written with `net/http`. Implements `review.LLM`, and lists models. | `reviewer-core/src/llm/openrouter.ts`, `completeStructured` and `listModels` in `server/src/adapters/llm/openai.ts` |
| `cmd/review` | The review command above | — |
| `cmd/api` | The HTTP API server: settings from the environment, graceful shutdown, optional forwarding to the TS server (`TS_API_URL`) | `server/src/server.ts`, `platform/config.ts` |
| `internal/httpapi` | Routes, JSON and the error envelope, CORS, security headers, request logs, and the proxy for routes not ported yet | `server/src/app.ts`, the `modules/*/routes.ts` files |
| `internal/postgres` | Database queries: SQL in `queries/`, Go generated by [sqlc](https://sqlc.dev) (`make generate`) from the schema in the TS server's migrations | the Drizzle repositories |
| `internal/agents` | Creating and changing agents, with their version history | `server/src/modules/agents/repository.ts`, `service.ts`, `helpers.ts` |
| `internal/github` | Client for the parts of GitHub's REST API DevDigest uses (pull requests, their files, commits and review comments), written with `net/http`: retries rate limits, and server errors for reads, 30 s per request | `server/src/adapters/github/octokit.ts`, `platform/resilience.ts` |
| `internal/anthropic` | Anthropic's API through the official Go SDK ([anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)): the model list, and `review.LLM`, which asks for the review as the input of a tool the model must call, as TS does. The SDK's own credential lookup is off: the key comes from `internal/secrets`, like the others. | `server/src/adapters/llm/anthropic.ts` |
| `internal/git` | Runs `git`: clone and fetch (a GitHub token goes in a header, never saved; never waits for a password), and the diff a review reads | `server/src/adapters/git/simple-git.ts` |
| `internal/repointel` | Repo-intel. The index (`Indexer`): symbols and references parsed with tree-sitter, the import graph (`ImportGraph`), PageRank (`RankFiles`), the repository map (`RenderMap`), per-file routes and crons. A review's context from it: the map, the file ranks, and the callers of the symbols a change declares, found in the clone | `server/src/modules/repo-intel` (service, pipeline, repository), `server/src/adapters/astgrep`, `depgraph`, `tokenizer`, `codeindex/extract.ts` |
| `internal/runner` | Runs reviews in the background: the diff (git, or the saved patches), the repo-intel context, the review engine, then the review, findings and trace saved in one transaction. Keeps each run's live log in memory for its followers. | `server/src/modules/reviews/run-executor.ts`, `service.ts`, `diff-loader.ts`, `platform/sse.ts`, `platform/run-logger.ts` |
| `internal/repos` | Adding a GitHub repository from its URL, cloning it, refreshing and removing it | `server/src/modules/repos` |
| `internal/jobs` | Runs slow work in the background, 3 at a time, 2 minutes each at most, and records each job in the `jobs` table | `server/src/platform/jobs.ts` |
| `internal/migrate` | Applies the Drizzle migrations, recorded as Drizzle's migrator does | `server/src/db/migrate.ts`, drizzle-orm's `migrator` |
| `internal/seed` | The starting data: workspace, user, settings, demo repository and review, built-in agents (their prompts embedded, copies of `docs/agent-prompts`) | `server/src/db/seed.ts`, `seed-prompts.ts` |
| `cmd/db` | `db migrate` and `db seed` | the `db:migrate` and `db:seed` scripts |
| `internal/pulls` | Saving pull requests from GitHub: the list, missing diff stats, one pull request with its files and commits | the sync code in `server/src/modules/pulls/routes.ts` and `polling/routes.ts` |
| `internal/secrets` | API keys and tokens: `~/.devdigest/secrets.json` first, then the environment | `server/src/adapters/secrets/local.ts` |
| `internal/pgtest` | A throwaway, migrated Postgres for tests: one container per test binary, one database per test | `server/test/helpers/pg.ts` |

Dependencies point inward: `cmd/review` wires `openai` into `review`; `openai`
imports `review` for the types of the `LLM` interface; `review` imports only
`diff`, and nothing in the review engine uses a package outside the standard
library. The API adds `pgx` (the Postgres driver), `google/uuid`, Anthropic's
official SDK, tree-sitter with its TypeScript and JavaScript grammars
(`go-tree-sitter`, which uses cgo: building needs a C compiler, such as
Xcode's), `tiktoken-go/tokenizer` for token counts, and, for tests only,
`testcontainers-go`.

**How the TS parsing is checked.** `ParseSymbols` and `References` must find
exactly what the TS server's ast-grep and regex code find, since what they
find goes into the review prompt. `testdata/symbols.golden.json` and
`testdata/references.golden.json` were made by running the TS functions on
the inputs next to them. Run once on every TypeScript and JavaScript file of
this repository (396 files, 1465 symbols), and on real changes to the
dev-digest clone, Go and TS agreed on everything.

**How the indexer is checked.** The Go indexer indexed the dev-digest clone
into a throwaway database, and every table was compared with the TS index of
the same commit in the dev database: 1032 symbols (with their content
hashes), 6416 references (with the file each resolves to), 514 import edges,
312 file ranks (the PageRank scores bit for bit), 21 files' routes and crons,
and the repository map (6130 bytes, 1494 tokens) were identical; so were the
index state and its stats but the duration: 499 ms, against 1532 ms in TS.
`testdata/index.golden.json` keeps small cases the TS functions answered.

## Commands

```sh
make check   # gofmt check, go vet, staticcheck, go test -race
make fmt     # format all files
```

`make lint` needs staticcheck: `go install honnef.co/go/tools/cmd/staticcheck@2026.1`.

## Deviations from the TS code

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
