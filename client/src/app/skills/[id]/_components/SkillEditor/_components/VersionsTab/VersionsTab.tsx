"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Badge, Button, ErrorState, Skeleton } from "@devdigest/ui";
import type { Skill, SkillVersion } from "@devdigest/shared";
import { useRestoreSkillVersion, useSkillVersions } from "../../../../../../../lib/hooks/skills";
import { useToast } from "../../../../../../../lib/toast";
import { s } from "./styles";

/** Versions tab — the body history, newest first. Restoring a version saves its body as the next one. */
export function VersionsTab({ skill }: { skill: Skill }) {
  const t = useTranslations("skills");
  const { data: versions, isLoading, isError, refetch } = useSkillVersions(skill.id);

  return (
    <div style={s.wrap}>
      <div style={s.titleRow}>
        <h2 style={s.h2}>{t("versions.title")}</h2>
        {versions && <Badge>{t("versions.count", { count: versions.length })}</Badge>}
      </div>
      <p style={s.subtitle}>{t("versions.subtitle")}</p>
      {isLoading && <Skeleton height={64} />}
      {isError && <ErrorState body={t("detail.loadError")} onRetry={() => refetch()} />}
      <div style={s.list}>
        {versions?.map((v) => (
          <VersionRow key={v.version} skill={skill} v={v} />
        ))}
      </div>
    </div>
  );
}

function VersionRow({ skill, v }: { skill: Skill; v: SkillVersion }) {
  const t = useTranslations("skills");
  const toast = useToast();
  const restore = useRestoreSkillVersion();
  const [open, setOpen] = React.useState(false);
  const current = v.version === skill.version;

  const onRestore = () =>
    restore.mutate(
      { id: skill.id, version: v.version },
      { onSuccess: (data) => toast.success(t("versions.restoredToast", { from: v.version, version: data.version })) },
    );

  return (
    <div style={s.row}>
      <div style={s.rowHead}>
        <Badge mono color={current ? "var(--accent)" : undefined} bg={current ? "var(--accent-bg)" : undefined}>
          {t("preview.version", { version: v.version })}
        </Badge>
        <div style={s.text}>
          <div style={s.message(!v.message)}>{v.message ?? t("versions.noMessage")}</div>
          <div style={s.date}>{new Date(v.created_at).toLocaleString()}</div>
        </div>
        <Button kind="ghost" size="sm" icon={open ? "EyeOff" : "Eye"} onClick={() => setOpen(!open)}>
          {open ? t("versions.hide") : t("versions.show")}
        </Button>
        {current ? (
          <Badge dot color="var(--ok)" bg="var(--ok-bg)">
            {t("versions.current")}
          </Badge>
        ) : (
          <Button kind="secondary" size="sm" icon="History" onClick={onRestore} disabled={restore.isPending}>
            {restore.isPending ? t("versions.restoring") : t("versions.restore")}
          </Button>
        )}
      </div>
      {open && (
        <pre className="mono" style={s.body}>
          {v.body}
        </pre>
      )}
    </div>
  );
}
