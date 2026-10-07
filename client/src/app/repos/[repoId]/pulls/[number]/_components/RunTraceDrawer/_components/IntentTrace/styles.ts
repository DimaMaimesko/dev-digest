import type { CSSProperties } from "react";

/** Co-located styles for IntentTrace. */
export const s = {
  wrap: { display: "flex", flexDirection: "column", gap: 10, fontSize: 13 } satisfies CSSProperties,
  none: { color: "var(--text-muted)" } satisfies CSSProperties,
  list: { display: "flex", flexDirection: "column", gap: 4 } satisfies CSSProperties,
  chip: { fontSize: 12, color: "var(--text-secondary)" } satisfies CSSProperties,
} as const;
