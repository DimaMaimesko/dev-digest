"use client";

import React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Badge, Checkbox, EmptyState, ErrorState, Icon, Skeleton } from "@devdigest/ui";
import type { Agent, Skill } from "@devdigest/shared";
import { useAgentSkills, useSetAgentSkills } from "../../../../../../../lib/hooks/agents";
import { useSkills } from "../../../../../../../lib/hooks/skills";
import { typeColor } from "../../../../../../../lib/skill-type";
import { arrange, matches, move, toggle } from "./helpers";
import { s } from "./styles";

/** Skills tab — every workspace skill; checked ones are linked to the agent,
 *  in the order its prompt uses them. Each change saves at once. */
export function SkillsTab({ agent }: { agent: Agent }) {
  const t = useTranslations("agents");
  const router = useRouter();
  const skills = useSkills();
  const links = useAgentSkills(agent.id);
  const setSkills = useSetAgentSkills();
  const [search, setSearch] = React.useState("");
  const [dragged, setDragged] = React.useState<string | null>(null);
  const [over, setOver] = React.useState<string | null>(null);

  // The linked IDs in order: the server's, or the change being saved.
  const saved = React.useMemo(() => (links.data ?? []).map((l) => l.skill_id), [links.data]);
  const [pending, setPending] = React.useState<string[] | null>(null);
  const ids = pending ?? saved;

  // Each save replaces the whole list, so saves go one at a time: a change
  // made while one is in flight waits, and only the latest is sent.
  const inFlight = React.useRef(false);
  const queued = React.useRef<string[] | null>(null);
  const send = (next: string[]) => {
    inFlight.current = true;
    setSkills.mutate(
      { id: agent.id, skillIds: next },
      {
        onSettled: () => {
          inFlight.current = false;
          const waiting = queued.current;
          queued.current = null;
          if (waiting) send(waiting);
          else setPending(null);
        },
      },
    );
  };
  const save = (next: string[]) => {
    setPending(next);
    if (inFlight.current) queued.current = next;
    else send(next);
  };

  if (skills.isLoading || links.isLoading) return <Skeleton height={160} />;
  if (skills.isError || links.isError) {
    return <ErrorState body={t("skills.loadError")} onRetry={() => (skills.refetch(), links.refetch())} />;
  }
  if (!skills.data?.length) {
    return (
      <EmptyState
        icon="Sparkles"
        title={t("skills.emptyTitle")}
        body={t("skills.emptyBody")}
        cta={t("skills.emptyCta")}
        onCta={() => router.push("/skills")}
      />
    );
  }

  const { linked, unlinked } = arrange(skills.data, ids);
  const filtering = search.trim() !== "";
  const shownLinked = linked.filter((sk) => matches(sk, search));
  const shownUnlinked = unlinked.filter((sk) => matches(sk, search));

  const drop = (target: string) => {
    if (dragged && dragged !== target) save(move(ids, ids.indexOf(dragged), ids.indexOf(target)));
    setDragged(null);
    setOver(null);
  };

  const row = (sk: Skill, index: number | null) => {
    const isLinked = index !== null;
    const canDrag = isLinked && !filtering;
    const color = typeColor(sk.type);
    return (
      <div
        key={sk.id}
        draggable={canDrag}
        onDragStart={() => setDragged(sk.id)}
        onDragEnd={() => (setDragged(null), setOver(null))}
        onDragOver={(e) => {
          if (!canDrag || !dragged) return;
          e.preventDefault();
          setOver(sk.id);
        }}
        onDrop={() => drop(sk.id)}
        style={s.row(isLinked, dragged === sk.id, over === sk.id && dragged !== sk.id)}
      >
        <span style={s.grip(canDrag)} aria-hidden>
          <Icon.Menu size={14} />
        </span>
        <Checkbox
          checked={isLinked}
          onChange={() => save(toggle(ids, sk.id))}
          label={
            <Link href={`/skills/${sk.id}`} className="mono" style={s.name}>
              {sk.name}
            </Link>
          }
        />
        {!sk.enabled && <span style={s.disabledNote}>{t("skills.disabledNote")}</span>}
        <div style={s.right}>
          {isLinked && (
            <>
              <button
                type="button"
                aria-label={t("skills.moveUp", { name: sk.name })}
                disabled={filtering || index === 0}
                onClick={() => save(move(ids, index, index - 1))}
                style={s.arrow(!filtering && index > 0)}
              >
                <Icon.ArrowUp size={13} />
              </button>
              <button
                type="button"
                aria-label={t("skills.moveDown", { name: sk.name })}
                disabled={filtering || index === ids.length - 1}
                onClick={() => save(move(ids, index, index + 1))}
                style={s.arrow(!filtering && index < ids.length - 1)}
              >
                <Icon.ArrowDown size={13} />
              </button>
            </>
          )}
          <Badge color={color.c} bg={color.bg}>
            {sk.type}
          </Badge>
        </div>
      </div>
    );
  };

  return (
    <div style={s.wrap}>
      <div style={s.header}>
        <h2 style={s.h2}>{t("skills.title")}</h2>
        <Badge color="var(--accent)" bg="var(--accent-bg)">
          {t("skills.enabledCount", { linked: linked.length, total: skills.data.length })}
        </Badge>
        <div style={s.search}>
          <Icon.Search size={13} style={s.searchIcon} />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("skills.filterPlaceholder")}
            style={s.searchInput}
          />
        </div>
      </div>
      <p style={s.hint}>{filtering ? t("skills.filterHint") : t("skills.orderHint")}</p>
      <div style={s.list}>
        {shownLinked.map((sk) => row(sk, ids.indexOf(sk.id)))}
        {shownLinked.length > 0 && shownUnlinked.length > 0 && <div style={s.divider} />}
        {shownUnlinked.map((sk) => row(sk, null))}
        {shownLinked.length + shownUnlinked.length === 0 && <div style={s.noMatch}>{t("skills.noMatch")}</div>}
      </div>
    </div>
  );
}
