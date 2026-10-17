/* TourHeader — repo name, how the tour was generated (files indexed, sha,
   model, cost, relative time) and the Regenerate action (AC-13, AC-31, AC-42:
   no Share). */
"use client";

import { useTranslations } from "next-intl";
import { Badge, Button } from "@devdigest/ui";
import type { OnboardingTour } from "@/lib/types";
import { formatCost } from "@/lib/format-cost";
import { relativeTime } from "@/app/repos/[repoId]/pulls/helpers";
import { shortSha } from "../../helpers";
import { s } from "./styles";

export function TourHeader({
  fullName,
  tour,
  disabled,
  pending,
  onRegenerate,
}: {
  fullName: string | null | undefined;
  tour: OnboardingTour;
  disabled: boolean;
  pending: boolean;
  onRegenerate: () => void;
}) {
  const t = useTranslations("onboardingTour");
  return (
    <div style={s.header}>
      <div>
        <h1 style={s.title}>{t("header.title", { name: fullName })}</h1>
        <p style={s.subtitle}>
          {t("header.filesIndexed", { count: tour.files_indexed })} ·{" "}
          {t("header.generatedAt", { when: relativeTime(tour.generated_at) })}
        </p>
        <div style={s.badgeRow}>
          <Badge mono icon="GitCommit">
            {shortSha(tour.commit_sha)}
          </Badge>
          <Badge icon="Cpu">{t("header.model", { provider: tour.provider, model: tour.model })}</Badge>
          <Badge icon="DollarSign" mono>
            {formatCost(tour.cost_usd)}
          </Badge>
        </div>
        {tour.code_index_empty ? (
          <div style={s.note}>{t("header.emptyIndex")}</div>
        ) : tour.index_status === "partial" ? (
          <div style={s.note}>{t("header.partialIndex")}</div>
        ) : null}
      </div>
      <Button kind="secondary" icon="RefreshCw" onClick={onRegenerate} disabled={disabled} loading={pending}>
        {t("header.regenerate")}
      </Button>
    </div>
  );
}
