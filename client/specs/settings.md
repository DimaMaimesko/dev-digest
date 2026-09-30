# Spec — Settings (`/settings/:section`)

Two sections: `api-keys` and `models`.

## API Keys (`/settings/api-keys`)
- Four rows: OpenAI, Anthropic, OpenRouter, GitHub. Each shows **Configured / Not set** from
  `GET /settings/secrets-status`, never the value.
- A password input (with reveal) and **Test connection**: `POST /settings/test-connection`
  with the provider and the typed key, if any. The server saves the key, then tests it; the
  row shows the result message, ok or not.
- A successful test refreshes the status badges and the provider model lists.

## Feature Models (`/settings/models`)
- One picker per system LLM feature (onboarding tour, review intent, risk brief,
  conformance, …). The list comes from `src/lib/feature-models.ts`, a client copy of the
  shared registry.
- The choice is saved to `settings.feature_models` (`PUT /settings`). An unset feature uses its
  registry default.
- Most of these features arrive in later course lessons; the pickers exist already.

## Relied on by e2e
Flow 07 checks the section titles "API Keys" and "Feature Models".
