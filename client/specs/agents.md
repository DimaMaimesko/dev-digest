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
- Tabs in `?tab=` (an unknown one opens Config). Later lessons add Evals, Stats and CI.
- **Config**: name, description, provider, model (from `GET /providers/:p/models`), system
  prompt, enabled. Saving a config change gives the agent a new version on the server.
- **Skills**: every workspace skill; ticked = linked, linked ones first in prompt order, with
  "N of M attached". Tick links a skill last; drag (≡) or the arrows reorder. Each change saves
  the whole ordered list at once (`POST /agents/:id/skills` with `skill_ids`); saves go one at
  a time, and only the latest waiting change is sent. Reordering is off while filtering. A
  skill disabled in the Skills Lab is marked "left out of prompts". Linking doesn't change the
  agent's version.
- Cards show how many skills the agent links (`skill_count`).

## Relied on by e2e
Flow 03: the list shows the seeded "Security Reviewer" (the seed has General, Security and
Performance reviewers).
