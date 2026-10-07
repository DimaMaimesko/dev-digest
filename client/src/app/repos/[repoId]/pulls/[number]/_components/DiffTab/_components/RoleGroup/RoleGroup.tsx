/* RoleGroup — one collapsible role group on the Files changed tab (Smart
   order): header (role label, one-line description, open-findings counter,
   "N files") + its files rendered through the shared DiffViewer when
   expanded. Collapse state is owned by the parent (DiffTab), so one group's
   toggle never touches another's (AC-8). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";
import type { PrFile } from "@devdigest/shared";
import {
  DiffViewer,
  pathsWithOpenFindings,
  type DiffCommentApi,
  type DiffFindingApi,
} from "@/components/diff-viewer";
import type { FileRole } from "@/lib/file-role";
import { OPEN_FINDINGS_DOT_COLOR } from "../../constants";
import { s, chevronFor } from "./styles";

export function RoleGroup({
  role,
  files,
  commenting,
  findings,
  collapsed,
  onToggle,
}: {
  role: FileRole;
  files: PrFile[];
  commenting?: DiffCommentApi;
  findings?: DiffFindingApi;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const t = useTranslations("smartDiff");

  // Count of *files* (not findings) in this group with at least one open
  // finding — placed before "N files" (AC-9). No counter at all when none
  // of the group's files have an open finding, even if some are triaged
  // (AC-10).
  const openFileCount = React.useMemo(() => {
    if (!findings) return 0;
    const openPaths = pathsWithOpenFindings(findings.findings);
    return files.filter((f) => openPaths.has(f.path)).length;
  }, [files, findings]);

  const counterLabel = t("openFindingsCounter", { count: openFileCount });

  return (
    <div style={s.group}>
      <button type="button" aria-expanded={!collapsed} onClick={onToggle} style={s.header}>
        <Icon.ChevronRight size={13} style={chevronFor(!collapsed)} />
        <span style={s.label}>{t(`roles.${role}.label`)}</span>
        <span style={s.description}>{t(`roles.${role}.description`)}</span>
        {openFileCount > 0 && (
          <span role="img" aria-label={counterLabel} title={counterLabel} style={s.counter}>
            <span style={{ ...s.counterDot, background: OPEN_FINDINGS_DOT_COLOR }} />
            {openFileCount}
          </span>
        )}
        <span style={s.filesCount}>{t("filesCount", { count: files.length })}</span>
      </button>
      {!collapsed && <DiffViewer files={files} commenting={commenting} findings={findings} />}
    </div>
  );
}
