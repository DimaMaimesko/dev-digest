# Spec — the review flow, end to end

What the starter must do for a user, across all three parts. Each step lists what
must hold. The details of each step are in `api/specs/`.

## 1. Settings
- A user can save an OpenAI, Anthropic or OpenRouter key and a GitHub token in the web
  app. Keys go to `~/.devdigest/secrets.json`, never to the database.
- "Test connection" tells a working key from a wrong one. → `api/specs/secrets.md`

## 2. Add a repository
- Pasting `https://github.com/<owner>/<name>` adds it to the workspace and answers at once.
  Cloning and indexing happen in the background.
- Only github.com URLs with valid owner and name are accepted; adding the same repo twice
  doesn't make a second row.
- When indexing finishes, `GET /repos/{id}/index-state` reports the index (the **Indexed** state).
  The starter's UI doesn't show it yet: `useRepoIntelStatus` exists in the client, unused.
  → `api/specs/repos-jobs.md`, `api/specs/repo-intel.md`

## 3. Import pull requests
- Opening a repository's PR list with a GitHub token syncs its pull requests from GitHub
  first; without a token, or when GitHub is down, the saved ones are shown.
- Opening a pull request shows its description, commits and a GitHub-like diff.

## 4. Review
- The user runs one agent, or all enabled agents, on a pull request. The request answers
  at once with one run per agent; the runs execute one after another in the background.
- The run's live log streams to the page while it runs, and the user can cancel it.
- A finished run produces a review: findings with a severity, and a 0–100 score.
- Each run records what it cost when the provider reports it (OpenRouter); failed and cancelled
  runs too. The PR list, the run timeline and the trace show it. → `run-cost.md`
- **Every finding points at lines the diff shows.** Findings citing other lines are dropped
  (the grounding gate) and listed as dropped in the run trace.
- With repo-intel on, the prompt includes the repository map and the callers of the changed
  symbols. → `api/specs/review-run.md`

## 5. Act on findings
- The user accepts or dismisses each finding; a new decision replaces the previous one.
- The user can delete a review, or a run with its review.

## Invariants
- Everything runs locally. The only outbound calls go to GitHub and the chosen LLM provider.
- No step needs an LLM key except running a review. The seeded demo data (`acme/payments-api`,
  PR #482, a finished run with its review and trace) works without any key; the e2e flows rely on this.
