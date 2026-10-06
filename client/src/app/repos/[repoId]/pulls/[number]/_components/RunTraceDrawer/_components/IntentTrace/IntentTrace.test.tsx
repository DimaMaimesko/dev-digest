import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { RunIntent } from "@devdigest/shared";
import messages from "../../../../../../../../../../messages/en/runs.json";
import { IntentTrace } from "./IntentTrace";

afterEach(cleanup);

function renderIntent(intent: RunIntent) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ runs: messages }}>
      <IntentTrace intent={intent} />
    </NextIntlClientProvider>,
  );
}

const BASE: RunIntent = {
  status: "derived",
  reason: null,
  confidence: "medium",
  sources: [{ kind: "title", label: "PR title", ref: null, truncated: false }],
  unresolved: [],
  provider: "anthropic",
  model: "haiku",
};

describe("IntentTrace", () => {
  it("shows a derived intent with its confidence, model and sources", () => {
    renderIntent(BASE);
    expect(screen.getByText("Derived")).toBeInTheDocument();
    expect(screen.getByText("medium")).toBeInTheDocument();
    expect(screen.getByText("anthropic / haiku")).toBeInTheDocument();
    expect(screen.getByText(/title: PR title/)).toBeInTheDocument();
  });

  it("shows a reused intent without re-deriving", () => {
    renderIntent({ ...BASE, status: "reused" });
    expect(screen.getByText("Reused")).toBeInTheDocument();
  });

  it("shows the failure reason and no confidence when derivation failed", () => {
    renderIntent({
      ...BASE,
      status: "failed",
      reason: "timed out after 60s",
      confidence: null,
      sources: [],
    });
    expect(screen.getByText("No intent — timed out after 60s")).toBeInTheDocument();
    expect(screen.queryByText("medium")).not.toBeInTheDocument();
  });
});
