# Agents

Custom subagents for this repo. Each is a single `.md` file with YAML frontmatter (`name`,
`description`, `tools`, `model`) plus a system-prompt body. `description` is what Claude Code matches
against a task to decide delegation — read the file itself for the full contract; this README is a
map, not a copy.

Together they implement **spec-driven development**: a feature-spec (what and why) → a plan (how,
split into tasks) → code → verification against both. Spec and plan conventions:
[`specs/README.md`](../../specs/README.md), [`plans/README.md`](../../plans/README.md).

## Catalog

| Agent | Responsibility | Tools | Model | Input | Output |
|---|---|---|---|---|---|
| [researcher](researcher.md) | Fact-finding backed by evidence — repo code/history or external sources. Never modifies files. | `Read, Grep, Glob, Bash, WebFetch, WebSearch` | sonnet | A concrete question | A structured report (`Findings / Evidence / References / Could not determine`) |
| [spec-creator](spec-creator.md) | Feature idea → feature-spec: problem, goals/non-goals, user stories, EARS acceptance criteria (`AC-1`…), edge cases, provenance, untrusted inputs. Asks only blocking questions; leaves the rest as `[NEEDS CLARIFICATION: …]`. May dispatch `researcher`. Never writes a plan or code. | `Read, Grep, Glob, Bash, WebFetch, Skill, Write, Agent` + DevDigest MCP read tools | opus | A feature idea, optionally designs | `specs/<slug>.md` (two or more parts) or `api/specs/<slug>.md` / `client/specs/<slug>.md`, `Status: draft` |
| [implementation-planner](implementation-planner.md) | Spec (or small clear request) → plan with tasks (owned paths, dependencies, skills, `AC-#`). Reads touched parts' `CLAUDE.md`/`INSIGHTS.md`; always asks single- vs multi-agent. Never writes a spec or code. | `Read, Grep, Glob, Bash, Skill, Write` | opus | A spec path or a clear request | `plans/<slug>.md`, `Status: draft` |
| [implementer](implementer.md) | Executes a plan, or one task of it, inside its owned paths; runs the plan's verification (`go test`, `vitest`…). No review pass. | `Read, Edit, Write, Grep, Glob, Bash, Skill` | sonnet | A plan path (+ a T-id) | Changed files, skills used, command results, deviations |
| [plan-verifier](plan-verifier.md) | Was it built as planned? Each plan item `DONE`/`PARTIAL`/`MISSING`/`CONTRADICTED` with `file:line`, traced to `AC-#`. Read-only; no code-quality opinions. | `Read, Grep, Glob, Bash` | sonnet | A plan path **and** the implementation | `PASS`/`INCOMPLETE` + traceability table |
| [backend-code-reviewer](backend-code-reviewer.md) | Go review of `api/` changes: correctness, concurrency/context, pgx/sqlc, JSON contracts, house idiom. Read-only. | `Read, Grep, Glob, Bash` | inherit | A diff (default: uncommitted `api/` changes) | Findings by Blocker / Should fix / Nit |
| [test-writer](test-writer.md) | Tests for existing code — Go stdlib tests and `pgtest`, vitest + RTL, e2e flows. Never fixes the code under test; reports bugs. | `Read, Edit, Write, Grep, Glob, Bash, Skill` | sonnet | Files/packages to test | Test files, commands with exit codes, suspected bugs |
| [doc-writer](doc-writer.md) | After a feature ships: folds it into the living specs, marks the feature-spec `implemented`, updates `docs/`/README. Markdown only. | `Read, Edit, Write, Grep, Glob, Bash, Skill` | sonnet | What shipped | Files updated, pointers, spec/code differences |

There's no frontend reviewer agent yet: client changes get `/code-review`.

## How they fit together

```
feature idea ─► spec-creator ─► specs/<slug>.md ──(you: Status: approved)──┐
                                                                           ▼
small/clear request ────────────────────────────────────────► implementation-planner ─► plans/<slug>.md
                                                                           (you: Status: approved)
                                                                                 │
                                                         /run-plan plan:plans/<slug>.md
                                     ┌───────────────────────────────────────────┤
                                     ▼                                           │
                          implementer ×N (by task DAG)                           │
                                     ▼                                           │
                plan-verifier  ‖  backend-code-reviewer (if api/ changed)        │
                                     ▼                                           │
                       bounded fix loop (implementer) ◄──────────────────────────┘
                                     ▼
          you: review diff · make check / pnpm test · /code-review
                                     ▼
          test-writer (optional) ─► doc-writer ─► /engineering-insights ─► commit
```

- **Files are the handoff.** A subagent starts with no memory of the conversation, so each stage
  reads the previous one's file cold. You decide when to move to the next stage — the two
  `Status: approved` edits are the human gates.
- **`plan-verifier` first.** It's the cheapest check and its verdict can send work back to
  `implementer`; reviewing code that's about to change is wasted. `/run-plan` runs it in parallel
  with `backend-code-reviewer` because both are read-only and neither reads the other's output.
- **No agent does another's job.** `spec-creator` never plans; `implementation-planner` never
  specs or codes; `implementer` never decides scope; `test-writer` never patches source; the
  reviewers never fix; `doc-writer` never writes source, new specs or plans. The one cross-call:
  `spec-creator` may dispatch `researcher` for a lookup.
- **Small changes skip the spec.** `implementation-planner` accepts a clear request directly; a
  one-line fix needs none of this.

## Sources

- Anthropic, *Create custom subagents* (code.claude.com/docs/en/sub-agents): `description` drives
  delegation; `tools` is an explicit allowlist; a subagent's body is its entire context; read-only
  agents deny `Write`/`Edit`.
- Anthropic, *Best practices for Claude Code*: show evidence (the command and its output) rather
  than assert success; have one Claude write code and another check it.
- Alistair Mavin et al., *Easy Approach to Requirements Syntax (EARS)*, 2009: the five requirement
  patterns `spec-creator` uses.
- GitHub Spec Kit `/clarify`: the six clarification categories and the `[NEEDS CLARIFICATION: …]`
  marker.
- Addy Osmani, *How to write a good spec for AI agents*: compare the result with the spec after
  implementing — the core of `plan-verifier`.
- This repo: root and per-part `CLAUDE.md` and `INSIGHTS.md`, `TESTING.md`, and the per-part
  `specs/` + `docs/` layout.
