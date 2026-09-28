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
| `internal/review` | Review domain: `Finding` and `Review`; `Ground`, the gate that drops findings citing lines outside the diff; `Prompt.Assemble`, which builds the model's messages; the `LLM` interface, and asking a model for a `Review` with a retry when its answer is invalid | `reviewer-core/src/grounding.ts`, `prompt.ts`, `llm/structured.ts`, the retry loop from the three LLM providers, and contracts from `shared/` |

**Where the retry loop lives.** In the TS code, each of the three LLM providers
(OpenAI, Anthropic, OpenRouter) has its own copy of the loop that re-asks the
model when its JSON is invalid. In Go, the `LLM` interface makes one call per
request, and `review` owns the loop, once. Provider adapters stay thin.

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

**Structured output** (`llm/structured.ts`):

| Case | TypeScript | Go |
|---|---|---|
| JSON schema sent with every call | 8.9 KB: includes an unused copy of the whole schema, added by the Zod converter | 4.1 KB: the same schema without the copy (checked to be semantically equal) |
| Fenced answer with a code block inside a string value | Cut at the inner ```` ``` ````, so parsing fails and a retry is spent | Parsed. A JSON decoder reads from the first `{` and understands strings. |
| A required string field is missing | Rejected by Zod | Decodes as `""`. Grounding drops a finding with no `file`. Enums, ranges and types are checked as before. |

Not ported yet: the lethal-trifecta fields (`trifecta_components`,
`evidence`) stay in the schema, so the model's answer has the same shape, but
Go ignores them until the lesson that uses them. Run cost isn't tracked,
matching commit `d45ab0d`, which removed it from the product.
