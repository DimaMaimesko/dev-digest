/* RunLocallySection — numbered run-locally steps with a CopyButton per
   command, plus the fixed caution note (AC-25–AC-27). Commands are never
   clipped server-side, so they render as-is, with no grounding (no paths). */
"use client";

import { useTranslations } from "next-intl";
import type { RunLocallyContent } from "@/lib/types";
import { CopyButton } from "./_components/CopyButton";

export function RunLocallySection({ content }: { content: RunLocallyContent }) {
  const t = useTranslations("onboardingTour");

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <ol style={{ margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 10 }}>
        {content.steps.map((step, i) => (
          <li
            key={i}
            style={{
              listStyle: "none",
              display: "flex",
              alignItems: "center",
              gap: 10,
              background: "var(--bg-elevated)",
              border: "1px solid var(--border)",
              borderRadius: 7,
              padding: "8px 10px",
            }}
          >
            <span className="mono tnum" style={{ fontSize: 12, color: "var(--text-muted)", width: 16 }}>
              {i + 1}
            </span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <code className="mono" style={{ fontSize: 13, color: "var(--text-primary)" }}>
                {step.command}
              </code>
              {step.comment && (
                <div style={{ fontSize: 12, color: "var(--text-muted)", marginTop: 2 }}>{step.comment}</div>
              )}
            </div>
            <CopyButton command={step.command} />
          </li>
        ))}
      </ol>
      <div style={{ fontSize: 12.5, color: "var(--text-muted)" }}>{t("runLocallyNote")}</div>
    </div>
  );
}
