---
name: engineering-insights
description: Captures the non-obvious lessons of the current task (a failure and its root cause, a pattern that worked, a decision and its reason, a fact about the codebase or toolchain) into the INSIGHTS.md of the part the task touched, so the next session starts already knowing them. Use at the end of a task that hit a failure, took a wrong turn, or made a design decision; right after a non-obvious fix is confirmed; or when the user says "/engineering-insights", "wrap up", "capture learnings", "what did we learn", "запиши інсайти".
---

# engineering-insights

Turn what this session learned the hard way into short, cold-readable entries in
the right `INSIGHTS.md`. The next session reads them before it starts (root
`CLAUDE.md` → "Insights loop"), so one good entry saves the next person the same
wrong turn. A generic entry costs every future session tokens and attention and
saves nothing: **writing nothing is a valid outcome.**

## Where entries go

| The insight is about… | File |
|---|---|
| `api/**`: Go, sqlc, migrations, the review engine, repo-intel, jobs | `api/INSIGHTS.md` |
| `client/**`: Next.js, hooks, contracts on the client side, vitest | `client/INSIGHTS.md` |
| `e2e/**`: flows, agent-browser, the runner | `e2e/INSIGHTS.md` |
| Docker, ports, `scripts/`, CI workflows, toolchain, anything spanning parts | `INSIGHTS.md` (root) |

Pick the part where the **rule has to be applied**, not where the symptom showed
up. One insight, one file. Never write into `api/clones/`.

## Entry format

Newest first: insert right under the `---` line that ends the file's header.

```
## YYYY-MM-DD · <kind> · short title
Symptom: what you saw
Cause: why it happens / why this choice
Rule: what to do next time, imperative, one or two lines
Evidence: path/to/file.go:42, a commit, a command
Promoted: no
```

`<kind>` is one of:

- **mistake**: something went wrong: a failure, a trap, a wrong approach the
  agent or a human took. `Symptom:` is required.
- **pattern**: an approach that worked here and isn't obvious from the code.
- **decision**: a choice between options. `Cause:` is the reason, and names
  the option that was rejected.
- **context**: a fact about the codebase, a tool or the environment that you
  only learn by tripping over it.

`Symptom:` is optional for kinds other than mistake. `Evidence:` is optional
only when there is nothing to point at (a pure env issue). Don't wrap a field
past two lines.

## Workflow

1. **Collect candidates** from this session only:
   - errors hit and how they were fixed (the root cause, not the first guess)
   - approaches that were tried and dropped, and why
   - user corrections ("no, not like that", "we don't do X here")
   - retries, surprises, "it turned out that…"
   - decisions with a reason that the code alone doesn't show
   - `git status` / `git diff --stat` to see which parts were touched
2. **Filter each candidate** through the gate below. Most get dropped.
3. **Check for an existing entry.** Grep the target `INSIGHTS.md`, and also
   that part's `CLAUDE.md`, `specs/` and `docs/`, for the same rule:
   - Already a line in `CLAUDE.md`, a spec or a doc → drop it.
   - Already an entry with `Promoted: no` → **don't add a duplicate.** The rule
     has now bitten twice: propose promoting it (step 6).
   - An existing entry that is now **wrong** → don't edit it. Add the corrected
     entry and add one line to the old one:
     `Superseded: YYYY-MM-DD · <new title>`.
4. **Write** the surviving entries in the format above, in English, with
   today's date. Old entries are append-only: the only edits you may make to
   them are the `Promoted:` value and a `Superseded:` line.
5. **Validate**: `.claude/skills/engineering-insights/check.sh` from the repo
   root. Fix whatever it reports.
6. **Promote** only with the user's go-ahead. When a rule has bitten twice,
   propose one line for the "Gotchas" / "Do not touch" section of the matching
   `CLAUDE.md`, show it, and apply it only after the user agrees. Then set
   `Promoted: yes (<file> "<section>")` on the entry.
7. **Report** in a few lines: entries written (file → title), candidates you
   dropped and why (one short line each), and any promotion you proposed. Don't
   commit; the user reviews the diff.

## Quality gate

Keep a candidate only if **all** of these hold:

- **Not obvious.** Someone reading the code, `CLAUDE.md` or the error message
  would not already know it.
- **Actionable cold.** A reader with no memory of this session knows what to do
  from `Rule:` alone.
- **Specific.** It names the real file, command, flag, version or value. If you
  can't point at anything concrete, it's too vague.
- **Worth it.** It would have saved at least ~10 minutes or a wrong turn.
- **Durable.** It stays true after this branch merges. A one-off typo or a bug
  that has been fixed and can't come back is not an insight. The fix plus its
  test is the record.

The cheap-fix check: if the insight is really "this tool is awkward" (for
example, a test command so long everyone forgets it), consider fixing the root
cause (a Make target, a script) and say so instead of writing an entry.

Good and bad entries from this repo → `examples.md`.

## Never

- Never put secrets, tokens, API keys or `.env` values in an entry, not even partly.
- Never rewrite, reorder or delete old entries. Pruning is a separate step the
  user asks for.
- Never replay the session ("first we tried…, then…"). Write the conclusion.
- Never write an entry just to have written one.

## Pruning (only when asked)

Run `check.sh`, then for each file: remove entries whose `Cause:` no longer
exists in the code (verify with grep), merge duplicates, and collapse entries
already `Promoted: yes` whose rule now lives in `CLAUDE.md`. Show the user the
list before deleting anything.
