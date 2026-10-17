/* IndexNotice — AC-3: why Generate/Regenerate is disabled when the repo
   isn't indexed yet or indexing is still running. The literal strings are
   relied on by e2e (flow 09). */
"use client";

import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";

export function IndexNotice({ state }: { state: "not_indexed" | "indexing" }) {
  const t = useTranslations("onboardingTour");
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        fontSize: 13,
        color: "var(--text-secondary)",
        background: "var(--bg-elevated)",
        border: "1px solid var(--border)",
        borderRadius: 7,
        padding: "8px 12px",
      }}
    >
      <Icon.Info size={14} style={{ color: "var(--text-muted)" }} />
      <span>{state === "not_indexed" ? t("indexNotice.notIndexed") : t("indexNotice.indexing")}</span>
    </div>
  );
}
