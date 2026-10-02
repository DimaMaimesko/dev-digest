import type { CSSProperties } from "react";

/** Co-located styles for the skill ConfigTab. */
export const s = {
  wrap: { maxWidth: 860 } satisfies CSSProperties,
  header: { display: "flex", alignItems: "center", gap: 10, marginBottom: 18 } satisfies CSSProperties,
  h2: { fontSize: 16, fontWeight: 700 } satisfies CSSProperties,
  enabledLabel: {
    marginLeft: "auto",
    display: "flex",
    alignItems: "center",
    gap: 10,
    fontSize: 13,
    color: "var(--text-secondary)",
  } satisfies CSSProperties,
  bodyHead: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    padding: "8px 12px",
    border: "1px solid var(--border)",
    borderBottom: "none",
    borderRadius: "7px 7px 0 0",
    background: "var(--bg-surface)",
    fontSize: 13,
  } satisfies CSSProperties,
  tokens: { marginLeft: "auto", fontSize: 12, color: "var(--text-muted)" } satisfies CSSProperties,
  agents: { display: "flex", flexWrap: "wrap", gap: 8 } satisfies CSSProperties,
  agentLink: { textDecoration: "none" } satisfies CSSProperties,
  none: { fontSize: 13, color: "var(--text-muted)" } satisfies CSSProperties,
  actions: { display: "flex", alignItems: "center", gap: 12, marginTop: 8 } satisfies CSSProperties,
} as const;
