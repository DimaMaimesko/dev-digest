# e2e/ — browser flows for the web app

Vercel **agent-browser** (Rust CLI over CDP) driven by a tiny runner (`run.ts`, tsx). No
Playwright, no LLM, no API key. Node 22, **npm** (not pnpm: this package has a `package-lock.json`).

## Commands
- `../scripts/e2e.sh`: **the way to run it locally.** Isolated, freshly seeded stack on
  Postgres :5434, API :3101, web :3100; torn down afterwards. Safe next to the dev stack.
- `npm test` (from e2e/): runs the flows against `E2E_BASE_URL` (default :3000). CI does this
  after building its own stack (`e2e-web.yml`).
- `npm run typecheck`: not run in CI; run it after editing `run.ts` or `lib/`.
- Once per machine: `npm i -g agent-browser && agent-browser install`.

## Map
- `specs/NN-name.flow.json`: one flow each, run in file-name order in one shared browser
  session. **These are the specs; there's no separate Markdown spec folder here.**
- `run.ts`: loads the flows, runs each step's command, stops a flow at its first failure,
  screenshots it to `test-results/`.
- `lib/assert.ts`: the `Flow`/`Step` types, `{BASE}` substitution, the substring check.

## Rules for flows
- A step is `{ "cmd": [...agent-browser argv], "label": "...", "assert"?: {"stdoutIncludes": "..."} }`.
  A non-zero exit fails it, so `wait --text` / `wait --url` **are** the assertions.
- Deterministic locators only: `wait --url`, `wait --text`, `wait --load networkidle`,
  `find text|role|label … click`. **Never the AI `chat` command.**
- Read-only: flows use seeded data only and never submit a form that writes or calls a model.
- New flow → the next `NN-` number, a `name`, a `description` that says what it relies on,
  and a row in README's coverage table.

## Gotchas
- Flows depend on text from two other parts: the seed (`../api/internal/seed/seed.go`: PR #482
  "Add rate limiting to public API endpoints", "Security Reviewer", the finding "Hardcoded Stripe
  secret key in commit", `src/config.ts`) and the client's English copy (tab labels "Agent runs",
  "Files changed", section titles). Which flow needs what → `docs/writing-flows.md`.
- Flow 02 follows the home redirect to the *first* repo, so any DB with other repos fails
  02/04/05. Never point `npm test` at the dev database.
- NEVER `docker compose down -v` to get a clean DB: it wipes the dev volume. Use `e2e.sh`.

## Read when needed
- Writing or debugging a flow, and what each flow depends on → `docs/writing-flows.md`
- Setup, env knobs, coverage table → `README.md`
- A flow fails for no visible reason → `INSIGHTS.md`
