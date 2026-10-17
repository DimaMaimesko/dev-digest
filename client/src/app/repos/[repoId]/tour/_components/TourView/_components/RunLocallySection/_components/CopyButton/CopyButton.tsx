/* CopyButton — AC-26: copies the exact command via navigator.clipboard,
   showing "Copied" / "Copy failed". The accessible name stays the fixed
   "Copy command: {command}" regardless of state (e2e flow 09 looks it up by
   that exact name without clicking it). */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";

type Status = "idle" | "copied" | "failed";

export function CopyButton({ command }: { command: string }) {
  const t = useTranslations("onboardingTour");
  const [status, setStatus] = React.useState<Status>("idle");

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setStatus("copied");
    } catch {
      setStatus("failed");
    } finally {
      setTimeout(() => setStatus("idle"), 1500);
    }
  };

  return (
    <button
      type="button"
      aria-label={t("copyCommand", { command })}
      onClick={onCopy}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "4px 9px",
        borderRadius: 6,
        border: "1px solid var(--border-strong)",
        background: "var(--bg-elevated)",
        color: "var(--text-secondary)",
        fontSize: 12,
        cursor: "pointer",
      }}
    >
      <Icon.Copy size={12} />
      {status === "copied" ? t("copied") : status === "failed" ? t("copyFailed") : t("copy")}
    </button>
  );
}
