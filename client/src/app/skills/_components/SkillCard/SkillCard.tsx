/* SkillCard — name, type badge, source, agent count, enabled toggle, delete. */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Icon, Badge, Toggle } from "@devdigest/ui";
import type { Skill } from "@devdigest/shared";
import { useDeleteSkill } from "../../../../lib/hooks/skills";
import { typeColor } from "../../../../lib/skill-type";
import { s } from "./styles";

export function SkillCard({
  skill,
  active,
  onClick,
  onToggle,
  onDeleted,
}: {
  skill: Skill;
  active?: boolean;
  onClick?: () => void;
  onToggle?: (enabled: boolean) => void;
  onDeleted?: () => void;
}) {
  const t = useTranslations("skills");
  const del = useDeleteSkill();
  const color = typeColor(skill.type);

  const remove = () => {
    const question =
      skill.agent_count > 0
        ? t("card.deleteConfirmUsed", { name: skill.name, count: skill.agent_count })
        : t("card.deleteConfirm", { name: skill.name });
    if (window.confirm(question)) del.mutate(skill.id, { onSuccess: onDeleted });
  };

  return (
    <div onClick={onClick} style={s.card(!!active, skill.enabled)}>
      <div style={s.headerRow}>
        <div style={s.iconBox(color.c, color.bg)}>
          <Icon.Sparkles size={14} />
        </div>
        <span className="mono" style={s.name}>
          {skill.name}
        </span>
        {onToggle && (
          <div onClick={(e) => e.stopPropagation()}>
            <Toggle on={skill.enabled} onChange={onToggle} size={14} />
          </div>
        )}
        <button
          onClick={(e) => {
            e.stopPropagation();
            remove();
          }}
          disabled={del.isPending}
          title={t("card.delete")}
          aria-label={t("card.delete")}
          style={s.deleteBtn(del.isPending)}
        >
          <Icon.Trash size={14} />
        </button>
      </div>
      <div style={s.description}>{skill.description}</div>
      <div style={s.metaRow}>
        <Badge color={color.c} bg={color.bg}>
          {t(`listItem.type.${skill.type}`)}
        </Badge>
        <span style={s.source}>{t(`listItem.source.${skill.source}`)}</span>
        <span style={s.agents}>{t("card.agentCount", { count: skill.agent_count })}</span>
      </div>
    </div>
  );
}
