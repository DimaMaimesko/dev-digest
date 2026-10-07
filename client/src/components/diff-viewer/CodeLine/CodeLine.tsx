/* CodeLine — one rendered diff line: gutter number, +/- sign, text, plus the
   hover "+" affordance, any anchored comment threads, and an inline composer. */
"use client";

import React from "react";
import type { ReactNode } from "react";
import type { Severity } from "@devdigest/ui";
import type { FindingRecord } from "@devdigest/shared";
import { commentTargetFor, type CommentThread, type DiffCommentApi, cs } from "../comments";
import { type Line } from "../helpers";
import { FINDING_SEV_COLOR, FINDING_SEV_COLOR_FALLBACK } from "../constants";
import { SEVERITY_LABEL_KEY } from "../findings";
import { s, lineRowFor, lineSignFor } from "../styles";
import { CommentThreadView } from "../CommentThreadView";
import { InlineComposer } from "../InlineComposer";
import { useTranslations } from "next-intl";

export function CodeLine({
  ln,
  path,
  threads,
  commenting,
  findingsHere,
  stripeSeverity,
  labelSeverity,
  renderFinding,
}: {
  ln: Line;
  path: string;
  threads: CommentThread[];
  commenting?: DiffCommentApi;
  /** Findings anchored to this line (open or triaged), stacked in severity order. */
  findingsHere?: FindingRecord[];
  /** Highest severity among open findings covering this line (left stripe). */
  stripeSeverity?: Severity;
  /** Highest severity among open findings anchored to this line (right label). */
  labelSeverity?: Severity;
  renderFinding?: (f: FindingRecord) => ReactNode;
}) {
  const t = useTranslations("shell");
  const [hover, setHover] = React.useState(false);
  const [composing, setComposing] = React.useState(false);

  if (ln.kind === "hunk") {
    return (
      <div className="mono" style={s.hunk}>
        {ln.text}
      </div>
    );
  }

  const sign = ln.kind === "add" ? "+" : ln.kind === "del" ? "−" : "";
  const target = commenting?.canComment ? commentTargetFor(ln) : null;
  const showAdd = hover && !!target && !composing;
  const stripeColor = stripeSeverity
    ? FINDING_SEV_COLOR[stripeSeverity] ?? FINDING_SEV_COLOR_FALLBACK
    : undefined;
  const labelKey = labelSeverity ? SEVERITY_LABEL_KEY[labelSeverity] : undefined;

  return (
    <div
      style={cs.rowWrap}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <div style={lineRowFor(ln.kind, stripeColor)}>
        <span className="mono tnum" style={{ ...s.lineNo, position: "relative" }}>
          {showAdd && target && (
            <button
              type="button"
              title="Add a comment on this line"
              aria-label="Add a comment on this line"
              onClick={() => setComposing(true)}
              style={cs.addBtn}
            >
              +
            </button>
          )}
          {ln.newNo ?? ln.oldNo ?? ""}
        </span>
        <span className="mono" style={lineSignFor(ln.kind)}>
          {sign}
        </span>
        <span className="mono" style={s.lineText}>
          {ln.text || " "}
        </span>
        {labelKey && (
          <span className="mono" style={{ ...s.severityLabel, color: stripeColor }}>
            {t(`diffViewer.${labelKey}`)}
          </span>
        )}
      </div>

      {commenting &&
        commenting.showComments &&
        threads.map((th) => (
          <CommentThreadView key={th.rootId} thread={th} commenting={commenting} path={path} />
        ))}

      {renderFinding && findingsHere && findingsHere.length > 0 && (
        <div style={s.findingsWrap}>
          {findingsHere.map((f) => (
            <React.Fragment key={f.id}>{renderFinding(f)}</React.Fragment>
          ))}
        </div>
      )}

      {commenting && composing && target && (
        <InlineComposer
          commenting={commenting}
          path={path}
          line={target.line}
          side={target.side}
          onClose={() => setComposing(false)}
        />
      )}
    </div>
  );
}
