/* IntentTrace — the PR-intent this run used (or didn't): status, confidence,
   the intent model and the sources/unresolved refs gathered for it. Rendered
   inside TraceBody only when the trace carries an `intent` (absent on traces
   saved before this feature). Every field is a plain text child — no Markdown
   renderer, no `<a>`/`<img>` built from intent data (AC-22). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Badge } from "@devdigest/ui";
import type { RunIntent } from "@devdigest/shared";
import { TraceSection } from "../TraceSection";
import { Row } from "../atoms";
import { s } from "./styles";

const CONFIDENCE_COLOR: Record<string, string> = {
  high: "var(--ok)",
  medium: "var(--warn)",
  low: "var(--crit)",
};

export function IntentTrace({ intent }: { intent: RunIntent }) {
  const t = useTranslations("runs");
  const statusLabel =
    intent.status === "derived"
      ? t("trace.intent.derived")
      : intent.status === "reused"
        ? t("trace.intent.reused")
        : t("trace.intent.failed", { reason: intent.reason ?? "" });
  const statusColor = intent.status === "failed" ? "var(--crit)" : "var(--ok)";

  return (
    <TraceSection
      icon="Gauge"
      title={t("trace.intent.title")}
      right={
        <Badge color={statusColor} bg="var(--bg-hover)">
          {statusLabel}
        </Badge>
      }
    >
      <div style={s.wrap}>
        {intent.confidence != null && (
          <Row label={t("trace.intent.confidence")}>
            <Badge color={CONFIDENCE_COLOR[intent.confidence] ?? "var(--text-muted)"} bg="var(--bg-hover)">
              {intent.confidence}
            </Badge>
          </Row>
        )}
        <Row label={t("trace.intent.model")}>
          <span className="mono">
            {intent.provider ?? "—"} / {intent.model ?? "—"}
          </span>
        </Row>
        <Row label={t("trace.intent.sources")}>
          {intent.sources.length === 0 ? (
            <span style={s.none}>{t("trace.intent.none")}</span>
          ) : (
            <div style={s.list}>
              {intent.sources.map((src, i) => (
                <span key={i} className="mono" style={s.chip}>
                  {src.kind}: {src.label}
                  {src.truncated ? " (truncated)" : ""}
                </span>
              ))}
            </div>
          )}
        </Row>
        <Row label={t("trace.intent.unresolved")}>
          {intent.unresolved.length === 0 ? (
            <span style={s.none}>{t("trace.intent.none")}</span>
          ) : (
            <div style={s.list}>
              {intent.unresolved.map((u, i) => (
                <span key={i} className="mono" style={s.chip}>
                  {u.ref} — {u.reason}
                </span>
              ))}
            </div>
          )}
        </Row>
      </div>
    </TraceSection>
  );
}
