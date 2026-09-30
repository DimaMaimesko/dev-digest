# Spec — Agents (`/agents`, `/agents/:id`)

An agent is a reviewer: a provider, a model and a system prompt.

## List (`/agents`)
- One card per agent, with an enabled toggle (`PUT /agents/:id` with `{enabled}`); turning an
  agent on or off doesn't create a new version.
- Deleting an agent asks for confirmation.
- **Create**: name, description, provider (openai · anthropic · openrouter; default openai),
  model (default `gpt-4.1`), system prompt. On success, opens the new agent's editor.
- Clicking a card opens `/agents/:id?tab=config`.

## Editor (`/agents/:id`)
- The starter has one tab, **Config**: name, description, provider, model (from
  `GET /providers/:p/models`), system prompt, enabled. Later lessons add Skills, Evals, Stats
  and CI tabs; the tab stays in `?tab=` for that.
- Saving a config change gives the agent a new version on the server.

## Relied on by e2e
Flow 03: the list shows the seeded "Security Reviewer" (the seed has General, Security and
Performance reviewers).
