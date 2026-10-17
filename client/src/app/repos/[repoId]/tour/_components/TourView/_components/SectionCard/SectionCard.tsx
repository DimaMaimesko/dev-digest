/* SectionCard — one tour section (architecture, critical paths, run locally,
   reading path, first tasks). Expanded by default (AC-38); the header is a
   real <button aria-expanded> for keyboard + screen reader support. Shows the
   section's status as text (never color alone) and, when grounding dropped
   anything, "{n} unverified references removed" (AC-18). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Icon, type IconName } from "@devdigest/ui";
import type { TourSectionKind } from "@/lib/types";

export function SectionCard({
  kind,
  icon,
  status,
  reason,
  removedRefs,
  children,
}: {
  kind: TourSectionKind;
  icon: IconName;
  status: "done" | "failed";
  reason: string | null;
  removedRefs: number;
  children: React.ReactNode;
}) {
  const t = useTranslations("onboardingTour");
  const [open, setOpen] = React.useState(true);
  const I = Icon[icon];

  return (
    <section
      id={`tour-section-${kind}`}
      data-section-kind={kind}
      style={{
        border: "1px solid var(--border)",
        borderRadius: 10,
        background: "var(--bg-surface)",
        overflow: "hidden",
        scrollMarginTop: 16,
      }}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        style={{
          width: "100%",
          display: "flex",
          alignItems: "center",
          gap: 10,
          padding: "13px 16px",
          background: "none",
          border: "none",
          cursor: "pointer",
          color: "var(--text-primary)",
          textAlign: "left",
        }}
      >
        <I size={15} style={{ color: "var(--text-muted)" }} />
        <span style={{ fontWeight: 600, fontSize: 14 }}>{t(`sections.${kind}`)}</span>
        <span style={{ fontSize: 12.5, color: status === "failed" ? "var(--crit)" : "var(--text-muted)" }}>
          {t(`sectionStatus.${status}`)}
        </span>
        {removedRefs > 0 && (
          <span style={{ fontSize: 12, color: "var(--text-muted)" }}>{t("removedRefs", { count: removedRefs })}</span>
        )}
        <span style={{ flex: 1 }} />
        <Icon.ChevronDown
          size={16}
          style={{ transform: open ? "rotate(180deg)" : "none", transition: "transform .15s", color: "var(--text-muted)" }}
        />
      </button>
      {open && (
        <div style={{ padding: "0 16px 16px" }}>
          {status === "failed" ? (
            <div style={{ fontSize: 13, color: "var(--text-secondary)" }}>{reason}</div>
          ) : (
            children
          )}
        </div>
      )}
    </section>
  );
}
