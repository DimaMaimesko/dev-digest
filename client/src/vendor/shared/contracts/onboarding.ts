import { z } from 'zod';

/**
 * Onboarding Tour: the generated per-repo tour (architecture, critical paths,
 * run-locally, reading path, first tasks), its generation runs and the repo's
 * index readiness.
 *
 * Unrelated to the old generic `Onboarding` block in `./knowledge.ts`, which
 * stays as is for later lessons.
 */

// ---- Enums ----
export const TourSectionKind = z.enum([
  'architecture',
  'critical_paths',
  'run_locally',
  'reading_path',
  'first_tasks',
]);
export type TourSectionKind = z.infer<typeof TourSectionKind>;

export const TourSectionStatus = z.enum(['pending', 'generating', 'done', 'failed']);
export type TourSectionStatus = z.infer<typeof TourSectionStatus>;

export const TourDiagramNodeKind = z.enum([
  'entry',
  'module',
  'middleware',
  'datastore',
  'external',
]);
export type TourDiagramNodeKind = z.infer<typeof TourDiagramNodeKind>;

// ---- Grounded path references ----
export const TourPathRef = z.object({
  path: z.string(),
  is_dir: z.boolean(),
});
export type TourPathRef = z.infer<typeof TourPathRef>;

// ---- Architecture content ----
export const TourSummarySegment = z.discriminatedUnion('type', [
  z.object({ type: z.literal('text'), text: z.string() }),
  TourPathRef.extend({ type: z.literal('path') }),
]);
export type TourSummarySegment = z.infer<typeof TourSummarySegment>;

export const TourDiagram = z.object({
  nodes: z.array(
    z.object({
      id: z.string(),
      label: z.string(),
      kind: TourDiagramNodeKind,
      path: z.string().nullable(),
      is_dir: z.boolean(),
    }),
  ),
  edges: z.array(
    z.object({
      from: z.string(),
      to: z.string(),
      label: z.string().nullable(),
    }),
  ),
});
export type TourDiagram = z.infer<typeof TourDiagram>;

export const ArchitectureContent = z.object({
  summary: z.array(TourSummarySegment),
  diagram: TourDiagram.nullable(),
});
export type ArchitectureContent = z.infer<typeof ArchitectureContent>;

// ---- Path-list content (critical paths, reading path) ----
export const PathListContent = z.object({
  items: z.array(TourPathRef.extend({ reason: z.string() })),
});
export type PathListContent = z.infer<typeof PathListContent>;

// ---- Run-locally content ----
export const RunLocallyContent = z.object({
  steps: z.array(
    z.object({
      command: z.string(),
      comment: z.string().nullable(),
    }),
  ),
});
export type RunLocallyContent = z.infer<typeof RunLocallyContent>;

// ---- First-tasks content ----
export const FirstTasksContent = z.object({
  tasks: z.array(
    z.object({
      title: z.string(),
      description: z.string(),
      size: z.enum(['small', 'medium']),
      paths: z.array(TourPathRef),
      todo: z
        .object({
          path: z.string(),
          line: z.number().int(),
          text: z.string(),
        })
        .nullable(),
    }),
  ),
});
export type FirstTasksContent = z.infer<typeof FirstTasksContent>;

// ---- Section, discriminated by kind ----
export const TourSection = z.discriminatedUnion('kind', [
  z.object({
    kind: z.literal('architecture'),
    status: z.enum(['done', 'failed']),
    reason: z.string().nullable(),
    removed_refs: z.number().int(),
    content: ArchitectureContent.nullable(),
  }),
  z.object({
    kind: z.literal('critical_paths'),
    status: z.enum(['done', 'failed']),
    reason: z.string().nullable(),
    removed_refs: z.number().int(),
    content: PathListContent.nullable(),
  }),
  z.object({
    kind: z.literal('run_locally'),
    status: z.enum(['done', 'failed']),
    reason: z.string().nullable(),
    removed_refs: z.number().int(),
    content: RunLocallyContent.nullable(),
  }),
  z.object({
    kind: z.literal('reading_path'),
    status: z.enum(['done', 'failed']),
    reason: z.string().nullable(),
    removed_refs: z.number().int(),
    content: PathListContent.nullable(),
  }),
  z.object({
    kind: z.literal('first_tasks'),
    status: z.enum(['done', 'failed']),
    reason: z.string().nullable(),
    removed_refs: z.number().int(),
    content: FirstTasksContent.nullable(),
  }),
]);
export type TourSection = z.infer<typeof TourSection>;

// ---- The stored tour ----
export const OnboardingTour = z.object({
  repo_id: z.string(),
  commit_sha: z.string(),
  files_indexed: z.number().int(),
  index_status: z.enum(['full', 'partial']),
  code_index_empty: z.boolean(),
  generated_at: z.string(),
  provider: z.string(),
  model: z.string(),
  cost_usd: z.number().nullable(),
  sections: z.array(TourSection),
});
export type OnboardingTour = z.infer<typeof OnboardingTour>;

// ---- A generation (run) ----
export const OnboardingGeneration = z.object({
  id: z.string(),
  repo_id: z.string(),
  status: z.enum(['queued', 'running', 'done', 'failed']),
  error: z.string().nullable(),
  commit_sha: z.string(),
  created_at: z.string(),
  finished_at: z.string().nullable(),
  sections: z.array(
    z.object({
      kind: TourSectionKind,
      status: TourSectionStatus,
      reason: z.string().nullable(),
    }),
  ),
});
export type OnboardingGeneration = z.infer<typeof OnboardingGeneration>;

// ---- Repo index readiness ----
export const RepoIndexReadiness = z.object({
  state: z.enum(['ready', 'indexing', 'not_indexed']),
  commit_sha: z.string().nullable(),
  files_indexed: z.number().int(),
  partial: z.boolean(),
  code_index_empty: z.boolean(),
});
export type RepoIndexReadiness = z.infer<typeof RepoIndexReadiness>;

// ---- GET /repos/{id}/onboarding response envelope ----
export const OnboardingTourResponse = z.object({
  index: RepoIndexReadiness,
  tour: OnboardingTour.nullable(),
  generation: OnboardingGeneration.nullable(),
});
export type OnboardingTourResponse = z.infer<typeof OnboardingTourResponse>;
