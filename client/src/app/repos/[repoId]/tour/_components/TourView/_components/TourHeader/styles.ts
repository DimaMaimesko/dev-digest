import type { CSSProperties } from "react";

export const s = {
  header: {
    display: "flex",
    alignItems: "flex-start",
    justifyContent: "space-between",
    gap: 16,
  } satisfies CSSProperties,
  title: {
    fontSize: 20,
    fontWeight: 700,
    letterSpacing: "-0.01em",
    color: "var(--text-primary)",
    margin: 0,
  } satisfies CSSProperties,
  subtitle: {
    fontSize: 13.5,
    color: "var(--text-secondary)",
    marginTop: 6,
  } satisfies CSSProperties,
  badgeRow: {
    display: "flex",
    flexWrap: "wrap",
    gap: 8,
    marginTop: 10,
  } satisfies CSSProperties,
  note: {
    marginTop: 10,
    fontSize: 13,
    color: "var(--text-secondary)",
    background: "var(--bg-elevated)",
    border: "1px solid var(--border)",
    borderRadius: 7,
    padding: "7px 11px",
  } satisfies CSSProperties,
} as const;
