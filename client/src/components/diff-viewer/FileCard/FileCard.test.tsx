import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { FindingRecord } from "@devdigest/shared";
import type { PrFile, PrReviewComment } from "@/lib/types";
import type { DiffCommentApi } from "../comments";
import type { DiffFindingApi } from "../findings";
import shellMessages from "../../../../messages/en/shell.json";
import { FileCard } from "./FileCard";

afterEach(cleanup);

const FILE: PrFile = {
  path: "src/config.ts",
  additions: 2,
  deletions: 1,
  patch: "@@ -1,2 +1,3 @@\n const a = 1;\n-const b = 2;\n+const b = 3;\n+const c = 4;",
};

const NO_PATCH_FILE: PrFile = {
  path: "src/no-patch.ts",
  additions: 0,
  deletions: 0,
  patch: null,
};

function baseFinding(overrides: Partial<FindingRecord>): FindingRecord {
  return {
    id: "f1",
    severity: "CRITICAL",
    category: "security",
    title: "A finding",
    file: "src/config.ts",
    start_line: 2,
    end_line: 2,
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

function renderFinding(f: FindingRecord) {
  return <div data-testid={`finding-${f.id}`}>{f.title}</div>;
}

function renderWithIntl(ui: React.ReactElement) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ shell: shellMessages }}>
      {ui}
    </NextIntlClientProvider>,
  );
}

const COMMENTS: PrReviewComment[] = [
  {
    id: 1,
    path: "src/config.ts",
    line: 2,
    original_line: 2,
    side: "RIGHT",
    body: "A GitHub review comment",
    user: "reviewer",
    created_at: "2026-01-01T00:00:00Z",
    html_url: "https://github.com/x",
    in_reply_to_id: null,
    is_outdated: false,
  },
];

describe("FileCard findings", () => {
  it("shows the open-findings dot only when the file has an open finding", () => {
    const findings: DiffFindingApi = {
      findings: [baseFinding({})],
      renderFinding,
    };
    renderWithIntl(<FileCard file={FILE} findings={findings} />);
    expect(screen.getByLabelText("has open findings")).toBeInTheDocument();
  });

  it("shows no dot when the file's shown findings are all triaged", () => {
    const findings: DiffFindingApi = {
      findings: [baseFinding({ dismissed_at: "2026-01-01T00:00:00Z" })],
      renderFinding,
    };
    renderWithIntl(<FileCard file={FILE} findings={findings} />);
    expect(screen.queryByLabelText("has open findings")).not.toBeInTheDocument();
  });

  it("leaves the GitHub comment counter unaffected by findings", () => {
    const commenting: DiffCommentApi = {
      comments: COMMENTS,
      canComment: true,
      showComments: true,
      posting: false,
      onSubmit: async () => {},
    };
    const findings: DiffFindingApi = {
      findings: [baseFinding({})],
      renderFinding,
    };
    const { container } = renderWithIntl(
      <FileCard file={FILE} commenting={commenting} findings={findings} />,
    );
    const counter = container.querySelector(".lucide-message-square")?.parentElement;
    expect(counter?.textContent).toContain("1");
    expect(screen.getByLabelText("has open findings")).toBeInTheDocument();
  });

  it("renders finding cards under the line even with comments hidden (AC-24)", () => {
    const commenting: DiffCommentApi = {
      comments: [],
      canComment: true,
      showComments: false,
      posting: false,
      onSubmit: async () => {},
    };
    const findings: DiffFindingApi = {
      findings: [baseFinding({ title: "Hardcoded secret" })],
      renderFinding,
    };
    renderWithIntl(<FileCard file={FILE} commenting={commenting} findings={findings} />);
    expect(screen.getByText("Hardcoded secret")).toBeInTheDocument();
  });

  it("lists a finding with no rendered line under Not in the diff", () => {
    const findings: DiffFindingApi = {
      findings: [baseFinding({ file: "src/no-patch.ts", title: "No patch finding" })],
      renderFinding,
    };
    renderWithIntl(<FileCard file={NO_PATCH_FILE} findings={findings} />);
    expect(screen.getByText("Not in the diff")).toBeInTheDocument();
    expect(screen.getByText("No patch finding")).toBeInTheDocument();
  });
});
