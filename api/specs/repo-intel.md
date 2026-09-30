# Spec — repo-intel

`internal/repointel`: the index of a repository's code, and the context a review gets from it.

## What gets indexed
- TypeScript and JavaScript files only, parsed with tree-sitter: symbols (with content hashes
  and signatures cut at 120 characters, between characters) and references.
- The import graph: relative imports only. A `.js` import names the `.ts` file; a directory
  names its index. `import type` and imports used only as types are not edges.
- Each file's PageRank over the graph (damping 0.85, ≤ 100 iterations, tolerance 1e-6). The
  arithmetic follows graphology's order, so scores match the TS index bit for bit.
- The repository map: the top-ranked signatures in 1500 tokens (`cl100k_base`). Ties are
  broken by rank, export, line, name, then path.
- Per file: the HTTP routes and cron schedules it names.

## Limits
- At most 5000 files; files over 400 KB are skipped.
- Past 110 s of parsing the index stops and is `partial`, before the job's 2-minute limit.
- An incremental index takes at most 300 changed files; more means a full index. A commit
  that deletes a file gives a `partial` incremental index (kept from TS).
- Raising `indexerVersion` makes every repository indexed by an older version re-index in full.

## When it runs
- After each clone, as a background job. `POST /repos/{id}/refresh` fetches, then indexes.
  `POST /repos/{id}/resync` moves the clone to the default branch's latest commit first.
- Every write of one index run is one transaction, and one run per repository at a time.
- `GET /repos/{id}/index-state` reports the index state (the **Indexed** badge; the starter's UI doesn't render it yet).

## What a review reads from it
The map, the file ranks and the callers of the symbols a change declares, found by reading
the clone's files in path order (files over 2 MB are skipped). → `review-run.md`

## Regression fixtures
`testdata/symbols.golden.json`, `references.golden.json` and `index.golden.json` hold what
the TS implementation answered. A test failing there means behavior changed; change the
golden file only on purpose.
