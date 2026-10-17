import type { TourSectionKind } from "@/lib/types";
import type { IconName } from "@devdigest/ui";

/** Fixed section order (Approach → Domain "Kinds, in this order"). Used for
    rendering, the generation's section list and "On this page". */
export const SECTION_ORDER: TourSectionKind[] = [
  "architecture",
  "critical_paths",
  "run_locally",
  "reading_path",
  "first_tasks",
];

export const SECTION_ICON: Record<TourSectionKind, IconName> = {
  architecture: "Boxes",
  critical_paths: "Target",
  run_locally: "Play",
  reading_path: "FileText",
  first_tasks: "ListChecks",
};
