/* Onboarding Tour — /repos/:repoId/tour. Stays thin: resolves :repoId and
   renders RepoNotFound for an unknown repo (as the pulls page does), or the
   TourView otherwise. */
"use client";

import { useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { AppShell } from "@/components/app-shell";
import { RepoNotFound } from "@/components/repo-not-found";
import { useActiveRepo, useRepoNotFound } from "@/lib/repo-context";
import { TourView } from "./_components/TourView";

export default function TourPage() {
  const t = useTranslations("onboardingTour");
  const params = useParams<{ repoId: string }>();
  const repoId = params.repoId;
  const { activeRepo } = useActiveRepo();
  const repoNotFound = useRepoNotFound(repoId);
  const repoName = activeRepo?.full_name ?? repoId;

  if (repoNotFound) {
    return (
      <AppShell crumb={[{ label: repoName, mono: true }, { label: t("breadcrumb") }]}>
        <RepoNotFound />
      </AppShell>
    );
  }

  return (
    <AppShell crumb={[{ label: repoName, mono: true }, { label: t("breadcrumb") }]}>
      <TourView repoId={repoId} fullName={repoName} />
    </AppShell>
  );
}
