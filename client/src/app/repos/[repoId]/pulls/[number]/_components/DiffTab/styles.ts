import type { CSSProperties } from "react";

export const s = {
  headerActions: {
    display: "flex",
    alignItems: "center",
    gap: 10,
  } satisfies CSSProperties,
  orderSwitch: {
    display: "flex",
    gap: 4,
  } satisfies CSSProperties,
  liveRun: {
    marginBottom: 18,
  } satisfies CSSProperties,
  groups: {
    display: "flex",
    flexDirection: "column",
    gap: 14,
  } satisfies CSSProperties,
} as const;
