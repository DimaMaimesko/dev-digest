/* TourView — the Onboarding Tour screen body (everything under AppShell).
   Reads GET /repos/:id/onboarding (index readiness + stored tour + latest
   generation) and polls it while a generation is live. */
"use client";

import { useTranslations } from "next-intl";
import { Button, ErrorState, Icon, Skeleton } from "@devdigest/ui";
import { ApiError } from "@/lib/api";
import { formatCost } from "@/lib/format-cost";
import { useOnboardingTour, useGenerateTour } from "@/lib/hooks/onboarding";
import { SECTION_ORDER, SECTION_ICON } from "./constants";
import { isStale } from "./helpers";
import { s } from "./styles";
import { TourHeader } from "./_components/TourHeader";
import { StaleNotice } from "./_components/StaleNotice";
import { IndexNotice } from "./_components/IndexNotice";
import { GenerationBanner } from "./_components/GenerationBanner";
import { OnThisPage } from "./_components/OnThisPage";
import { SectionCard } from "./_components/SectionCard";
import { ArchitectureSection } from "./_components/ArchitectureSection";
import { PathListSection } from "./_components/PathListSection";
import { RunLocallySection } from "./_components/RunLocallySection";
import { FirstTasksSection } from "./_components/FirstTasksSection";

export function TourView({
  repoId,
  fullName,
}: {
  repoId: string | null | undefined;
  fullName: string | null | undefined;
}) {
  const t = useTranslations("onboardingTour");
  const { data, isLoading, isError, error, refetch } = useOnboardingTour(repoId);
  const generate = useGenerateTour(repoId);

  if (isLoading) {
    return (
      <div style={s.loadingStack}>
        <Skeleton height={80} />
        <Skeleton height={160} />
        <Skeleton height={160} />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <ErrorState
        body={error instanceof ApiError ? error.message : undefined}
        onRetry={() => refetch()}
      />
    );
  }

  const { index, tour, generation } = data;
  const stale = isStale(tour?.commit_sha, index.commit_sha);
  const indexReady = index.state === "ready";
  const live = generation?.status === "queued" || generation?.status === "running";
  const pending = generate.isPending || live;
  const generateDisabled = !indexReady || pending;

  const onGenerate = () => {
    if (tour && !stale) {
      const ok = window.confirm(t("header.regenerateConfirm", { cost: formatCost(tour.cost_usd) }));
      if (!ok) return;
    }
    generate.mutate();
  };

  const mutationError = generate.error instanceof ApiError ? generate.error : null;

  return (
    <div style={s.layout}>
      <div style={s.main}>
        {tour ? (
          <TourHeader fullName={fullName} tour={tour} disabled={generateDisabled} pending={pending} onRegenerate={onGenerate} />
        ) : (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              textAlign: "center",
              gap: 8,
              padding: "60px 28px",
            }}
          >
            <div
              style={{
                width: 44,
                height: 44,
                borderRadius: 10,
                display: "grid",
                placeItems: "center",
                background: "var(--bg-elevated)",
                border: "1px solid var(--border)",
                color: "var(--text-muted)",
                marginBottom: 8,
              }}
            >
              <Icon.FileText size={22} />
            </div>
            <div style={{ fontSize: 15, fontWeight: 600, color: "var(--text-primary)" }}>
              {t("emptyState.title")}
            </div>
            <div style={{ fontSize: 14, color: "var(--text-secondary)", maxWidth: 360, lineHeight: 1.5 }}>
              {t("emptyState.body")}
            </div>
            <div style={{ marginTop: 12 }}>
              <Button kind="secondary" icon="Sparkles" onClick={onGenerate} disabled={generateDisabled} loading={pending}>
                {t("header.generate")}
              </Button>
            </div>
          </div>
        )}

        {stale && tour && <StaleNotice oldSha={tour.commit_sha} newSha={index.commit_sha} />}

        {!indexReady && <IndexNotice state={index.state === "indexing" ? "indexing" : "not_indexed"} />}

        {mutationError?.code === "config_error" && (
          <GenerationBanner kind="config_error" message={mutationError.message} />
        )}

        {live && <GenerationBanner kind={tour ? "regenerating" : "generating"} />}

        {!live && generation?.status === "failed" && (
          <GenerationBanner kind="failed" message={generation.error} />
        )}

        {tour &&
          tour.sections.map((section) => (
            <SectionCard
              key={section.kind}
              kind={section.kind}
              icon={SECTION_ICON[section.kind]}
              status={section.status}
              reason={section.reason}
              removedRefs={section.removed_refs}
            >
              {section.status === "done" && section.content && (
                <>
                  {section.kind === "architecture" && (
                    <ArchitectureSection content={section.content} fullName={fullName} sha={tour.commit_sha} />
                  )}
                  {(section.kind === "critical_paths" || section.kind === "reading_path") && (
                    <PathListSection content={section.content} fullName={fullName} sha={tour.commit_sha} />
                  )}
                  {section.kind === "run_locally" && <RunLocallySection content={section.content} />}
                  {section.kind === "first_tasks" && (
                    <FirstTasksSection content={section.content} fullName={fullName} sha={tour.commit_sha} />
                  )}
                </>
              )}
            </SectionCard>
          ))}
      </div>

      {tour && (
        <div style={s.side}>
          <OnThisPage kinds={SECTION_ORDER.filter((k) => tour.sections.some((s2) => s2.kind === k))} />
        </div>
      )}
    </div>
  );
}
