# `api/` — DevDigest backend in Go

A Go rewrite of [`server/`](../server/README.md) and
[`reviewer-core/`](../reviewer-core/README.md), built to serve the existing
Next.js client unchanged. Rules for the code are in [`CLAUDE.md`](CLAUDE.md).

**Status:** phase 1 of 6 is done: the review engine runs from the command
line. Phase 2 has started: the HTTP API serves `/health`, `/health/ready`,
`GET /repos`, `GET /repos/{id}/pulls`, `GET /pulls/{id}`, and the agent reads
`GET /agents`, `/agents/{id}`, `/agents/{id}/versions`,
`/agents/{id}/versions/{version}` and `/agents/{id}/skills`, and
`GET /settings`, `/settings/secrets-status` and `/workspace`. The web app still
uses the TypeScript server.

Pull requests are served from the database. With a GitHub token, the TS server
first syncs them from GitHub on every read; the Go server will do that in
phase 3, with PR import. Until then it serves what was last synced.

## Run the API

It uses the same database, environment variables and secrets file
(`~/.devdigest/secrets.json`) as the TS server. The database must be migrated
and seeded, as `./scripts/dev.sh` does.

Unlike the TS server, it doesn't read `server/.env`. To run it next to the TS
server with the same settings, load that file into the shell and start it from
`server/`, so relative paths such as `DEVDIGEST_CLONE_DIR=./clones` resolve the
same way:

```sh
cd api && make build   # writes bin/api and bin/review
cd ../server
(set -a; . ./.env; set +a; API_PORT=3002 ../api/bin/api)
curl localhost:3002/workspace
```

## Checking it matches the TS server

`TestParityWithTypeScript` calls a running TS server and the Go handlers on
the same database, and requires the same status and JSON. It walks the real
data: every repository, its pull requests, and each pull request's detail;
every agent, its skills and each saved version; plus the not-found cases. Lists are compared in any order, and times as
instants:

```sh
PARITY_TS_URL=http://localhost:3001 go test ./internal/httpapi -run Parity -v
```

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

| `internal/openai` | Client for OpenAI-compatible chat completions APIs (OpenAI, OpenRouter, Ollama, vLLM), written with `net/http`. Implements `review.LLM`. | `reviewer-core/src/llm/openrouter.ts`, `completeStructured` in `server/src/adapters/llm/openai.ts` |
| `cmd/review` | The review command above | — |
| `cmd/api` | The HTTP API server: settings from the environment, graceful shutdown | `server/src/server.ts`, `platform/config.ts` |
| `internal/httpapi` | Routes, JSON and the error envelope, CORS, security headers, request logs | `server/src/app.ts`, the `modules/*/routes.ts` files |
| `internal/postgres` | Database queries: SQL in `queries/`, Go generated by [sqlc](https://sqlc.dev) (`make generate`) from the schema in the TS server's migrations | the Drizzle repositories |
| `internal/secrets` | API keys and tokens: `~/.devdigest/secrets.json` first, then the environment | `server/src/adapters/secrets/local.ts` |
| `internal/pgtest` | A throwaway, migrated Postgres for tests: one container per test binary, one database per test | `server/test/helpers/pg.ts` |

Dependencies point inward: `cmd/review` wires `openai` into `review`; `openai`
imports `review` for the types of the `LLM` interface; `review` imports only
`diff`, and nothing in the review engine uses a package outside the standard
library. The API adds `pgx` (the Postgres driver), `google/uuid`, and, for tests
only, `testcontainers-go`.

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
| Order of a PR's files and commits | Whatever order Postgres returns (the tables have no column to sort by) | Files by path, commits by time |
| Times in `GET /pulls/{id}` | GitHub's format (`…41Z`) right after a sync, JavaScript's (`…41.000Z`) otherwise | Always JavaScript's |
| `details` of a 422 for a bad path value (ID, version number) | Zod's issue objects | `[{"path": ["id"], "message": "Invalid uuid"}]`; same code and message |
| Listening address | Every network interface (changed in `c477e5a`: both servers now listen on 127.0.0.1 only) | 127.0.0.1 |

| Secrets file changed by hand | Not seen until a restart (read once and cached) | Seen at once (read on each use; it's tiny) |
| Secrets file that isn't valid JSON | Treated as empty: every key looks "Not set", with no hint why | An error naming the file; the request fails with 500 |
| `GITHUB_TOKEN=` empty (as `.env.example` ships it) and `GITHUB_PAT` set | **Returns `""`**: `"" ?? GITHUB_PAT` doesn't fall back, so the documented `GITHUB_PAT` fallback never works after copying `.env.example` | Uses `GITHUB_PAT` |
| A setting stored both workspace-wide and for the user | Whichever row Postgres returns last | The user's own value |

Kept as in TS, though odd: the PR list shows a review status (`needs_review`,
`reviewed`, `stale`), but `GET /pulls/{id}` shows GitHub's state (`open`) for
the same pull request.

Not ported yet: the lethal-trifecta fields (`trifecta_components`,
`evidence`) stay in the schema, so the model's answer has the same shape, but
Go ignores them until the lesson that uses them. Run cost isn't tracked,
matching commit `d45ab0d`, which removed it from the product.
