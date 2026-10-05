# Spec — the `devdigest-mcp` server

An MCP server that lets a coding agent (Claude Code, …) use DevDigest: list the reviewers,
run one on a pull request, read the findings, the repository's conventions and, later, the
blast radius. `cmd/mcp` is wiring only; the server is `internal/mcpserver`.

## Shape
- **Transport:** stdio. One process per chat, started by the client from the repo's
  `.mcp.json`. Logs go to standard error only: standard output is the protocol.
- **A client of the HTTP API, nothing else.** It never opens the database: reviews run in the
  API's runner, and the API's rules (workspace scope, validation) stay in one place.
  The API's base URL is `DEVDIGEST_API_URL`, else `http://127.0.0.1:$API_PORT`, else
  `http://127.0.0.1:3001`.
- **SDK:** `github.com/modelcontextprotocol/go-sdk/mcp` (the official one).
- The API not answering is a tool error that says so and how to start it
  (`./scripts/dev.sh`), never a crash of the server.

## Token budget (the reason for most rules below)
- What a new chat pays for is the server's `instructions` plus the tool names (with tool
  search, Claude Code's default), or plus every tool schema (without it). So:
  - `instructions` stay under **600 characters** (Claude Code cuts them at 2048), the order of
    calls first.
  - A tool description is one or two sentences, at most 300 characters. A parameter's
    description is one short line. Allowed values are `enum`s, not prose.
  - No output schemas: the answer is compact JSON text, described in the tool's description.
- What a call pays for is its answer. Answers are compact JSON (no indentation, no nulls, no
  empty fields), use names and `file:line` rather than UUIDs where a name is enough, and stay
  well under Claude Code's 10 000-token warning: lists are capped, and a capped list says so
  (`"truncated": N` more).

## Identifying things
- A repository is `repo`: `owner/name`, matched case-insensitively against `GET /repos`'
  `full_name`. Unknown → error listing the imported repositories (at most 10).
- A pull request is `repo` + `pr` (its number), found in `GET /repos/{id}/pulls`. Unknown →
  error saying to import or refresh it in DevDigest.
- An agent is `agent`: its name (case-insensitive) or ID. Unknown or ambiguous → error listing
  the agents' names.

## Tools
All tools except `devdigest_run_agent` are read-only (`readOnlyHint`). Errors are tool results
with `isError: true` and a message that names the next step, not protocol errors.

### `devdigest_list_agents`
No input. `GET /agents` → per agent: `id`, `name`, `enabled`, `model` (`provider/model`),
`skills` (count), `description` cut at 200 characters. Never the system prompt.

### `devdigest_run_agent`
Input: `repo`, `pr`, `agent` (a name, an ID, or `all` for every enabled agent), `wait`
(default `false`). `POST /pulls/{id}/review` with `agentId` or `all: true`.
- Without `wait`: answers at once with the started runs (`run_id`, `agent`).
- With `wait`: polls `GET /pulls/{id}/runs` every 2 s until none of its runs is `running`, at
  most 5 minutes. Answers each run's `status`, `score`, `blockers`, the count of findings by
  severity and its `error` if it failed. Never the findings themselves: that's
  `devdigest_get_findings`. Runs still going after 5 minutes are answered as `running`.
- The wait stops when the call is cancelled. It never cancels the runs.
- `all` with no enabled agent → error.

### `devdigest_get_findings`
Input: `repo`, `pr`, `run_id` (optional), `min_severity` (`suggestion` default, `warning`,
`critical`), `format` (`concise` default, `detailed`), `limit` (default 50, at most 200).
- Reads `GET /pulls/{id}/runs` and `GET /pulls/{id}/reviews`.
- With `run_id`: that run. A run of another PR, or unknown → error. A `running` run answers its
  status and no findings; a `failed` one its error; a `cancelled` one says so.
- Without it: the latest review of each agent (reviews are newest first).
- Per review: `agent`, `run_id`, `score`, `verdict`, then the findings at or above
  `min_severity`, most severe first, then by location. Dismissed findings are left out.
- `concise` finding: `severity`, `at` (`file:start` or `file:start-end`), `title`.
  `detailed` adds `category`, `rationale`, `suggestion`, `confidence`, `accepted`.
- More findings than `limit` → the first `limit` and `"truncated": N`.

### `devdigest_get_conventions`
Input: `repo`, `accepted_only` (default `false`).
- `conventions`: from the new route `GET /repos/{id}/conventions` (below): `rule`, `evidence`
  (`path`, when known), `confidence`, `accepted`. At most 50, accepted first.
- `skills`: until the conventions extractor exists, the workspace's **enabled** skills of type
  `convention` (`GET /skills`): `name`, `description`, `body` cut at 2000 characters.
  Skills are per workspace, not per repository; the answer says so in one field (`scope`).

### `devdigest_get_blast_radius`
Input: `repo`, `pr`. **A stub**: it checks the PR exists, then answers the `BlastRadius`
contract (`client/src/vendor/shared/contracts/brief.ts`) with empty `changed_symbols` and
`downstream` and a `summary` saying it isn't implemented yet. Not an error, so an agent's
workflow can already call it; the real one reads `repo-intel` and keeps the shape.

## The route it adds: `GET /repos/{id}/conventions`
- The repository's rows of `conventions`, as `ConventionCandidate[]`
  (`client/src/vendor/shared/contracts/knowledge.ts`). Accepted first, then by confidence
  (highest first, unknown last), then by rule.
- The contract has no nulls: a missing `evidence_path` / `evidence_snippet` is `""`, a missing
  `confidence` is `0`.
- Unknown repository, or another workspace's → 404 `not_found`. Malformed ID → 422.

## Using it
`.mcp.json` at the repo root runs `go -C api run ./cmd/mcp` (or `api/bin/mcp` after `make build`).
Claude Code asks once before it starts a project's server.
The API must be running (`./scripts/dev.sh --no-client` is enough).

## Testing
- `internal/mcpserver`: the server against a fake API (`httptest.Server` with canned JSON),
  driven through the SDK's in-memory transport by a real MCP client: tool list and budgets
  (instructions and descriptions lengths), every tool's answer and its errors, the wait loop
  (with a short poll interval).
- The new route: `internal/httpapi` with Postgres, like the other routes.
