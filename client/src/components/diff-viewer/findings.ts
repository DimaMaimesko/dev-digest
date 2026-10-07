/* Findings support for the DiffViewer (Files changed tab). Pure helpers + the
   API shape the viewer needs to render findings where the code is; rendering
   itself is a caller-supplied callback (see client/CLAUDE.md "Map": this
   folder must not import from src/app/**).

   `FindingRecord` is imported as a type only — a runtime import from
   @devdigest/shared breaks the build (client/INSIGHTS.md 2026-09-30). Any
   runtime value (severity order, colors) is mirrored locally. */
import type { Severity } from "@devdigest/ui";
import type { FindingRecord } from "@devdigest/shared";
import type { Line } from "./helpers";

/** What the viewer needs to read findings + render a finding card. */
export interface DiffFindingApi {
  findings: FindingRecord[];
  renderFinding: (f: FindingRecord) => import("react").ReactNode;
}

/** A finding is open when it has been neither accepted nor dismissed. */
export function isOpen(f: FindingRecord): boolean {
  return !f.accepted_at && !f.dismissed_at;
}

/** Paths with at least one open finding. */
export function pathsWithOpenFindings(findings: FindingRecord[]): Set<string> {
  const out = new Set<string>();
  for (const f of findings) if (isOpen(f)) out.add(f.file);
  return out;
}

/** Sort weight per severity (lower = shown first). Mirrors
    FindingsPanel/constants.ts SEVERITY_ORDER — do not import across folders. */
const SEVERITY_ORDER: Record<string, number> = {
  CRITICAL: 0,
  WARNING: 1,
  SUGGESTION: 2,
  INFO: 3,
};

function severityRank(severity: string): number {
  return SEVERITY_ORDER[severity] ?? SEVERITY_ORDER.INFO!;
}

/** Highest severity of a and b (lower rank wins). */
function higherSeverity(a: Severity, b: Severity): Severity {
  return severityRank(a) <= severityRank(b) ? a : b;
}

/** CRITICAL → "blocker", WARNING → "warning", SUGGESTION → "suggestion".
    INFO gets no label — the spec defines none. Values are `shell.diffViewer`
    message keys, not literal copy. */
export const SEVERITY_LABEL_KEY: Partial<Record<Severity, string>> = {
  CRITICAL: "severityBlocker",
  WARNING: "severityWarning",
  SUGGESTION: "severitySuggestion",
};

export interface PlacedFindings {
  /** Findings anchored to a given index into `parsePatch` output, stacked in
      severity order (CRITICAL, WARNING, SUGGESTION), open or triaged alike. */
  byLine: Map<number, FindingRecord[]>;
  /** Highest severity among the *open* findings whose range covers this line. */
  stripe: Map<number, Severity>;
  /** Highest severity among the *open* findings anchored to this line. */
  label: Map<number, Severity>;
  /** Findings with no rendered line in their range (AC-20). */
  notInDiff: FindingRecord[];
}

/**
 * Place findings against parsed patch lines. Map keys are indexes into the
 * `lines` array (i.e. into `parsePatch` output).
 *
 * Only `add`/`ctx` lines with a `newNo` are candidates (AC-18 — a deleted
 * line never anchors). The anchor is the candidate whose `newNo === end_line`,
 * or else the last candidate with `newNo` in `[start_line, end_line]`, or else
 * the finding goes to `notInDiff` (AC-14, AC-20).
 */
export function placeFindings(lines: Line[], findings: FindingRecord[]): PlacedFindings {
  const byLine = new Map<number, FindingRecord[]>();
  const stripe = new Map<number, Severity>();
  const label = new Map<number, Severity>();
  const notInDiff: FindingRecord[] = [];

  const candidates: { index: number; newNo: number }[] = [];
  lines.forEach((ln, index) => {
    if ((ln.kind === "add" || ln.kind === "ctx") && ln.newNo != null) {
      candidates.push({ index, newNo: ln.newNo });
    }
  });

  for (const f of findings) {
    const exact = candidates.find((c) => c.newNo === f.end_line);
    let anchor: number | null = exact ? exact.index : null;
    if (anchor == null) {
      const inRange = candidates.filter(
        (c) => c.newNo >= f.start_line && c.newNo <= f.end_line,
      );
      if (inRange.length > 0) anchor = inRange[inRange.length - 1]!.index;
    }
    if (anchor == null) {
      notInDiff.push(f);
      continue;
    }

    const list = byLine.get(anchor) ?? [];
    list.push(f);
    byLine.set(anchor, list);

    if (isOpen(f)) {
      const sev = f.severity as Severity;
      for (const c of candidates) {
        if (c.newNo >= f.start_line && c.newNo <= f.end_line) {
          const existing = stripe.get(c.index);
          stripe.set(c.index, existing ? higherSeverity(existing, sev) : sev);
        }
      }
      const existingLabel = label.get(anchor);
      label.set(anchor, existingLabel ? higherSeverity(existingLabel, sev) : sev);
    }
  }

  for (const list of byLine.values()) {
    list.sort((a, b) => severityRank(a.severity) - severityRank(b.severity));
  }

  return { byLine, stripe, label, notInDiff };
}
