"use client";

import React from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { Badge, Button, FormField, Icon, SelectInput, Textarea, TextInput, Toggle } from "@devdigest/ui";
import type { Skill, SkillType } from "@devdigest/shared";
import { useSkillAgents, useUpdateSkill } from "../../../../../../../lib/hooks/skills";
import { useToast } from "../../../../../../../lib/toast";
import { SKILL_TYPES } from "./constants";
import { estimateTokens } from "./helpers";
import { s } from "./styles";

/** Config tab — edit a skill. A changed body is saved as a new version, with an optional note. */
export function ConfigTab({ skill }: { skill: Skill }) {
  const t = useTranslations("skills");
  const toast = useToast();
  const update = useUpdateSkill();
  const { data: agents } = useSkillAgents(skill.id);
  const [name, setName] = React.useState(skill.name);
  const [description, setDescription] = React.useState(skill.description);
  const [type, setType] = React.useState<SkillType>(skill.type);
  const [body, setBody] = React.useState(skill.body);
  const [enabled, setEnabled] = React.useState(skill.enabled);
  const [message, setMessage] = React.useState("");

  const bodyChanged = body !== skill.body;
  const typeOptions = SKILL_TYPES.map((v) => ({ value: v, label: t(`listItem.type.${v}`) }));

  const save = () =>
    update.mutate(
      {
        id: skill.id,
        patch: {
          name: name.trim(),
          description,
          type,
          body,
          enabled,
          ...(bodyChanged && message.trim() ? { message: message.trim() } : {}),
        },
      },
      {
        onSuccess: (data) => {
          setMessage("");
          toast.success(t("config.savedToast", { version: data.version }));
        },
      },
    );

  return (
    <div style={s.wrap}>
      <div style={s.header}>
        <h2 style={s.h2}>{t("config.title")}</h2>
        <Badge mono>{t("preview.version", { version: skill.version })}</Badge>
        <label style={s.enabledLabel} title={t("config.enabledHint")}>
          {t("config.enabled")}
          <Toggle on={enabled} onChange={setEnabled} size={16} />
        </label>
      </div>
      <FormField label={t("config.name")} required>
        <TextInput value={name} onChange={setName} mono />
      </FormField>
      <FormField label={t("config.description")}>
        <TextInput value={description} onChange={setDescription} />
      </FormField>
      <FormField label={t("config.type")}>
        <SelectInput value={type} onChange={(v) => setType(v as SkillType)} options={typeOptions} />
      </FormField>
      <FormField label={t("config.source")}>
        <TextInput value={t(`listItem.source.${skill.source}`)} readOnly />
      </FormField>
      <FormField label={t("config.body")} hint={t("config.bodyHint")} required>
        <div style={s.bodyHead}>
          <Icon.FileText size={13} />
          <span className="mono">{skill.name}.md</span>
          {bodyChanged && <Badge>{t("config.unsaved")}</Badge>}
          <span style={s.tokens}>{t("config.tokens", { count: estimateTokens(body) })}</span>
        </div>
        <Textarea value={body} onChange={setBody} rows={16} mono />
      </FormField>
      {bodyChanged && (
        <FormField label={t("config.message")} hint={t("config.messageHint")}>
          <TextInput value={message} onChange={setMessage} placeholder={t("config.messagePlaceholder")} />
        </FormField>
      )}
      <FormField label={t("config.usedBy")}>
        {agents && agents.length > 0 ? (
          <div style={s.agents}>
            {agents.map((a) => (
              <Link key={a.id} href={`/agents/${a.id}`} style={s.agentLink}>
                <Badge icon="Cpu">{a.name}</Badge>
              </Link>
            ))}
          </div>
        ) : (
          <span style={s.none}>{t("config.usedByNone")}</span>
        )}
      </FormField>
      <div style={s.actions}>
        <Button
          kind="primary"
          icon="Check"
          onClick={save}
          disabled={update.isPending || name.trim() === "" || body.trim() === ""}
        >
          {update.isPending ? t("config.saving") : t("config.save")}
        </Button>
      </div>
    </div>
  );
}
