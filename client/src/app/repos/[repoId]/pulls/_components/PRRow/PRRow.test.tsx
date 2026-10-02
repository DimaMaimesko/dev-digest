import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { PrMeta } from "@devdigest/shared";
import messages from "../../../../../../../messages/en/prReview.json";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

import { PRRow } from "./PRRow";

afterEach(cleanup);

const PR: PrMeta = {
  id: "p1",
  number: 482,
  title: "Add rate limiting",
  author: "ann",
  branch: "feat",
  base: "main",
  head_sha: "abc",
  additions: 10,
  deletions: 2,
  files_count: 3,
  status: "reviewed",
  updated_at: "2026-09-01T10:00:00.000Z",
  score: 85,
};

function renderRow(pr: Partial<PrMeta>) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ prReview: messages }}>
      <PRRow pr={{ ...PR, ...pr }} repoId="r1" />
    </NextIntlClientProvider>,
  );
}

describe("PRRow — cost", () => {
  it("shows what all the PR's runs cost, exact on hover", () => {
    renderRow({ cost_usd: 1.23456 });
    expect(screen.getByText("$1.23")).toHaveAttribute("title", "$1.23456");
  });

  it("shows — when no run's cost is known", () => {
    renderRow({ cost_usd: null, score: 85 });
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
