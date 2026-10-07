/* DiffTab — Files changed tab (Smart Diff). Smart order groups the PR's
   files by role (core, tests, wiring, docs, boilerplate) with per-group
   open-findings counters; Original order is the flat list the API serves.
   Findings shown here are each agent's latest review's findings restricted
   to the listed files (helpers.ts); accept/dismiss goes through the same
   `useFindingAction` the Agent runs tab uses, so both tabs invalidate the
   same `["reviews", prId]` query (AC-19, AC-31). A review started from this
   tab shows its live progress here instead of switching to Agent runs
   (AC-29). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { SectionLabel, Button } from "@devdigest/ui";
import {
  DiffViewer,
  isOpen,
  type DiffCommentApi,
  type DiffFindingApi,
} from "@/components/diff-viewer";
import { usePrComments, useCreatePrComment, useFindingAction } from "@/lib/hooks/reviews";
import { notify } from "@/lib/toast";
import type { FileRole } from "@/lib/file-role";
import { FindingCard } from "../FindingCard";
import { RunStatus } from "../RunStatus";
import { RoleGroup } from "./_components/RoleGroup";
import { DEFAULT_COLLAPSED, groupByRole, shownFindings } from "./helpers";
import { s } from "./styles";
import type { FindingActionKind, FindingRecord, PrFile, ReviewRecord } from "@devdigest/shared";

export type DiffOrder = "smart" | "original";

interface DiffTabProps {
  prId: string | null;
  files: PrFile[];
  /** The PR's own file count. The API lists at most one GitHub page of files,
      so this can exceed `files.length`; the header then reads "N of M". */
  totalFiles?: number;
  /** Inline commenting is offered only on open PRs (GitHub rejects otherwise). */
  canComment?: boolean;
  reviews: ReviewRecord[];
  liveRunIds: string[];
  onRunDone: () => void;
  order: DiffOrder;
  onSetOrder: (order: DiffOrder) => void;
  /** owner/repo + head sha — used to deep-link a finding's file:line to GitHub. */
  repoFullName?: string | null;
  headSha?: string | null;
}

export function DiffTab({
  prId,
  files,
  totalFiles,
  canComment,
  reviews,
  liveRunIds,
  onRunDone,
  order,
  onSetOrder,
  repoFullName,
  headSha,
}: DiffTabProps) {
  const t = useTranslations("smartDiff");
  const { data: comments } = usePrComments(prId);
  const create = useCreatePrComment(prId);
  const findingAction = useFindingAction();
  // Comments start hidden so the diff is clean by default — toggle to reveal.
  const [showComments, setShowComments] = React.useState(false);
  // Group collapse state resets to the AC-7 defaults on reload (plan
  // assumption 15): docs and boilerplate start collapsed.
  const [collapsed, setCollapsed] = React.useState<Record<FileRole, boolean>>(() => ({
    ...DEFAULT_COLLAPSED,
  }));

  const commentCount = comments?.length ?? 0;

  const commenting: DiffCommentApi = {
    comments: comments ?? [],
    canComment: !!canComment && !!prId,
    showComments,
    posting: create.isPending,
    onSubmit: async (input) => {
      try {
        const res = await create.mutateAsync(input);
        setShowComments(true); // a just-posted comment shouldn't stay hidden
        return res;
      } catch (err) {
        notify.error(err instanceof Error ? err.message : "Couldn't post the comment to GitHub.");
        throw err;
      }
    },
  };

  const renderFinding = React.useCallback(
    (f: FindingRecord) => (
      <FindingCard
        key={f.id}
        f={f}
        defaultExpanded={isOpen(f)}
        pending={findingAction.isPending}
        repoFullName={repoFullName}
        headSha={headSha}
        onAction={(action: FindingActionKind) =>
          prId && findingAction.mutate({ findingId: f.id, action, prId })
        }
      />
    ),
    [findingAction, repoFullName, headSha, prId],
  );

  const shown = React.useMemo(() => shownFindings(reviews, files), [reviews, files]);
  const findingsApi: DiffFindingApi = React.useMemo(
    () => ({ findings: shown, renderFinding }),
    [shown, renderFinding],
  );
  const groups = React.useMemo(() => groupByRole(files), [files]);

  const toggleGroup = (role: FileRole) =>
    setCollapsed((prev) => ({ ...prev, [role]: !prev[role] }));

  const liveRun = liveRunIds.length > 0 && (
    <div style={s.liveRun}>
      <RunStatus runIds={liveRunIds} onDone={onRunDone} />
    </div>
  );

  // AC-25: no changed files → the existing "no changed files" empty state,
  // with no header, order switch or groups. A run started from this tab
  // still shows its progress, and its onDone still refetches the reviews.
  if (files.length === 0) {
    return (
      <section>
        {liveRun}
        <DiffViewer files={[]} />
      </section>
    );
  }

  const totalAdditions = files.reduce((n, f) => n + (f.additions ?? 0), 0);
  const totalDeletions = files.reduce((n, f) => n + (f.deletions ?? 0), 0);

  return (
    <section>
      <SectionLabel
        icon="Code"
        right={
          <div style={s.headerActions}>
            {commentCount > 0 && (
              <Button
                kind="ghost"
                size="sm"
                icon={showComments ? "EyeOff" : "Eye"}
                onClick={() => setShowComments((v) => !v)}
              >
                {showComments ? "Hide comments" : "Show comments"} ({commentCount})
              </Button>
            )}
            <div style={s.orderSwitch}>
              <Button
                kind={order === "smart" ? "secondary" : "ghost"}
                size="sm"
                active={order === "smart"}
                aria-pressed={order === "smart"}
                onClick={() => onSetOrder("smart")}
              >
                {t("order.smart")}
              </Button>
              <Button
                kind={order === "original" ? "secondary" : "ghost"}
                size="sm"
                active={order === "original"}
                aria-pressed={order === "original"}
                onClick={() => onSetOrder("original")}
              >
                {t("order.original")}
              </Button>
            </div>
          </div>
        }
      >
        {totalFiles != null && totalFiles > files.length
          ? t("headerPartial", {
              count: files.length,
              total: totalFiles,
              adds: totalAdditions,
              dels: totalDeletions,
            })
          : t("header", { count: files.length, adds: totalAdditions, dels: totalDeletions })}
      </SectionLabel>

      {liveRun}

      {order === "original" ? (
        <DiffViewer files={files} commenting={commenting} findings={findingsApi} />
      ) : (
        <div style={s.groups}>
          {groups.map(({ role, files: groupFiles }) => (
            <RoleGroup
              key={role}
              role={role}
              files={groupFiles}
              commenting={commenting}
              findings={findingsApi}
              collapsed={collapsed[role]}
              onToggle={() => toggleGroup(role)}
            />
          ))}
        </div>
      )}
    </section>
  );
}
