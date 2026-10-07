import type { CSSProperties } from "react";

export const s = {
  group: {
    display: "flex",
    flexDirection: "column",
    gap: 8,
  } satisfies CSSProperties,
  header: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    width: "100%",
    padding: "8px 12px",
    border: "1px solid var(--border)",
    borderRadius: 7,
    background: "var(--bg-elevated)",
    cursor: "pointer",
    textAlign: "left",
  } satisfies CSSProperties,
  label: {
    fontSize: 13,
    fontWeight: 700,
    color: "var(--text-primary)",
    flexShrink: 0,
  } satisfies CSSProperties,
  description: {
    fontSize: 12,
    color: "var(--text-muted)",
    flex: 1,
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  } satisfies CSSProperties,
  counter: {
    display: "inline-flex",
    alignItems: "center",
    gap: 5,
    fontSize: 12,
    fontWeight: 600,
    color: "var(--text-secondary)",
    flexShrink: 0,
  } satisfies CSSProperties,
  counterDot: {
    display: "inline-block",
    width: 7,
    height: 7,
    borderRadius: "50%",
    flexShrink: 0,
  } satisfies CSSProperties,
  filesCount: {
    fontSize: 12,
    color: "var(--text-muted)",
    flexShrink: 0,
  } satisfies CSSProperties,
} as const;

export function chevronFor(expanded: boolean): CSSProperties {
  return {
    color: "var(--text-muted)",
    transform: expanded ? "rotate(90deg)" : "none",
    transition: "transform .12s",
    flexShrink: 0,
  };
}
