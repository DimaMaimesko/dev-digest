/* ArchitectureDiagram — a CSS box layout in columns by node kind, each kind
   colored from a CSS variable (AC-21). No canvas/SVG line routing: the
   adjacent text list of nodes and edges (rendered by ArchitectureSection)
   carries the same information accessibly. */
"use client";

import { useTranslations } from "next-intl";
import type { TourDiagram, TourDiagramNodeKind } from "@/lib/types";

const KIND_ORDER: TourDiagramNodeKind[] = ["entry", "module", "middleware", "datastore", "external"];

const KIND_COLOR: Record<TourDiagramNodeKind, string> = {
  entry: "var(--accent)",
  module: "var(--text-primary)",
  middleware: "var(--warn)",
  datastore: "var(--ok)",
  external: "var(--text-muted)",
};

export function ArchitectureDiagram({ diagram }: { diagram: TourDiagram }) {
  const t = useTranslations("onboardingTour");
  const byKind = KIND_ORDER.map((kind) => ({
    kind,
    nodes: diagram.nodes.filter((n) => n.kind === kind),
  })).filter((g) => g.nodes.length > 0);

  return (
    <div
      role="img"
      aria-label={t("sections.architecture")}
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${byKind.length}, 1fr)`,
        gap: 12,
      }}
    >
      {byKind.map((group) => (
        <div key={group.kind} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {group.nodes.map((node) => (
            <div
              key={node.id}
              style={{
                border: `1px solid ${KIND_COLOR[node.kind]}`,
                color: KIND_COLOR[node.kind],
                borderRadius: 6,
                padding: "6px 10px",
                fontSize: 12.5,
                fontWeight: 600,
                background: "var(--bg-elevated)",
              }}
            >
              {node.label}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
