"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Markdown } from "@devdigest/ui";
import type { Skill } from "@devdigest/shared";
import { promptText } from "./helpers";
import { s } from "./styles";

/** Preview tab — the saved body rendered as the reviewing agent receives it. */
export function PreviewTab({ skill }: { skill: Skill }) {
  const t = useTranslations("skills");
  return (
    <div style={s.wrap}>
      <h2 style={s.h2}>{t("previewTab.title")}</h2>
      <p style={s.subtitle}>{t("previewTab.subtitle")}</p>
      <div style={s.card}>
        <Markdown>{promptText(skill)}</Markdown>
      </div>
    </div>
  );
}
