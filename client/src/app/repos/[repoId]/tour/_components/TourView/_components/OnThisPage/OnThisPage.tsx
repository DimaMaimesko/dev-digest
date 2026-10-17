/* OnThisPage — AC-38: anchor links to each of the 5 fixed sections, with the
   currently visible one highlighted via IntersectionObserver. jsdom (tests)
   has no IntersectionObserver, so observing is skipped there rather than
   crashing — the links still render and still work. */
"use client";

import React from "react";
import { useTranslations } from "next-intl";
import type { TourSectionKind } from "@/lib/types";
import { SECTION_ORDER } from "../../constants";

export function OnThisPage({ kinds }: { kinds: TourSectionKind[] }) {
  const t = useTranslations("onboardingTour");
  const [active, setActive] = React.useState<TourSectionKind | null>(null);
  const order = SECTION_ORDER.filter((k) => kinds.includes(k));

  React.useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const els = order
      .map((k) => document.getElementById(`tour-section-${k}`))
      .filter((el): el is HTMLElement => el != null);
    if (els.length === 0) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting);
        if (visible.length === 0) return;
        const topMost = visible.sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        const kind = topMost?.target.getAttribute("data-section-kind") as TourSectionKind | null;
        if (kind) setActive(kind);
      },
      { rootMargin: "-10% 0px -70% 0px" },
    );
    els.forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, [order]);

  return (
    <nav aria-label={t("onThisPage")} style={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <div
        style={{
          fontSize: 11,
          fontWeight: 700,
          letterSpacing: "0.05em",
          textTransform: "uppercase",
          color: "var(--text-muted)",
          marginBottom: 6,
        }}
      >
        {t("onThisPage")}
      </div>
      {order.map((kind) => (
        <a
          key={kind}
          href={`#tour-section-${kind}`}
          onClick={(e) => {
            e.preventDefault();
            document.getElementById(`tour-section-${kind}`)?.scrollIntoView({ behavior: "smooth" });
          }}
          style={{
            fontSize: 13,
            padding: "5px 0",
            color: active === kind ? "var(--text-primary)" : "var(--text-secondary)",
            fontWeight: active === kind ? 600 : 400,
            textDecoration: "none",
          }}
        >
          {t(`sections.${kind}`)}
        </a>
      ))}
    </nav>
  );
}
