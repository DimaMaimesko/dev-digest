import type { CSSProperties } from "react";

/** Co-located styles for the skill VersionsTab. */
export const s = {
  wrap: { maxWidth: 860 } satisfies CSSProperties,
  titleRow: { display: "flex", alignItems: "center", gap: 10 } satisfies CSSProperties,
  h2: { fontSize: 16, fontWeight: 700 } satisfies CSSProperties,
  subtitle: { fontSize: 13, color: "var(--text-secondary)", margin: "4px 0 16px" } satisfies CSSProperties,
  list: { display: "flex", flexDirection: "column", gap: 10 } satisfies CSSProperties,
  row: {
    padding: "12px 16px",
    borderRadius: 8,
    border: "1px solid var(--border)",
    background: "var(--bg-elevated)",
  } satisfies CSSProperties,
  rowHead: { display: "flex", alignItems: "center", gap: 14 } satisfies CSSProperties,
  text: { flex: 1, minWidth: 0 } satisfies CSSProperties,
  message: (muted: boolean): CSSProperties => ({
    fontSize: 14,
    fontWeight: 600,
    color: muted ? "var(--text-muted)" : "var(--text-primary)",
  }),
  date: { fontSize: 12, color: "var(--text-muted)", marginTop: 2 } satisfies CSSProperties,
  body: {
    margin: "12px 0 0",
    padding: 12,
    borderRadius: 6,
    background: "var(--code-bg)",
    fontSize: 12,
    whiteSpace: "pre-wrap",
    overflowX: "auto",
  } satisfies CSSProperties,
} as const;
