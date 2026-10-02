# Test Coverage Nudge

When the diff adds a branch (a new `if`, `switch` case, early return, or error path) and no
test in the diff exercises it, add one SUGGESTION naming the branch and the input that would
reach it. One finding per file at most; never block a PR on coverage alone.
