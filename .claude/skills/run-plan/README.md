# Run Plan Skill

Implementation Plan executor for DevDigest. One command takes an **already-approved** plan
(`plans/<slug>.md`) and drives it to reviewed code — dispatching the implementer and reviewer
agents, keeping its own context lean (only short agent reports), and resolving review findings in a
bounded fix loop.

Spec authoring (`spec-creator`) and planning (`implementation-planner`) are run **separately**
beforehand. This command starts from the plan.

## What it does

```
args: plan:<path>  [mode:multi|single]  [max-fix:N]
  └─ read plan (## Tasks · DAG · owned paths · execution mode)
       └─ implementer ×N   (parallel by DAG / non-overlapping owned paths, or one at a time)
            └─ plan-verifier ‖ backend-code-reviewer (if api/ changed)   (parallel, read-only)
                 └─ fix loop ×≤max-fix   (implementer fixes blocker/should-fix + missing/partial → re-review)
                      └─ final report  +  next steps (make check, /code-review, doc-writer, insights)
```

## When to invoke

- `/run-plan plan:plans/<feature>.md` (optionally `mode:single`, `max-fix:2`)
- Phrases: "run the plan", "execute the plan", "implement plans/<x>.md".

## Inputs

| Token | Meaning | Default |
|-------|---------|---------|
| `plan:<path>` | Approved plan. **Required.** | — |
| free-text prose | Notes/constraints for this run | — |
| `mode:multi` / `mode:single` | Override the plan's Execution mode | read from plan |
| `max-fix:<n>` | Cap on the fix loop | `3` |

## Agents orchestrated

| Stage | Agent | Role |
|-------|-------|------|
| Build | `implementer` ×N | One task each; parallel by non-overlapping owned paths; runs the plan's verification |
| Review | `plan-verifier` | Was it built as planned? Traceability to `AC-#` (read-only) |
| Review | `backend-code-reviewer` | Go correctness, idiom, SQL, contracts — only when `api/` changed (read-only) |
| Fix | `implementer` ×N | Resolve Blocker/Should-fix + MISSING/PARTIAL findings |

**Not invoked here:** `spec-creator`, `implementation-planner` (run beforehand), `test-writer` and
`doc-writer` (run after, by hand). There's no client reviewer agent yet: client changes get
`/code-review`, recommended in the final report.

## Guardrails

- Starts from an approved plan with a `## Tasks` section — never authors a spec or a plan.
- Never commits, pushes, merges, or opens a PR.
- Fix loop is bounded by `max-fix`; remaining findings go to a human.
- Concurrent implementers own non-overlapping paths.

## File structure

```
run-plan/
├── SKILL.md     ← orchestrator — phased execution algorithm + bounded fix loop
├── tile.json    ← skill metadata
└── README.md    ← this file
```
