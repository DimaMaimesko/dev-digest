/* GenerationBanner — generation progress (AC-9), a failed generation's reason
   (AC-36) and a config_error with a Settings → API Keys link (AC-33). */
"use client";

import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";

type Kind = "generating" | "regenerating" | "failed" | "config_error";

export function GenerationBanner({ kind, message }: { kind: Kind; message?: string | null }) {
  const t = useTranslations("onboardingTour");
  const router = useRouter();

  if (kind === "generating" || kind === "regenerating") {
    return (
      <div style={rowStyle("var(--text-secondary)", "var(--bg-elevated)", "var(--border)")}>
        <Icon.RefreshCw size={14} style={{ animation: "ddspin 1s linear infinite" }} />
        <span>{t(kind === "generating" ? "generationBanner.generating" : "generationBanner.regenerating")}</span>
      </div>
    );
  }

  if (kind === "config_error") {
    return (
      <div style={rowStyle("var(--crit)", "var(--crit-bg)", "var(--crit)")}>
        <Icon.AlertOctagon size={14} />
        <span>
          {message ?? ""}{" "}
          <a
            href="/settings/api-keys"
            onClick={(e) => {
              e.preventDefault();
              router.push("/settings/api-keys");
            }}
            style={{ color: "inherit", textDecoration: "underline" }}
          >
            {t("generationBanner.settingsLink")}
          </a>
        </span>
      </div>
    );
  }

  return (
    <div style={rowStyle("var(--crit)", "var(--crit-bg)", "var(--crit)")}>
      <Icon.AlertOctagon size={14} />
      <span>
        {t("generationBanner.failed")}
        {message ? ` — ${message}` : ""}
      </span>
    </div>
  );
}

function rowStyle(color: string, bg: string, border: string) {
  return {
    display: "flex",
    alignItems: "center",
    gap: 8,
    fontSize: 13,
    color,
    background: bg,
    border: `1px solid ${border}`,
    borderRadius: 7,
    padding: "8px 12px",
  } as const;
}
