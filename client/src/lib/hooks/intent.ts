/* hooks/intent.ts — Intent Layer. GET /pulls/:id/intent returns the PR's
   stored intent (statement, scope, confidence, sources, unresolved refs), or
   null when none has been derived yet. Invalidated by page.tsx alongside
   pr-runs when a review settles, since a settled run may have derived or
   reused the intent. */
"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { PrIntentResponse } from "../types";

export function usePrIntent(prId: string | number | null | undefined) {
  return useQuery({
    queryKey: ["pr-intent", prId],
    queryFn: () => api.get<PrIntentResponse>(`/pulls/${prId}/intent`),
    enabled: prId != null,
  });
}
