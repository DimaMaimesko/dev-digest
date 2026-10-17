/* FirstTasksSection — small/medium starter tasks, each with its grounded
   paths (Open links) and, when matched against a scanned TODO, the TODO's
   own text (never the model's words, AC-41). */
"use client";

import { useTranslations } from "next-intl";
import { Badge } from "@devdigest/ui";
import type { FirstTasksContent } from "@/lib/types";
import { OpenLink } from "../OpenLink";

export function FirstTasksSection({
  content,
  fullName,
  sha,
}: {
  content: FirstTasksContent;
  fullName: string | null | undefined;
  sha: string;
}) {
  const t = useTranslations("onboardingTour");

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {content.tasks.map((task, i) => (
        <div
          key={i}
          style={{
            border: "1px solid var(--border)",
            borderRadius: 8,
            padding: 12,
            display: "flex",
            flexDirection: "column",
            gap: 6,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontWeight: 600, fontSize: 13.5 }}>{task.title}</span>
            <Badge mono>{task.size}</Badge>
          </div>
          <div style={{ fontSize: 13, color: "var(--text-secondary)", lineHeight: 1.55 }}>{task.description}</div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {task.paths.map((p, j) => (
              <span key={j} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                <code className="mono" style={{ fontSize: 12, color: "var(--text-primary)" }}>
                  {p.path}
                </code>
                <OpenLink fullName={fullName} sha={sha} path={p.path} isDir={p.is_dir} />
              </span>
            ))}
          </div>
          {task.todo && (
            <div style={{ fontSize: 12.5, color: "var(--text-muted)" }}>
              {t("todoFrom", { path: task.todo.path, line: task.todo.line })} — {task.todo.text}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
