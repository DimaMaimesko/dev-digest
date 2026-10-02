/* Route: /skills/:id (Skill editor). The skill list on the left, the selected
   skill's Config / Preview / Versions tabs on the right; the tab is ?tab=. */
"use client";

import React from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Badge, ErrorState, Icon, Skeleton } from "@devdigest/ui";
import { ApiError } from "../../../lib/api";
import { useSkill } from "../../../lib/hooks/skills";
import { SkillsLab } from "../_components/SkillsLab";
import { typeColor } from "../_components/SkillCard/helpers";
import { SkillEditor } from "./_components/SkillEditor";
import { TAB_KEYS } from "./_components/SkillEditor/constants";

export default function SkillPage() {
  const t = useTranslations("skills");
  const { id } = useParams<{ id: string }>();
  const search = useSearchParams();
  const router = useRouter();
  const { data: skill, isLoading, isError, error, refetch } = useSkill(id);

  const requested = search.get("tab") ?? "";
  const tab = TAB_KEYS.includes(requested) ? requested : "config";
  const setTab = (next: string) => {
    const sp = new URLSearchParams(search.toString());
    sp.set("tab", next);
    router.replace(`/skills/${id}?${sp.toString()}`);
  };

  let pane: React.ReactNode;
  if (isError && error instanceof ApiError && error.status === 404) {
    pane = <ErrorState fullScreen title={t("detail.notFound.title")} body={t("detail.notFound.body")} />;
  } else if (isError) {
    pane = <ErrorState fullScreen body={t("detail.loadError")} onRetry={() => refetch()} />;
  } else if (isLoading || !skill) {
    pane = (
      <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 16 }}>
        <Skeleton height={24} width={240} />
        <Skeleton height={200} />
      </div>
    );
  } else {
    const color = typeColor(skill.type);
    pane = (
      <>
        <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "16px 28px 0", flexShrink: 0 }}>
          <Icon.Sparkles size={18} style={{ color: color.c }} />
          <h1 className="mono" style={{ fontSize: 18, fontWeight: 700 }}>
            {skill.name}
          </h1>
          <Badge color={color.c} bg={color.bg}>
            {t(`listItem.type.${skill.type}`)}
          </Badge>
          <Badge mono>{t("preview.version", { version: skill.version })}</Badge>
          {!skill.enabled && <Badge color="var(--text-muted)">{t("card.disabled")}</Badge>}
        </div>
        <div style={{ flex: 1, minHeight: 0, overflow: "auto" }}>
          <SkillEditor skill={skill} tab={tab} onTab={setTab} />
        </div>
      </>
    );
  }

  return (
    <SkillsLab selectedId={id} crumbLabel={skill?.name ?? t("detail.crumbSkill")}>
      {pane}
    </SkillsLab>
  );
}
