/** Fixed color for the group open-findings counter dot. Mirrors
    `src/components/diff-viewer/constants.ts` OPEN_FINDINGS_DOT_COLOR — not
    imported across folders since that file's barrel doesn't export it, and
    the value is not severity-colored (spec q7). */
export const OPEN_FINDINGS_DOT_COLOR = "var(--crit)";
