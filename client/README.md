# `@devdigest/web` — the studio (Next.js 15)

The DevDigest UI: import repos, browse pull requests, run and read AI reviews,
and author agents. App Router + React Server/Client components, data via
**TanStack Query** hooks over the Go API (`../api`). (This is the starter surface;
course lessons add the Skills, Memory, Eval, Blast/Brief, multi-agent, CI, and
dashboard screens.)

- **Stack:** Next.js 15 (App Router), React 19, TanStack Query, `next-intl`
  (messages in `messages/<locale>/*.json`), `recharts`, `mermaid`,
  `react-markdown`. UI primitives are vendored under `src/vendor/ui`
  (`@devdigest/ui`) and the API's Zod contracts under `src/vendor/shared`
  (`@devdigest/shared`).
- **API base:** `NEXT_PUBLIC_API_BASE` (default `http://localhost:3001`), used by
  `src/lib/api.ts`. Every data hook lives in `src/lib/hooks/*`.
- **Run:** `pnpm dev` (`:3000`), or `../scripts/dev.sh` for the whole stack.
  **Test:** `pnpm test`. **Typecheck:** `pnpm typecheck`.

Cross-cutting chrome lives in `src/components/app-shell` (nav, breadcrumbs,
`g`-then-key shortcuts). Pages are thin; feature logic sits in colocated
`_components/<Name>/` folders.

## Where to read more

| | |
|---|---|
| [`specs/`](specs) | What each screen must do: [PR list](specs/pr-list.md), [PR detail](specs/pr-detail.md), [agents](specs/agents.md), [onboarding](specs/onboarding.md), [settings](specs/settings.md) |
| [`docs/route-map.md`](docs/route-map.md) | Every route, its URL state and the API calls behind it |
| [`docs/data-layer.md`](docs/data-layer.md) | The fetch client, hooks, query keys, polling, SSE, error handling |
| [`src/vendor/ui/README.md`](src/vendor/ui/README.md) | The design system: components, tokens, theming |
| [`INSIGHTS.md`](INSIGHTS.md) | Lessons learned: build, tests, env |

## Testing

Component tests (`*.test.tsx`, next to the components they cover) run under
vitest + jsdom. They mock the hook modules in `src/lib/hooks`, so they need
neither the API nor a browser. The real browser journeys (client + API + seeded
DB) are covered by the deterministic agent-browser suite in
[`../e2e`](../e2e/README.md) and the `e2e-web.yml` workflow. See
[`../TESTING.md`](../TESTING.md).
