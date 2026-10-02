# PR Quality Rubric

Evaluate the pull request against the following dimensions. Report a finding only when the
issue is **worth the author's time**: aim for a few high-signal findings, not many small ones.

## Correctness
- Does the change do what the PR description claims?
- Are edge cases (empty input, nulls, concurrency) handled?

## Security
- Any secrets, tokens, or credentials in the diff?
- Untrusted input reaching a sink (SQL, shell, fetch)?

## Tests
- Are new branches covered by assertions?
- Are the tests meaningful (not just snapshot churn)?

## Scope
- Does the diff stay within the stated intent?
- Flag out-of-scope changes separately rather than blocking on them.
