import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { OnboardingTour } from "@/lib/types";
import messages from "../../../../../../../../../messages/en/onboardingTour.json";

import { TourHeader } from "./TourHeader";

afterEach(cleanup);

function renderHeader(tour: OnboardingTour) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ onboardingTour: messages }}>
      <TourHeader
        fullName="acme/payments-api"
        tour={tour}
        disabled={false}
        pending={false}
        onRegenerate={vi.fn()}
      />
    </NextIntlClientProvider>,
  );
}

const BASE_TOUR: OnboardingTour = {
  repo_id: "r1",
  commit_sha: "a1b2c3d4e5f6",
  files_indexed: 42,
  index_status: "full",
  code_index_empty: false,
  generated_at: "2026-10-01T10:00:00.000Z",
  provider: "openrouter",
  model: "deepseek/deepseek-v4-flash",
  cost_usd: 0.0042,
  sections: [],
};

describe("TourHeader", () => {
  it("shows the partial-index note when the index is partial and not empty (AC-31)", () => {
    renderHeader({ ...BASE_TOUR, index_status: "partial", code_index_empty: false });
    expect(screen.getByText("Index is partial — some files were skipped.")).toBeInTheDocument();
    expect(
      screen.queryByText("Code index is empty — this tour uses the file tree, docs and manifests only."),
    ).not.toBeInTheDocument();
  });

  it("shows the empty-index note when the code index is empty (AC-43)", () => {
    renderHeader({ ...BASE_TOUR, index_status: "partial", code_index_empty: true });
    expect(
      screen.getByText("Code index is empty — this tour uses the file tree, docs and manifests only."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Index is partial — some files were skipped.")).not.toBeInTheDocument();
  });

  it("shows the empty-index note over the partial note even when the index is full", () => {
    renderHeader({ ...BASE_TOUR, index_status: "full", code_index_empty: true });
    expect(
      screen.getByText("Code index is empty — this tour uses the file tree, docs and manifests only."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Index is partial — some files were skipped.")).not.toBeInTheDocument();
  });

  it("shows neither note for a full, non-empty index", () => {
    renderHeader({ ...BASE_TOUR, index_status: "full", code_index_empty: false });
    expect(screen.queryByText("Index is partial — some files were skipped.")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Code index is empty — this tour uses the file tree, docs and manifests only."),
    ).not.toBeInTheDocument();
  });
});
