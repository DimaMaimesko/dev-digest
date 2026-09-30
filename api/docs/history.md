# History: the port from TypeScript

The backend was TypeScript (`server/`, Fastify, and `reviewer-core/`) until the
Go rewrite finished. The last commit with the TS code is `e9e4574`, tagged
`ts-final`. This file records how the port was done and verified. It doesn't
change any more; for what the code does now, see `../specs/`.

## Phases

All six are done.

1. Port `reviewer-core` into `internal/review`, with an OpenAI-compatible adapter and the `cmd/review` CLI.
2. API skeleton and the database-backed read endpoints (18 of 22 `GET` routes; the other 4 needed GitHub, the LLM adapters or the run bus), each checked by a parity test against the running TS server.
3. Write paths. A fallback proxy (`TS_API_URL`) forwarded unported routes to the TS server, so the web app could run on the Go server throughout. `PUT /settings`; agent writes (`internal/agents`); accept and dismiss findings, delete reviews and runs; GitHub sync on pull request reads and `POST /repos/{id}/poll` (`internal/github`, `internal/pulls`); PR comments, read and post; model lists (`internal/anthropic`).
4. Reviews: inputs (`internal/git`, `internal/repointel` with the tree-sitter callers), Anthropic for reviews, the run executor (`internal/runner`), and the review, events and cancel routes. Prompts checked byte-for-byte against TS with fake models.
5. Repositories: clone and fetch, the job runner, `POST /repos`, refresh, delete (`internal/repos`, `internal/jobs`); the repo-intel indexer and `POST /repos/{id}/resync`; `POST /settings/test-connection`. All 40 routes ported.
6. Migrations and seed in Go (`internal/migrate`, compatible with Drizzle's bookkeeping; `internal/seed`; `cmd/db`), with the migrations moved to `api/migrations` (embedded). `dev.sh`, `e2e.sh` and the `e2e web` workflow switched to the Go server, with its settings in `api/.env`. Then `server/`, `reviewer-core/`, their workflows, the fallback proxy and the parity test were removed.

## Porting rule: order the moves by shared state

While both servers ran, a route that wrote state the TS server cached in memory could move
only after every TS route reading that cache. Example: `POST /settings/test-connection`
writes `secrets.json`, which TS read once and cached, so it stayed on TS until phase 5.

## How the migrations and seed were checked

Checked, while both existed, on throwaway databases: one migrated and seeded
by the TS server, one by Go, and one by TS then Go. The schemas (`pg_dump --schema-only`),
Drizzle's records and every seeded row were identical, and Go found nothing
to do on the TS one. The test databases (`internal/pgtest`) are migrated
with `internal/migrate`.

## How the API was checked against the TS server

While both servers existed, a parity test called the running TS server and
the Go handlers on the same database and required the same status and JSON,
walking the real data: every repository, its index state and pull requests;
each pull request's detail, reviews and runs, and each run's trace; every
agent, its skills and each saved version; plus the not-found cases. Another
compared the Go GitHub client with what the TS server fetched from GitHub.
During the move, the Go server forwarded the routes it didn't handle yet to
the TS server, so the web app could run on it throughout. Both are gone with
the TS code (`ts-final` has them, in `internal/httpapi`).

To check the reviews themselves, the dev database was copied into a throwaway
one and both servers reviewed the same pull requests with a recording fake
model, one of them a change to a copy of the dev-digest clone: the prompts
they sent, from the diff to the callers and the repository map, were
identical to the byte.

## How the TS parsing was checked

`ParseSymbols` and `References` must find
exactly what the TS server's ast-grep and regex code find, since what they
find goes into the review prompt. `internal/repointel/testdata/symbols.golden.json` and
`testdata/references.golden.json` were made by running the TS functions on
the inputs next to them. Run once on every TypeScript and JavaScript file of
this repository (396 files, 1465 symbols), and on real changes to the
dev-digest clone, Go and TS agreed on everything.

## How the indexer was checked

The Go indexer indexed the dev-digest clone
into a throwaway database, and every table was compared with the TS index of
the same commit in the dev database: 1032 symbols (with their content
hashes), 6416 references (with the file each resolves to), 514 import edges,
312 file ranks (the PageRank scores bit for bit), 21 files' routes and crons,
and the repository map (6130 bytes, 1494 tokens) were identical; so were the
index state and its stats but the duration: 499 ms, against 1532 ms in TS.
`internal/repointel/testdata/index.golden.json` keeps small cases the TS functions answered.
