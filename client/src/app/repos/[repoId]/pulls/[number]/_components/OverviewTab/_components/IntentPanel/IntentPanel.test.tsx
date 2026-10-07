import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { PrIntentResponse } from "@/lib/types";
import messages from "../../../../../../../../../../messages/en/intent.json";

const mockUsePrIntent = vi.fn();
vi.mock("@/lib/hooks/intent", () => ({
  usePrIntent: (...args: unknown[]) => mockUsePrIntent(...args),
}));

import { IntentPanel } from "./IntentPanel";

afterEach(cleanup);

const FULL: PrIntentResponse = {
  pr_id: "p1",
  intent: "Protect the public API from abuse.",
  in_scope: ["Token-bucket rate limiting middleware"],
  out_of_scope: ["Authenticated and internal endpoints"],
  confidence: "medium",
  sources: [{ kind: "title", label: "pr-title", ref: null, truncated: false }],
  unresolved: [{ ref: "specs/x.md", reason: "not found" }],
  head_sha: "abc",
  provider: "anthropic",
  model: "haiku",
  tokens_in: 100,
  tokens_out: 50,
  cost_usd: null,
  derived_at: "2026-10-06T10:00:00.000Z",
};

function renderPanel() {
  return render(
    <NextIntlClientProvider locale="en" messages={{ intent: messages }}>
      <IntentPanel prId="p1" />
    </NextIntlClientProvider>,
  );
}

describe("IntentPanel", () => {
  it("renders the statement, scope lists, confidence, sources and unresolved refs", () => {
    mockUsePrIntent.mockReturnValue({ data: FULL });
    renderPanel();

    expect(screen.getByText("Intent")).toBeInTheDocument();
    expect(screen.getByText("Protect the public API from abuse.")).toBeInTheDocument();
    expect(screen.getByText("In scope")).toBeInTheDocument();
    expect(screen.getByText("Token-bucket rate limiting middleware")).toBeInTheDocument();
    expect(screen.getByText("Out of scope")).toBeInTheDocument();
    expect(screen.getByText("Authenticated and internal endpoints")).toBeInTheDocument();
    expect(screen.getByText("Confidence: medium")).toBeInTheDocument();
    expect(screen.getByText("Sources")).toBeInTheDocument();
    expect(screen.getByText("pr-title")).toBeInTheDocument();
    expect(screen.getByText("Unresolved references")).toBeInTheDocument();
    expect(screen.getByText("specs/x.md — not found")).toBeInTheDocument();
  });

  it("shows the empty-state text and no error when there is no stored intent", () => {
    mockUsePrIntent.mockReturnValue({ data: null });
    renderPanel();

    expect(screen.getByText("Intent is derived on the next review.")).toBeInTheDocument();
    expect(screen.queryByText(/error/i)).not.toBeInTheDocument();
  });

  it("renders an untrusted statement literally, with no img element in the DOM", () => {
    mockUsePrIntent.mockReturnValue({
      data: {
        ...FULL,
        intent: "![x](https://evil.example/p.png) <img src=x>",
        sources: [],
        unresolved: [],
      },
    });
    renderPanel();

    expect(
      screen.getByText("![x](https://evil.example/p.png) <img src=x>"),
    ).toBeInTheDocument();
    expect(document.querySelector("img")).not.toBeInTheDocument();
  });
});
