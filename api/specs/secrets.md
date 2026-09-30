# Spec — API keys and tokens

`internal/secrets`. Four secrets: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`,
`GITHUB_TOKEN`.

## Where they come from
1. `~/.devdigest/secrets.json`: what the user saved in the web app's Settings. It wins.
2. The environment (`api/.env`, loaded by `dev.sh`). An empty value in the file falls back
   to the environment.
3. `GITHUB_TOKEN` falls back to `GITHUB_PAT` when it is unset **or empty** (`.env.example`
   ships it empty).

Secrets never go to the database, the logs or an API response.
`GET /settings/secrets-status` says only whether each one is set.

## The file
- Read on each use (it's tiny), so a hand edit shows up at once.
- Invalid JSON is an error naming the file (the request fails with 500), not "every key unset".
- Saved atomically: written to a temp file, then renamed over it, one save at a time.
  Always readable only by its owner (0600).

## `POST /settings/test-connection`
Body: `{"provider": "openai" | "anthropic" | "openrouter" | "github", "key"?: "…"}`.
- A `key`, when sent, is saved first; then the **saved** secret is tested.
- Always 200: `{"provider", "ok", "message"}`. A failed test is `"ok": false` with the reason.
- GitHub: `GET /user` → "Connected as @login".
- Model providers: the model list → "OK — N models available". OpenRouter's list answers
  any key, so its key is checked with `GET /key` first.
- A missing secret → `"ok": false`, "<NAME> is not configured".

## Keys for clones
The GitHub token goes to git as an HTTP header for each command and is never written into a
clone. A clone must never wait for a password: without a token, a private repo fails at once.
