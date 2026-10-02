/* Route: /skills (Skills Lab). The skill list, with a prompt to pick one.
   Suspense: the list reads ?tab= to keep it when a skill is picked. */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { EmptyState } from "@devdigest/ui";
import { SkillsLab } from "./_components/SkillsLab";

export default function SkillsPage() {
  const t = useTranslations("skills");
  return (
    <React.Suspense>
      <SkillsLab>
        <EmptyState icon="Sparkles" title={t("page.selectPrompt.title")} body={t("page.selectPrompt.body")} />
      </SkillsLab>
    </React.Suspense>
  );
}
