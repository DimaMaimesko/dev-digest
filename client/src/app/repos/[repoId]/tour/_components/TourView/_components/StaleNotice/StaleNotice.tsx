/* StaleNotice — AC-14: the repo moved past the tour's commit. Names both
   short shas; never regenerates on its own. */
"use client";

import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";
import { shortSha } from "../../helpers";

export function StaleNotice({
  oldSha,
  newSha,
}: {
  oldSha: string | null | undefined;
  newSha: string | null | undefined;
}) {
  const t = useTranslations("onboardingTour");
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        fontSize: 13,
        color: "var(--warn)",
        background: "var(--warn-bg)",
        border: "1px solid var(--warn)",
        borderRadius: 7,
        padding: "8px 12px",
      }}
    >
      <Icon.AlertTriangle size={14} />
      <span>{t("staleNotice", { oldSha: shortSha(oldSha), newSha: shortSha(newSha) })}</span>
    </div>
  );
}
