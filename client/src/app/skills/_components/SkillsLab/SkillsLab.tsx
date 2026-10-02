/* SkillsLab — the Skills screens' frame: the skill list on the left (search,
   create, enable, delete) and the route's content on the right. Selecting a
   skill keeps the current ?tab=. */
"use client";

import React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Button, Dropdown, EmptyState, ErrorState, Icon, Skeleton } from "@devdigest/ui";
import { AppShell } from "../../../../components/app-shell";
import { useSkills, useUpdateSkill } from "../../../../lib/hooks/skills";
import { SkillCard } from "../SkillCard";
import { CreateSkillModal } from "../CreateSkillModal";
import { filterSkills } from "./helpers";
import { s } from "./styles";

export function SkillsLab({
  selectedId,
  crumbLabel,
  children,
}: {
  selectedId?: string;
  /** The last breadcrumb, such as the selected skill's name. */
  crumbLabel?: string;
  children: React.ReactNode;
}) {
  const t = useTranslations("skills");
  const router = useRouter();
  const search = useSearchParams();
  const { data: skills, isLoading, isError, refetch } = useSkills();
  const update = useUpdateSkill();
  const [creating, setCreating] = React.useState(false);
  const [query, setQuery] = React.useState("");

  const tab = search.get("tab") ?? "config";
  const list = filterSkills(skills ?? [], query);
  const crumb = [
    { label: t("page.crumbLab") },
    { label: t("page.crumbSkills"), href: "/skills" },
    ...(crumbLabel ? [{ label: crumbLabel }] : []),
  ];

  return (
    <AppShell crumb={crumb}>
      {creating && <CreateSkillModal onClose={() => setCreating(false)} />}
      <div style={s.frame}>
        <div style={s.list}>
          <div style={s.listHead}>
            <div style={s.titleRow}>
              <h1 style={s.h1}>{t("page.heading")}</h1>
              <Dropdown
                width={210}
                align="right"
                trigger={
                  <Button kind="primary" size="sm" icon="Plus" iconRight="ChevronDown">
                    {t("page.addSkill")}
                  </Button>
                }
                items={[{ label: t("page.createSkill"), icon: "Edit", onClick: () => setCreating(true) }]}
              />
            </div>
            <div style={s.search}>
              <Icon.Search size={13} style={s.searchIcon} />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("page.searchPlaceholder")}
                style={s.searchInput}
              />
            </div>
          </div>
          <div style={s.cards}>
            {isLoading && (
              <div style={s.skeletons}>
                <Skeleton height={96} />
                <Skeleton height={96} />
                <Skeleton height={96} />
              </div>
            )}
            {isError && <ErrorState body={t("page.loadError")} onRetry={() => refetch()} />}
            {!isLoading && !isError && skills?.length === 0 && (
              <EmptyState
                icon="Sparkles"
                title={t("page.emptyManual.title")}
                body={t("page.emptyManual.body")}
                cta={t("page.emptyManual.cta")}
                onCta={() => setCreating(true)}
              />
            )}
            {!!skills?.length && list.length === 0 && <div style={s.noMatch}>{t("page.noMatch")}</div>}
            {list.map((sk) => (
              <SkillCard
                key={sk.id}
                skill={sk}
                active={sk.id === selectedId}
                onClick={() => router.push(`/skills/${sk.id}?tab=${tab}`)}
                onToggle={(enabled) => update.mutate({ id: sk.id, patch: { enabled } })}
                onDeleted={sk.id === selectedId ? () => router.push("/skills") : undefined}
              />
            ))}
          </div>
        </div>
        <div style={s.pane}>{children}</div>
      </div>
    </AppShell>
  );
}
