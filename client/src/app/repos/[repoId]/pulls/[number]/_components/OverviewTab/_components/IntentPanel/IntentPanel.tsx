/* IntentPanel — the Overview tab's "why this PR exists" panel (Intent Layer,
   AC-20/21/22). Renders the stored intent: statement, in/out of scope,
   confidence, sources and unresolved references. Every field is rendered as
   plain React text — never Markdown, never dangerouslySetInnerHTML, never an
   <a>/<img> built from intent data, because the intent is untrusted, model
   produced content (specs/intent-layer.md "Untrusted inputs"). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Badge, SectionLabel } from "@devdigest/ui";
import { usePrIntent } from "@/lib/hooks/intent";
import { confidenceColor } from "./helpers";
import { s } from "./styles";

export function IntentPanel({ prId }: { prId: string | number | null | undefined }) {
  const t = useTranslations("intent");
  const { data: intent } = usePrIntent(prId);

  if (!intent) {
    return (
      <section>
        <SectionLabel icon="Target">{t("title")}</SectionLabel>
        <div style={s.empty}>{t("empty")}</div>
      </section>
    );
  }

  const color = confidenceColor(intent.confidence);

  return (
    <section>
      <SectionLabel
        icon="Target"
        right={
          <Badge color={color.c} bg={color.bg}>
            {t("confidenceBadge", { level: intent.confidence })}
          </Badge>
        }
      >
        {t("title")}
      </SectionLabel>
      <div style={s.box}>
        <div style={s.statement}>{intent.intent}</div>

        <div style={s.columns}>
          <div>
            <div style={s.listLabel}>{t("inScope")}</div>
            <ul style={s.list}>
              {intent.in_scope.map((item, i) => (
                <li key={i} style={s.listItem}>
                  {item}
                </li>
              ))}
            </ul>
          </div>
          <div>
            <div style={s.listLabel}>{t("outOfScope")}</div>
            <ul style={s.list}>
              {intent.out_of_scope.map((item, i) => (
                <li key={i} style={s.listItem}>
                  {item}
                </li>
              ))}
            </ul>
          </div>
        </div>

        {intent.sources.length > 0 && (
          <div style={s.metaBlock}>
            <div style={s.listLabel}>{t("sources")}</div>
            <div style={s.tagsRow}>
              {intent.sources.map((src, i) => (
                <span key={i} style={s.tag}>
                  {src.label}
                  {src.truncated ? ` (${t("truncated")})` : ""}
                </span>
              ))}
            </div>
          </div>
        )}

        {intent.unresolved.length > 0 && (
          <div style={s.metaBlock}>
            <div style={s.listLabel}>{t("unresolved")}</div>
            <ul style={s.list}>
              {intent.unresolved.map((u, i) => (
                <li key={i} style={s.listItem}>
                  {u.ref} — {u.reason}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  );
}
