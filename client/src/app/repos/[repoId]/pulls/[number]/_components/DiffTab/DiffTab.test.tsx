import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { FindingRecord, PrFile, ReviewRecord } from "@devdigest/shared";

const findingActionMutate = vi.fn();

vi.mock("@/lib/hooks/reviews", () => ({
  usePrComments: () => ({ data: [] }),
  useCreatePrComment: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useFindingAction: () => ({ mutate: findingActionMutate, isPending: false }),
  useRunEvents: () => ({ events: [], running: false }),
}));

// RunStatus streams over SSE; a stub exposes its runIds and lets a test fire onDone.
vi.mock("../RunStatus", () => ({
  RunStatus: ({ runIds, onDone }: { runIds: string[]; onDone?: () => void }) => (
    <button data-testid="run-status" onClick={onDone}>
      {runIds.join(",")}
    </button>
  ),
}));

import { DiffTab } from "./DiffTab";
import smartDiff from "../../../../../../../../messages/en/smartDiff.json";
import shell from "../../../../../../../../messages/en/shell.json";
import prReview from "../../../../../../../../messages/en/prReview.json";

afterEach(cleanup);

function renderWithIntl(ui: React.ReactElement) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ smartDiff, shell, prReview }}>
      {ui}
    </NextIntlClientProvider>,
  );
}

function file(path: string, overrides: Partial<PrFile> = {}): PrFile {
  return { path, additions: 1, deletions: 0, patch: null, ...overrides };
}

function finding(overrides: Partial<FindingRecord>): FindingRecord {
  return {
    id: "f1",
    severity: "CRITICAL",
    category: "security",
    title: "A finding",
    file: "src/a.ts",
    start_line: 1,
    end_line: 1,
    rationale: "rationale",
    suggestion: null,
    confidence: 0.9,
    kind: "finding",
    trifecta_components: null,
    evidence: null,
    review_id: "r1",
    accepted_at: null,
    dismissed_at: null,
    ...overrides,
  };
}

function review(findings: FindingRecord[], overrides: Partial<ReviewRecord> = {}): ReviewRecord {
  return {
    id: "r1",
    pr_id: "pr1",
    agent_id: "agent-1",
    run_id: null,
    agent_name: "Agent One",
    kind: "review",
    verdict: null,
    summary: null,
    score: null,
    model: null,
    grounding: null,
    created_at: "2026-01-01T00:00:00Z",
    findings,
    ...overrides,
  };
}

const MIXED_FILES: PrFile[] = [
  file("src/server.ts"), // core
  file("src/utils.test.ts"), // tests
  file("vite.config.ts"), // wiring
  file("docs/guide.md"), // docs
  file("pnpm-lock.yaml"), // boilerplate
];

const BASE_PROPS = {
  prId: "pr1",
  canComment: false,
  liveRunIds: [] as string[],
  onRunDone: vi.fn(),
  order: "smart" as const,
  onSetOrder: vi.fn(),
  repoFullName: null,
  headSha: null,
};

afterEach(() => {
  findingActionMutate.mockClear();
});

describe("DiffTab — Smart order grouping", () => {
  it("renders only the groups with files, in role order, each collapsed per AC-7 defaults", () => {
    renderWithIntl(<DiffTab {...BASE_PROPS} files={MIXED_FILES} reviews={[]} />);

    expect(screen.getByText("Core logic")).toBeInTheDocument();
    expect(screen.getByText("Tests")).toBeInTheDocument();
    expect(screen.getByText("Wiring")).toBeInTheDocument();
    expect(screen.getByText("Docs")).toBeInTheDocument();
    expect(screen.getByText("Boilerplate")).toBeInTheDocument();

    // core/tests/wiring expanded by default -> their file paths render.
    expect(screen.getByText("src/server.ts")).toBeInTheDocument();
    expect(screen.getByText("src/utils.test.ts")).toBeInTheDocument();
    expect(screen.getByText("vite.config.ts")).toBeInTheDocument();
    // docs/boilerplate collapsed by default -> their file paths are hidden.
    expect(screen.queryByText("docs/guide.md")).not.toBeInTheDocument();
    expect(screen.queryByText("pnpm-lock.yaml")).not.toBeInTheDocument();
  });

  it("header click toggles only that group", () => {
    renderWithIntl(<DiffTab {...BASE_PROPS} files={MIXED_FILES} reviews={[]} />);

    fireEvent.click(screen.getByRole("button", { name: /Docs/ }));
    expect(screen.getByText("docs/guide.md")).toBeInTheDocument();
    // Boilerplate, untouched, stays collapsed.
    expect(screen.queryByText("pnpm-lock.yaml")).not.toBeInTheDocument();
    // Core, untouched, stays expanded.
    expect(screen.getByText("src/server.ts")).toBeInTheDocument();
  });

  it("hides groups/order switch and shows only the empty state when there are no files", () => {
    renderWithIntl(<DiffTab {...BASE_PROPS} files={[]} reviews={[]} />);
    expect(screen.getByText("No changed files.")).toBeInTheDocument();
    expect(screen.queryByText("Smart order")).not.toBeInTheDocument();
    expect(screen.queryByText("Original order")).not.toBeInTheDocument();
    expect(screen.queryByText("Core logic")).not.toBeInTheDocument();
  });
});

describe("DiffTab — open-findings counter", () => {
  const TWO_CORE_FILES: PrFile[] = [file("src/a.ts"), file("src/b.ts")];

  it("counts files with an open finding, not findings (two files, five findings -> 2)", () => {
    const findings = [
      finding({ id: "f1", file: "src/a.ts" }),
      finding({ id: "f2", file: "src/a.ts" }),
      finding({ id: "f3", file: "src/a.ts" }),
      finding({ id: "f4", file: "src/b.ts" }),
      finding({ id: "f5", file: "src/b.ts" }),
    ];
    renderWithIntl(
      <DiffTab {...BASE_PROPS} files={TWO_CORE_FILES} reviews={[review(findings)]} />,
    );
    expect(screen.getByLabelText("2 files with open findings")).toBeInTheDocument();
  });

  it("shows no counter when every shown finding is triaged", () => {
    const findings = [
      finding({ id: "f1", file: "src/a.ts", dismissed_at: "2026-01-02T00:00:00Z" }),
      finding({ id: "f2", file: "src/b.ts", accepted_at: "2026-01-02T00:00:00Z" }),
    ];
    renderWithIntl(
      <DiffTab {...BASE_PROPS} files={TWO_CORE_FILES} reviews={[review(findings)]} />,
    );
    expect(screen.queryByLabelText(/files with open findings/)).not.toBeInTheDocument();
  });

  it("re-rendering with the finding's last open one triaged drops the counter and the file dot", () => {
    const openFindings = [finding({ id: "f1", file: "src/a.ts" })];
    const { rerender } = renderWithIntl(
      <DiffTab {...BASE_PROPS} files={TWO_CORE_FILES} reviews={[review(openFindings)]} />,
    );
    expect(screen.getByLabelText("1 file with open findings")).toBeInTheDocument();
    expect(screen.getByLabelText("has open findings")).toBeInTheDocument();

    const triaged = [
      finding({ id: "f1", file: "src/a.ts", dismissed_at: "2026-01-02T00:00:00Z" }),
    ];
    rerender(
      <NextIntlClientProvider locale="en" messages={{ smartDiff, shell, prReview }}>
        <DiffTab {...BASE_PROPS} files={TWO_CORE_FILES} reviews={[review(triaged)]} />
      </NextIntlClientProvider>,
    );
    expect(screen.queryByLabelText(/files? with open findings/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("has open findings")).not.toBeInTheDocument();
  });
});

describe("DiffTab — order switch", () => {
  it("renders a flat list with no group headers in Original order", () => {
    renderWithIntl(
      <DiffTab {...BASE_PROPS} order="original" files={MIXED_FILES} reviews={[]} />,
    );
    expect(screen.queryByText("Core logic")).not.toBeInTheDocument();
    expect(screen.getByText("src/server.ts")).toBeInTheDocument();
    expect(screen.getByText("docs/guide.md")).toBeInTheDocument();
  });

  it("calls onSetOrder with the clicked option, and exposes aria-pressed", () => {
    const onSetOrder = vi.fn();
    renderWithIntl(
      <DiffTab {...BASE_PROPS} onSetOrder={onSetOrder} files={MIXED_FILES} reviews={[]} />,
    );
    const smartBtn = screen.getByRole("button", { name: "Smart order" });
    const originalBtn = screen.getByRole("button", { name: "Original order" });
    expect(smartBtn).toHaveAttribute("aria-pressed", "true");
    expect(originalBtn).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(originalBtn);
    expect(onSetOrder).toHaveBeenCalledWith("original");
  });
});

describe("DiffTab — live run", () => {
  it("renders RunStatus progress while a run is live", () => {
    renderWithIntl(
      <DiffTab {...BASE_PROPS} liveRunIds={["run-1"]} files={MIXED_FILES} reviews={[]} />,
    );
    expect(screen.getByTestId("run-status")).toHaveTextContent("run-1");
    expect(screen.getByText("Core logic")).toBeInTheDocument();
  });

  it("shows a live run and refetches when it settles even with no changed files", () => {
    const onRunDone = vi.fn();
    renderWithIntl(
      <DiffTab
        {...BASE_PROPS}
        liveRunIds={["run-1"]}
        onRunDone={onRunDone}
        files={[]}
        reviews={[]}
      />,
    );
    expect(screen.getByText("No changed files.")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("run-status"));
    expect(onRunDone).toHaveBeenCalledTimes(1);
  });
});

describe("DiffTab — header total", () => {
  it("shows the listed count when it matches the PR's file count", () => {
    renderWithIntl(
      <DiffTab {...BASE_PROPS} files={MIXED_FILES} totalFiles={MIXED_FILES.length} reviews={[]} />,
    );
    expect(screen.getByText(new RegExp(`Files changed · ${MIXED_FILES.length} files`))).toBeInTheDocument();
  });

  it("shows 'N of M' when the API listed fewer files than the PR has", () => {
    renderWithIntl(
      <DiffTab {...BASE_PROPS} files={MIXED_FILES} totalFiles={150} reviews={[]} />,
    );
    expect(
      screen.getByText(new RegExp(`Files changed · ${MIXED_FILES.length} of 150 files`)),
    ).toBeInTheDocument();
  });
});
