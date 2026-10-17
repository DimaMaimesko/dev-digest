/* hooks/onboarding.ts — React Query hooks for the Onboarding Tour (T7):
     GET  /repos/:id/onboarding → OnboardingTourResponse (index readiness, the
                                   stored tour and the latest generation)
     POST /repos/:id/onboarding → starts (or returns the in-progress)
                                   generation, 202 OnboardingGeneration

   Polling matches usePrRuns (docs/data-layer.md "Polling"): while the latest
   generation is queued/running, refetch every 2s so the page shows each
   section's status (and the finished tour) without a reload. */
"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OnboardingGeneration, OnboardingTourResponse } from "../types";

export function onboardingTourKey(repoId: string | null | undefined) {
  return ["onboarding-tour", repoId] as const;
}

/** GET /repos/:id/onboarding. Polls every 2s while a generation is live. */
export function useOnboardingTour(repoId: string | null | undefined) {
  return useQuery({
    queryKey: onboardingTourKey(repoId),
    queryFn: () => api.get<OnboardingTourResponse>(`/repos/${repoId}/onboarding`),
    enabled: !!repoId,
    refetchInterval: (query) => {
      const status = query.state.data?.generation?.status;
      return status === "queued" || status === "running" ? 2000 : false;
    },
  });
}

/** POST /repos/:id/onboarding (no body). Disables Generate/Regenerate while
    pending (AC-34); invalidates the tour query on success so the new/queued
    generation shows up immediately. */
export function useGenerateTour(repoId: string | null | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<OnboardingGeneration>(`/repos/${repoId}/onboarding`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: onboardingTourKey(repoId) });
    },
  });
}
