import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { RunTrace, RunIntent } from "@devdigest/shared";
import messages from "../../../../../../../../../../messages/en/runs.json";
import { TraceBody } from "./TraceBody";

afterEach(cleanup);

const TRACE: RunTrace = {
  config: { agent: "General", version: "1", provider: "openrouter", model: "m", pr: 482, source: "local" },
  stats: { duration_ms: 8200, tokens_in: 12000, tokens_out: 1500, findings: 2, grounding: "2/2 passed" },
  prompt_assembly: { system: "s", user: "u" },
  tool_calls: [],
  raw_output: "",
  memory_pulled: [],
  specs_read: [],
  log: [],
};

function renderStats(stats: Partial<RunTrace["stats"]>) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ runs: messages }}>
      <TraceBody trace={{ ...TRACE, stats: { ...TRACE.stats, ...stats } }} findings={[]} />
    </NextIntlClientProvider>,
  );
}

describe("TraceBody — stats", () => {
  it("shows the run's cost next to duration, tokens and findings", () => {
    renderStats({ cost_usd: 0.0123456 });
    for (const label of ["DURATION", "TOKENS", "FINDINGS", "COST"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(screen.getByText("$0.01")).toHaveAttribute("title", "$0.0123456");
  });

  it("shows — when the provider didn't report a cost, or the trace predates it", () => {
    renderStats({ cost_usd: null });
    expect(screen.getByText("—")).toBeInTheDocument();
    cleanup();
    renderStats({}); // no cost_usd key at all
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("shows a free run as $0.00, not unknown", () => {
    renderStats({ cost_usd: 0 });
    expect(screen.getByText("$0.00")).toBeInTheDocument();
  });
});

const INTENT: RunIntent = {
  status: "derived",
  reason: null,
  confidence: "high",
  sources: [],
  unresolved: [],
  provider: "anthropic",
  model: "haiku",
};

function renderTrace(trace: Partial<RunTrace>) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ runs: messages }}>
      <TraceBody trace={{ ...TRACE, ...trace }} findings={[]} />
    </NextIntlClientProvider>,
  );
}

describe("TraceBody — intent", () => {
  it("hides the Intent section on an old trace with no intent", () => {
    renderTrace({ intent: null });
    expect(screen.queryByText("Intent")).not.toBeInTheDocument();
  });

  it("shows the Intent section and its status when the trace has an intent", () => {
    renderTrace({ intent: INTENT });
    expect(screen.getAllByText("Intent").length).toBeGreaterThan(0);
    expect(screen.getByText("Derived")).toBeInTheDocument();
  });

  it("renders a failed intent's reason", () => {
    renderTrace({ intent: { ...INTENT, status: "failed", reason: "model gave no valid intent in 2 attempts" } });
    expect(screen.getByText("No intent — model gave no valid intent in 2 attempts")).toBeInTheDocument();
  });

  it("renders the pr-intent prompt block as text, even when it contains an <img> tag", () => {
    renderTrace({
      intent: INTENT,
      prompt_assembly: { ...TRACE.prompt_assembly, intent: '<img src="x" onerror="alert(1)"> some intent text' },
    });
    // The Prompt assembly section is collapsed by default; open it, then the
    // intent block itself, to reach the rendered text.
    fireEvent.click(screen.getByText("Prompt assembly"));
    const intentLabels = screen.getAllByText("Intent");
    const intentPromptLabel = intentLabels.at(-1);
    if (!intentPromptLabel) throw new Error("expected an Intent prompt label");
    fireEvent.click(intentPromptLabel);
    expect(screen.getByText(/some intent text/)).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });
});
