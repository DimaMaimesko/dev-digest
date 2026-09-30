# Writing and debugging flows

The flow format and setup are in `../README.md`. This file covers what the flows depend on
and how to add one without making the suite flaky.

## What each flow depends on

Every text a flow waits for or clicks comes from one of three places. Change one of them and
the flow fails, even though the e2e folder didn't change.

| Flow | Seed (`api/internal/seed/seed.go`) | Client copy | Route / URL state |
|---|---|---|---|
| 01 app boot | at least one repo | "Pull Requests" (`messages/en/prReview.json` `list.title`) | `/` redirects to `/repos/:id/pulls` |
| 02 PR detail | `acme/payments-api` is the **first** repo; PR #482 "Add rate limiting to public API endpoints" | — | `/pulls/482` |
| 03 agents | agent "Security Reviewer" | — | `/agents` |
| 04 findings | PR #482's review: verdict `request_changes`, 2 findings, one "Hardcoded Stripe secret key in commit" | tab button "Agent runs" (hardcoded in `PrDetailHeader.tsx`); the verdict shown as `verdict.replace("_", " ")` and "N findings", both in `ReviewRunAccordion.tsx` | `?tab=findings`; the newest run's accordion open by default |
| 05 diff | PR #482's file `src/config.ts` | tab button "Files changed" (`PrDetailHeader.tsx`) | `?tab=diff` |
| 06 onboarding | — | "Add a repository", "Repository URL" (hardcoded in `AddRepoView.tsx`) | `/onboarding` |
| 07 settings | — | "API Keys", "Feature Models" (`messages/en/settings.json`) | `/settings/api-keys`, `/settings/models` |

So:
- Changing the seed's demo data → run the suite.
- Renaming a tab, a title or the verdict label in the client → grep `specs/` for the old text.
- Moving tab state out of the URL breaks 04 and 05 (`wait --url tab=…`).

## Adding a flow
1. Pick a user journey that works on seeded, read-only data. If it needs new data, add it to
   the seed (idempotent, in `internal/seed`), not to the flow.
2. Create `specs/NN-name.flow.json` with the next number. Start from `open {BASE}/…`; don't rely
   on the page the previous flow left open, even though the session is shared.
3. After each navigation, `wait --url` for the route, then `wait --load networkidle` before
   reading data-driven text.
4. Click by visible text or by role and accessible name (`find role button click --name …`),
   never by CSS selector or position.
5. Write a `description` naming the seed data and copy the flow relies on, and add the row
   above and in the README's coverage table.
6. Run it through `../scripts/e2e.sh`.

## Debugging a failure
- The runner stops a flow at its first failing step and saves `test-results/<flow>-fail.png`.
  CI uploads the folder as an artifact.
- A `wait` that times out means the text or URL never appeared. Check the screenshot first,
  then the three sources in the table.
- Raise the per-step timeout with `E2E_STEP_TIMEOUT` (ms, default 60000) only to rule out
  slowness. A flow that needs more time is usually waiting for the wrong thing.
- To watch it run, set `"headed": true` in `agent-browser.json` locally (don't commit it).
