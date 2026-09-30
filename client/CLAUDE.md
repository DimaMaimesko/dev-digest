# client/ — DevDigest studio (Next.js)

Next.js 15 App Router · React 19 · TanStack Query 5 · next-intl 3 · Tailwind 4 (only via the
design system) · Zod 3 (types only) · vitest 2 + Testing Library + jsdom · pnpm.
Talks to the Go API at `NEXT_PUBLIC_API_BASE` (default `http://localhost:3001`).

## Commands (from client/)
- `pnpm dev`: :3000; needs the API (`../scripts/dev.sh` starts both)
- `pnpm test`: vitest, hermetic (no API, no browser)
- `pnpm typecheck`: CI runs both, `client.yml`

## Map
- `src/app/**/page.tsx`: routes; pages stay thin. Route list → `docs/route-map.md`
- `src/app/**/_components/<Name>/`: feature components, colocated with their route
- `src/components/`: cross-page chrome (app-shell, page-shell, diff-viewer, repo-not-found)
- `src/lib/api.ts`: the only `fetch`. `src/lib/hooks/*`: every query and mutation.
- `src/vendor/shared`: API contracts (`@devdigest/shared`). `src/vendor/ui`: the design system (`@devdigest/ui`).
- `messages/en/<namespace>.json`: UI strings, one file per feature namespace

## Conventions that differ from habit
- A component folder is `<Name>/{Name.tsx, index.ts, constants.ts, helpers.ts, styles.ts, Name.test.tsx}`.
  Sub-components nest in `_components/`.
- Styles are typed `CSSProperties` objects exported as `s` from `styles.ts`, using CSS variables
  (`var(--border)`). No Tailwind classes in app code; `className` is only for helpers like `mono`.
- UI primitives come from `@devdigest/ui` (import the barrel, never a layer file).
- Data: a page calls a hook from `@/lib/hooks`; hooks call `api.get/post/put/del`. Never `fetch`
  in a component. Mutations invalidate their query keys in `onSuccess`.
- Tab, filter and drawer state lives in the URL (`?tab=`, `?status=`, `?trace=`), not in React state.
- Strings: `useTranslations("<namespace>")`. A new feature gets its own `messages/en/<feature>.json`.
- Tests mock the hook modules (`vi.mock(".../lib/hooks/reviews")`), not `fetch`, and wrap in
  `NextIntlClientProvider` with the real messages file.

## Do not touch
- `src/vendor/ui/`: the vendored design system. Use it as it is.
- `src/vendor/shared/`: import **types only**. A runtime value pulls `./contracts/*.js`
  into webpack and breaks the build (that's why `lib/feature-models.ts` mirrors a registry).
  A contract changes only together with the Go API (`../api/specs/http-api.md`).
- `.env`: local. Change `.env.example` instead.

## Gotchas
- PR routes are keyed by PR **number**, but every PR API by the row's UUID: the page resolves
  number → id through the cached PR list first.
- e2e flows find elements by visible English text (tab labels, seeded titles). Renaming UI copy
  can break `../e2e`; run `../scripts/e2e.sh` after changing text on the flows' screens.
- Some copy is still hardcoded in components (tab labels, onboarding), not in `messages/`.
- `messages/en/` and some hooks (`useContextFiles`, `useReindexContext`) are for later course
  lessons; the API doesn't serve those routes yet. Don't delete them, and don't wire them in.

## Read when needed
- Behavior of a screen → `specs/<screen>.md` (pr-list, pr-detail, agents, onboarding, settings)
- Data layer: errors, query keys, polling, SSE → `docs/data-layer.md`
- Routes and the API calls behind each → `docs/route-map.md`
- Design-system components and tokens → `src/vendor/ui/README.md`
- Something strange in tests, build or dev → `INSIGHTS.md`
