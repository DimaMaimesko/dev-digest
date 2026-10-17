/* OpenLink — AC-30: "Open" for a grounded TourPathRef, to the GitHub blob
   (file) or tree (directory) at the tour's commit sha. The only href the
   tour page ever builds from model output, and only over a path that already
   passed server-side grounding. */
"use client";

import { useTranslations } from "next-intl";
import { Icon } from "@devdigest/ui";
import { githubPathUrl } from "@/lib/github-urls";

export function OpenLink({
  fullName,
  sha,
  path,
  isDir,
}: {
  fullName: string | null | undefined;
  sha: string;
  path: string;
  isDir: boolean;
}) {
  const t = useTranslations("onboardingTour");
  if (!fullName) return null;
  return (
    <a
      href={githubPathUrl(fullName, sha, path, isDir)}
      target="_blank"
      rel="noopener noreferrer"
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        fontSize: 12,
        color: "var(--accent-text)",
        textDecoration: "none",
      }}
    >
      <Icon.ExternalLink size={11} />
      {t("open")}
    </a>
  );
}
