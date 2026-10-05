---
name: run-plan
description: >
  Runs an already-approved DevDigest Implementation Plan (plans/<slug>.md) end-to-end: dispatches
  implementer agents per the plan's task DAG (multi-agent by non-overlapping owned paths, or
  single-agent), then gates with plan-verifier and, when api/ changed, backend-code-reviewer in
  parallel, then resolves their findings in a bounded fix loop. Starts FROM a plan — spec authoring
  (spec-creator) and planning (implementation-planner) are run separately beforehand. Never commits,
  pushes or merges.
  TRIGGER when: "run the plan", "/run-plan", "execute the plan", "implement this plan",
  "implement plans/<x>.md", "run-plan plan:<path>".
  Does NOT cover: writing specs, writing the plan, authoring tests (test-writer is not invoked here),
  docs (doc-writer), committing or pushing.
---

# Run Plan — Implementation Plan executor

> **Take an approved Implementation Plan and drive it to reviewed code: implement per the DAG, gate
> with plan-verifier + backend-code-reviewer, and resolve their findings in a bounded fix loop.**

You are the **orchestrator**, running in the main session. The spec and the plan already exist and
were approved by a human. You do **not** implement or review yourself — you dispatch the agents and
keep only their short final reports in context. Spawn agents with the `Agent` tool; run independent
agents **concurrently** (multiple tool calls in one message).

## Inputs (args)

| Token | Meaning | Default |
|-------|---------|---------|
| `plan:<path>` | Path to the approved plan, normally `plans/<slug>.md`. **Required.** | — |
| free-text prose | Optional notes / constraints for this run (e.g. "skip T4 for now"). | — |
| `mode:multi` / `mode:single` | Override the plan's Execution mode. | read from the plan |
| `max-fix:<n>` | Cap on the fix loop (Step 3). | `3` |

If no `plan:` is given, ask for the plan path and stop — do not guess. State your interpretation of
the args in one line before starting.

## Guardrails (always)

- **Starts from a plan.** Do not author a spec or a plan. If the plan is missing, unreadable, or has
  no `## Tasks` section, stop and say so (the plan needs re-running through `implementation-planner`).
- **Approved only.** If the plan's `Status:` isn't `approved`, ask before running it.
- **No test-writer.** Coverage comes from each implementer's tests and verification. Don't spawn
  `test-writer`.
- **Never commit, push, merge, or open a PR.** The run ends at a reviewed working tree.
- **Never** run `docker compose down -v`, migrations against the dev DB, or anything in `api/clones/`.
- **Bound the fix loop** to `max-fix` iterations. If findings remain, stop and report them.
- **Respect owned-path non-overlap** whenever implementers run concurrently.
- **Keep context lean.** Hold the plan path and each agent's short report — never an agent's full
  transcript.

## Execution algorithm

### Step 0 — Read the plan

Read the plan file. Under `## Tasks`, extract for every task: `T-id`, `Action`, `Module`, `Type`,
`Skills to use`, `Owned paths`, `Depends-on`, `Known gotchas`, `Acceptance`. Read `## Execution mode`;
a `mode:` arg overrides it. Build the dependency DAG from `Depends-on`. Note the git state
(`git status --porcelain`) so you can tell this run's changes from pre-existing ones. Print a one-line
summary (e.g. "5 tasks, multi-agent, 3 batches; fix loop max 3").

### Step 1 — Implement

**Multi-agent mode:**
1. Find the **ready set** — tasks whose `Depends-on` are all complete and whose `Owned paths` don't
   overlap any other task in the batch.
2. Spawn one **`implementer`** per ready task, **concurrently**. Give each: the plan path, its T-id and
   full task block, **and the other tasks' `Owned paths`** as off-limits.
3. Wait for the batch, collect reports, mark tasks done.
4. Repeat until all tasks are complete.

**Single-agent mode:** run the tasks in plan order, one `implementer` at a time, each given the plan
path and its T-id.

If an implementer reports **blocked / failing** and can't fix it in scope: record it, and either
dispatch one targeted retry or surface it to the user — never continue past a red task others depend
on.

### Step 2 — Review gate (parallel, read-only)

Compute the **changed-file set**: `git status --porcelain` / `git diff --name-only HEAD`, minus what
was already dirty in Step 0, cross-checked with the implementer reports. Then spawn, **concurrently**:

- **`plan-verifier`** — always — with the plan path and "working tree" → a traceability table and
  `PASS`/`INCOMPLETE`.
- **`backend-code-reviewer`** — only if files under `api/` changed — told to review exactly the
  changed `api/` files → findings as **Blocker / Should fix / Nit**.

If `client/` or `e2e/` changed, there is no dedicated reviewer agent: note it, and recommend
`/code-review` in the final report.

### Step 3 — Fix loop (bounded)

Build the **fix backlog**:
- `backend-code-reviewer` findings marked **Blocker** or **Should fix** (Nits → report only).
- `plan-verifier` rows marked **MISSING**, **PARTIAL** or **CONTRADICTED**. For `CONTRADICTED`, if
  the deviation looks intentional and better, don't "fix" it back — list it for the user to decide.

If the backlog is empty → Step 4. Otherwise, for iteration `i = 1 … max-fix`:

1. **Group** findings by file / owned path into non-overlapping fix tasks.
2. **Dispatch `implementer`(s)** — one per group, concurrent where paths are disjoint — each told:
   *"Fix exactly these findings in these files, stay in scope, re-run the plan's verification."* Pass
   each finding's text, `file:line`, and the reviewer's suggested fix, plus the plan path.
3. **Re-review only what changed**: re-run `backend-code-reviewer` on the touched `api/` files;
   re-run `plan-verifier` on the items that were not `DONE`.
4. Recompute the backlog:
   - empty → **break (gate PASS)**.
   - **no progress** since the last iteration (same findings) → break and flag as stuck.
   - otherwise → next iteration.

If `max-fix` is reached with a non-empty backlog, stop and list what remains. Never exceed the cap.

### Step 4 — Final report

Output the summary below. Don't commit or push.

## Output format (final report)

```
## Run Plan — <feature>

- **Plan:** `plans/<slug>.md` — mode: multi-agent | single-agent
- **Implemented:** <N> tasks (T1…Tn) — <one line>
- **Verification:** api: <exit codes; DB tests ran|skipped> · client: <…>

### Review gate
- plan-verifier: PASS | INCOMPLETE — <N/M DONE; open items>
- backend-code-reviewer: <blocker/should-fix/nit counts> | not run (no api/ changes)

### Fix loop
- iterations run: <i> / <max-fix>
- resolved: <findings fixed>
- **remaining (needs human):** <list, or "none">

### Next steps
1. Review the diff yourself.
2. `cd api && make check` (with Docker running) and/or `cd client && pnpm typecheck && pnpm test`.
3. `/code-review` (needed for client/ or e2e/ changes, which had no reviewer agent).
4. Optional: `test-writer` for more coverage; `doc-writer` to update the living specs and mark the
   feature-spec `implemented`.
5. `/engineering-insights` if the run hit a failure, wrong turn or decision; then commit.
```

## When you cannot proceed

If `plan:` is missing or unreadable, or an implementer is blocked on something only a human can
decide — stop and say plainly what you need. A clear "blocked here, need X" is a valid result; a
half-run pretending to be complete is not.
