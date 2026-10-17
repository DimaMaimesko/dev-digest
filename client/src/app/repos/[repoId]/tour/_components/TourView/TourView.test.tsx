import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { OnboardingTourResponse, OnboardingTour } from "@/lib/types";
import { ApiError } from "@/lib/api";
import messages from "../../../../../../../messages/en/onboardingTour.json";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const mockUseOnboardingTour = vi.fn();
const mockUseGenerateTour = vi.fn();
vi.mock("@/lib/hooks/onboarding", () => ({
  useOnboardingTour: (...args: unknown[]) => mockUseOnboardingTour(...args),
  useGenerateTour: (...args: unknown[]) => mockUseGenerateTour(...args),
}));

import { TourView } from "./TourView";

afterEach(cleanup);

function renderView() {
  return render(
    <NextIntlClientProvider locale="en" messages={{ onboardingTour: messages }}>
      <TourView repoId="r1" fullName="acme/payments-api" />
    </NextIntlClientProvider>,
  );
}

const TOUR: OnboardingTour = {
  repo_id: "r1",
  commit_sha: "a1b2c3d4e5f6",
  files_indexed: 42,
  index_status: "full",
  code_index_empty: false,
  generated_at: "2026-10-01T10:00:00.000Z",
  provider: "openrouter",
  model: "deepseek/deepseek-v4-flash",
  cost_usd: 0.0042,
  sections: [
    {
      kind: "architecture",
      status: "done",
      reason: null,
      removed_refs: 1,
      content: {
        summary: [
          { type: "text", text: "The service is entered through " },
          { type: "path", path: "src/api/users.ts", is_dir: false },
          { type: "text", text: ". <img src=x> stays literal." },
        ],
        diagram: null,
      },
    },
    {
      kind: "critical_paths",
      status: "done",
      reason: null,
      removed_refs: 0,
      content: { items: [{ path: "src/config.ts", is_dir: false, reason: "Loads all runtime config." }] },
    },
    {
      kind: "run_locally",
      status: "done",
      reason: null,
      removed_refs: 0,
      content: {
        steps: [
          { command: "pnpm install", comment: null },
          { command: "pnpm dev", comment: "then fill in the keys" },
        ],
      },
    },
    {
      kind: "reading_path",
      status: "failed",
      reason: "no verifiable entries",
      removed_refs: 0,
      content: null,
    },
    {
      kind: "first_tasks",
      status: "done",
      reason: null,
      removed_refs: 0,
      content: {
        tasks: [
          {
            title: "Fix the rate limiter",
            description: "Address the TODO.",
            size: "small",
            paths: [{ path: "src/middleware/ratelimit.ts", is_dir: false }],
            todo: { path: "src/middleware/ratelimit.ts", line: 12, text: "handle burst traffic" },
          },
        ],
      },
    },
  ],
};

const READY_INDEX = { state: "ready" as const, commit_sha: "a1b2c3d4e5f6", files_indexed: 42, partial: false, code_index_empty: false };

function response(partial: Partial<OnboardingTourResponse> = {}): OnboardingTourResponse {
  return { index: READY_INDEX, tour: null, generation: null, ...partial };
}

describe("TourView", () => {
  it("shows the empty state when there is no tour yet (AC-2)", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response(), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    expect(screen.getByText("No tour yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Generate tour" })).toBeEnabled();
  });

  it("disables Generate with the reason when the repo isn't indexed (AC-3)", () => {
    mockUseOnboardingTour.mockReturnValue({
      data: response({ index: { state: "not_indexed", commit_sha: null, files_indexed: 0, partial: false, code_index_empty: false } }),
      isLoading: false,
      isError: false,
    });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    expect(screen.getByText("Repository not indexed yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Generate tour" })).toBeDisabled();
  });

  it("renders the header, sections, statuses and removed-refs count", () => {
    mockUseOnboardingTour.mockReturnValue({
      data: response({ tour: TOUR, generation: null }),
      isLoading: false,
      isError: false,
    });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();

    expect(screen.getByText(/Onboarding for acme\/payments-api/)).toBeInTheDocument();
    expect(screen.getByText(/Generated from index of 42 files/)).toBeInTheDocument();
    expect(screen.getAllByText("Architecture overview").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Critical paths").length).toBeGreaterThan(0);
    expect(screen.getAllByText("How to run locally").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Guided reading path").length).toBeGreaterThan(0);
    expect(screen.getAllByText("First tasks").length).toBeGreaterThan(0);
    expect(screen.getByText("1 unverified reference removed")).toBeInTheDocument();
    expect(screen.getByText("no verifiable entries")).toBeInTheDocument();
    expect(screen.getByText("Diagram unavailable")).toBeInTheDocument();
    expect(screen.getByText("$0.0042")).toBeInTheDocument();
    // untrusted model text renders literally, no element created from it
    expect(screen.getByText(/img src=x/)).toBeInTheDocument();
    expect(document.querySelector("img")).not.toBeInTheDocument();
    // no Share action anywhere on the page (AC-42)
    expect(screen.queryByText(/share/i)).not.toBeInTheDocument();
  });

  it("shows the stale notice with both short shas (AC-14)", () => {
    mockUseOnboardingTour.mockReturnValue({
      data: response({ tour: TOUR, index: { ...READY_INDEX, commit_sha: "f6e5d4c3b2a1" } }),
      isLoading: false,
      isError: false,
    });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    expect(screen.getByText(/a1b2c3d.*to.*f6e5d4c/)).toBeInTheDocument();
  });

  it("shows a config_error inline with the Settings link (AC-33)", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response(), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({
      mutate: vi.fn(),
      isPending: false,
      error: new ApiError(
        "OPENROUTER_API_KEY is not configured for provider openrouter",
        400,
        "config_error",
      ),
    });
    renderView();
    expect(screen.getByText(/OPENROUTER_API_KEY is not configured/)).toBeInTheDocument();
    expect(screen.getByText("Settings → API Keys")).toBeInTheDocument();
  });

  it("shows \"Generation failed\" with the reason (AC-35/36)", () => {
    mockUseOnboardingTour.mockReturnValue({
      data: response({
        tour: TOUR,
        generation: {
          id: "g1",
          repo_id: "r1",
          status: "failed",
          error: "all sections failed",
          commit_sha: "a1b2c3d4e5f6",
          created_at: "2026-10-01T10:00:00.000Z",
          finished_at: "2026-10-01T10:01:00.000Z",
          sections: [],
        },
      }),
      isLoading: false,
      isError: false,
    });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    expect(screen.getByText(/Generation failed/)).toBeInTheDocument();
    expect(screen.getByText(/all sections failed/)).toBeInTheDocument();
  });

  it("collapses and expands a section with aria-expanded (AC-38)", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response({ tour: TOUR }), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    const header = screen.getByRole("button", { name: /Architecture overview/ });
    expect(header).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
  });

  it("lists all 5 sections under On this page (AC-38)", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response({ tour: TOUR }), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    const nav = screen.getByRole("navigation", { name: "On this page" });
    expect(nav.querySelectorAll("a").length).toBe(5);
  });

  it("copies the exact run-locally command and shows Copied / Copy failed (AC-26)", async () => {
    mockUseOnboardingTour.mockReturnValue({ data: response({ tour: TOUR }), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderView();

    const btn = screen.getByRole("button", { name: "Copy command: pnpm install" });
    fireEvent.click(btn);
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith("pnpm install"));
    await screen.findByText("Copied");
    // the accessible name stays fixed even once copied
    expect(screen.getByRole("button", { name: "Copy command: pnpm install" })).toBeInTheDocument();
  });

  it("shows the fixed run-locally caution note (AC-27)", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response({ tour: TOUR }), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    expect(screen.getByText("Generated from repository files — review before running.")).toBeInTheDocument();
  });

  it("shows the Open link for a grounded path", () => {
    mockUseOnboardingTour.mockReturnValue({ data: response({ tour: TOUR }), isLoading: false, isError: false });
    mockUseGenerateTour.mockReturnValue({ mutate: vi.fn(), isPending: false, error: null });
    renderView();
    const link = screen.getAllByRole("link", { name: /Open/ })[0];
    expect(link).toHaveAttribute(
      "href",
      "https://github.com/acme/payments-api/blob/a1b2c3d4e5f6/src/api/users.ts",
    );
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
