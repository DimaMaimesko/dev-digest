/* PathListSection — shared renderer for critical_paths and reading_path:
   each item is a grounded path plus the model's reason, with an Open link. */
"use client";

import type { PathListContent } from "@/lib/types";
import { OpenLink } from "../OpenLink";

export function PathListSection({
  content,
  fullName,
  sha,
}: {
  content: PathListContent;
  fullName: string | null | undefined;
  sha: string;
}) {
  return (
    <ul style={{ margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 10 }}>
      {content.items.map((item, i) => (
        <li
          key={`${item.path}-${i}`}
          style={{
            listStyle: "none",
            display: "flex",
            alignItems: "flex-start",
            gap: 10,
            borderBottom: i === content.items.length - 1 ? "none" : "1px solid var(--border)",
            paddingBottom: 10,
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <code className="mono" style={{ fontSize: 12.5, color: "var(--text-primary)" }}>
              {item.path}
            </code>
            <div style={{ fontSize: 13, color: "var(--text-secondary)", marginTop: 3 }}>{item.reason}</div>
          </div>
          <OpenLink fullName={fullName} sha={sha} path={item.path} isDir={item.is_dir} />
        </li>
      ))}
    </ul>
  );
}
