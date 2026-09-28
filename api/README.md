# `api/` — DevDigest backend in Go

A Go rewrite of [`server/`](../server/README.md) and
[`reviewer-core/`](../reviewer-core/README.md), built to serve the existing
Next.js client unchanged. Rules for the code are in [`CLAUDE.md`](CLAUDE.md).

**Status:** phase 1 of 6, porting the review engine. Nothing here serves HTTP
yet; the TypeScript server is still the one that runs.

## Packages

| Package | What it does | Ported from |
|---|---|---|
| `internal/diff` | Parses `git diff` output; answers "does this file's diff show lines N–M?" | `server/src/adapters/git/diff-parser.ts` |
| `internal/review` | Review domain: `Finding`; `Ground`, the gate that drops findings citing lines outside the diff; `Prompt.Assemble`, which builds the model's system and user messages | `reviewer-core/src/grounding.ts`, `reviewer-core/src/prompt.ts`, `Finding` and `PromptAssembly` from `shared/contracts/` |

The prompt must stay byte-for-byte what the TS engine sends. The files in
`internal/review/testdata/*.golden` were produced by running `assemblePrompt`
from `reviewer-core` on the same input as the Go test. Once the TS server is
removed, they become plain regression fixtures.

`review` depends on `diff`, and neither imports anything outside the standard
library.

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

**Grounding** (`grounding.ts`). The TS gate checked a finding's range by looping
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
