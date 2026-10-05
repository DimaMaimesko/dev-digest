# Specs

Two kinds of spec live in this repo. Both describe **what** the system does, never how it's built.

| Kind | What it describes | Where | Lifetime |
|---|---|---|---|
| **Living spec** | What one area does *today*: routes, states, rules | `api/specs/*.md`, `client/specs/*.md`; the cross-part flow in `specs/review-flow.md` | Kept true forever; updated when behavior changes |
| **Feature-spec** | One change: the problem, goals, acceptance criteria | `specs/<slug>.md` if it spans two or more parts, else `api/specs/<slug>.md` or `client/specs/<slug>.md` | Written before the code; marked `implemented` after it ships |

`e2e/specs/*.flow.json` are browser test flows, not Markdown specs. An e2e change is part of the
feature that needs it.

## Feature-spec shape

Written by the `spec-creator` agent (`.claude/agents/spec-creator.md`). Unnumbered kebab-case slug;
the plan for it reuses the slug (`specs/run-cost.md` → `plans/run-cost.md`).

```md
# Spec — <feature>
Status: draft | approved | implemented
Changes: <living specs this feature changes>

## Problem & Motivation
## Goals / Non-goals
## User stories
## Acceptance criteria (EARS)       ← AC-1, AC-2…, one testable sentence each
## Edge cases
## Non-functional
## Inputs (provenance)
## Untrusted inputs
## [NEEDS CLARIFICATION: …]
```

- **Status** — `spec-creator` writes `draft`. You set `approved` once every open question is
  answered; only an approved spec gets planned. `doc-writer` sets `implemented` after it ships.
- **Acceptance criteria** use EARS patterns: `The system shall …` · `WHEN <event>, the system
  shall …` · `WHILE <state>, …` · `IF <unwanted>, THEN the system shall …` · `WHERE <option>, …`.
  Plans, tasks and `plan-verifier` all refer to them by `AC-#`.
- `run-cost.md` predates this shape (it uses a Decisions table and "What must hold"); it isn't
  migrated.

## After a feature ships

Fold the change into the living specs named on `Changes:` (the `doc-writer` agent does this), so the
living specs stay the single description of current behavior. The feature-spec stays as the record
of why.
