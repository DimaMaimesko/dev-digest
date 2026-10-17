import type { CSSProperties } from "react";

export const s = {
  layout: {
    display: "grid",
    gridTemplateColumns: "1fr 220px",
    gap: 24,
    alignItems: "start",
  } satisfies CSSProperties,
  main: {
    display: "flex",
    flexDirection: "column",
    gap: 16,
    minWidth: 0,
  } satisfies CSSProperties,
  side: {
    position: "sticky",
    top: 16,
  } satisfies CSSProperties,
  loadingStack: {
    display: "flex",
    flexDirection: "column",
    gap: 10,
  } satisfies CSSProperties,
} as const;
