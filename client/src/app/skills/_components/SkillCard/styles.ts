import type { CSSProperties } from "react";

/** Co-located styles for SkillCard. */
export const s = {
  card: (active: boolean, enabled: boolean): CSSProperties => ({
    padding: 14,
    borderRadius: 8,
    cursor: "pointer",
    border: "1px solid " + (active ? "var(--border-strong)" : "var(--border)"),
    background: active ? "var(--bg-hover)" : "var(--bg-elevated)",
    opacity: enabled ? 1 : 0.6,
    marginBottom: 10,
  }),
  headerRow: { display: "flex", alignItems: "center", gap: 10 } satisfies CSSProperties,
  iconBox: (color: string, bg: string): CSSProperties => ({
    width: 26,
    height: 26,
    borderRadius: 7,
    background: bg,
    color,
    display: "grid",
    placeItems: "center",
    flexShrink: 0,
  }),
  name: {
    fontSize: 14,
    fontWeight: 600,
    flex: 1,
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
  } satisfies CSSProperties,
  description: {
    fontSize: 13,
    color: "var(--text-muted)",
    margin: "8px 0",
    lineHeight: 1.4,
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
  } satisfies CSSProperties,
  metaRow: { display: "flex", alignItems: "center", gap: 8 } satisfies CSSProperties,
  source: { fontSize: 12, color: "var(--text-muted)" } satisfies CSSProperties,
  agents: { fontSize: 12, color: "var(--text-muted)", marginLeft: "auto" } satisfies CSSProperties,
  deleteBtn: (busy: boolean): CSSProperties => ({
    background: "none",
    border: "none",
    cursor: busy ? "not-allowed" : "pointer",
    color: "var(--text-muted)",
    display: "inline-flex",
    padding: 4,
  }),
} as const;
