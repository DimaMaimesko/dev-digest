/* ArchitectureSection — the summary (grounded path segments render as a code
   span + Open link, AC-17/AC-20) and the diagram (structured nodes/edges,
   AC-22). `diagram: null` reads "Diagram unavailable" (AC-23). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import type { ArchitectureContent } from "@/lib/types";
import { OpenLink } from "../OpenLink";
import { ArchitectureDiagram } from "./_components/ArchitectureDiagram";

export function ArchitectureSection({
  content,
  fullName,
  sha,
}: {
  content: ArchitectureContent;
  fullName: string | null | undefined;
  sha: string;
}) {
  const t = useTranslations("onboardingTour");

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <p style={{ fontSize: 13.5, color: "var(--text-secondary)", lineHeight: 1.6, margin: 0 }}>
        {content.summary.map((seg, i) =>
          seg.type === "text" ? (
            <React.Fragment key={i}>{seg.text}</React.Fragment>
          ) : (
            <span key={i} style={{ display: "inline-flex", alignItems: "center", gap: 3, margin: "0 2px" }}>
              <code
                className="mono"
                style={{
                  fontSize: 12.5,
                  background: "var(--bg-elevated)",
                  border: "1px solid var(--border)",
                  borderRadius: 4,
                  padding: "0 4px",
                }}
              >
                {seg.path}
              </code>
              <OpenLink fullName={fullName} sha={sha} path={seg.path} isDir={seg.is_dir} />
            </span>
          ),
        )}
      </p>

      {content.diagram ? (
        <div>
          <ArchitectureDiagram diagram={content.diagram} />
          <div style={{ marginTop: 12, fontSize: 12.5, color: "var(--text-secondary)" }}>
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {content.diagram.nodes.map((n) => (
                <li key={n.id}>
                  {n.label} ({n.kind})
                </li>
              ))}
            </ul>
            {content.diagram.edges.length > 0 && (
              <ul style={{ margin: "6px 0 0", paddingLeft: 18 }}>
                {content.diagram.edges.map((e, i) => (
                  <li key={i}>
                    {e.from} → {e.to}
                    {e.label ? ` (${e.label})` : ""}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      ) : (
        <div style={{ fontSize: 13, color: "var(--text-muted)" }}>{t("diagramUnavailable")}</div>
      )}
    </div>
  );
}
