import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup, fireEvent, act } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { Agent, Skill } from "@devdigest/shared";
import messages from "../../../../../../../../messages/en/agents.json";

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));
const skill = (id: string, enabled = true): Skill => ({
  id,
  name: id,
  description: "",
  type: "security",
  source: "manual",
  body: "b",
  enabled,
  version: 1,
  evidence_files: null,
  agent_count: 0,
  created_at: "2026-10-02T12:00:00.000Z",
});
const SKILLS = [skill("rubric"), skill("secret-gate"), skill("off", false)];
// The agent links secret-gate, then rubric.
const LINKS = [
  { agent_id: "ag1", skill_id: "secret-gate", order: 0 },
  { agent_id: "ag1", skill_id: "rubric", order: 1 },
];

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("../../../../../../../lib/hooks/skills", () => ({
  useSkills: () => ({ data: SKILLS, isLoading: false, isError: false }),
}));
vi.mock("../../../../../../../lib/hooks/agents", () => ({
  useAgentSkills: () => ({ data: LINKS, isLoading: false, isError: false }),
  useSetAgentSkills: () => ({ mutate }),
}));

import { SkillsTab } from "./SkillsTab";

afterEach(() => {
  cleanup();
  mutate.mockReset();
});

const AGENT = { id: "ag1", name: "Security Reviewer" } as Agent;

function renderTab() {
  return render(
    <NextIntlClientProvider locale="en" messages={{ agents: messages }}>
      <SkillsTab agent={AGENT} />
    </NextIntlClientProvider>,
  );
}

const saved = () => mutate.mock.calls.at(-1)?.[0];

describe("Agent SkillsTab", () => {
  it("lists linked skills first, in the agent's order, and counts them", () => {
    renderTab();
    expect(screen.getByText("2 of 3 attached")).toBeInTheDocument();
    const names = screen.getAllByRole("checkbox").map((c) => c.closest("label")?.textContent);
    expect(names).toEqual(["secret-gate", "rubric", "off"]);
    expect(screen.getByText("disabled · left out of prompts")).toBeInTheDocument();
  });

  it("attaches a skill last, saving the whole list", () => {
    renderTab();
    fireEvent.click(screen.getByRole("checkbox", { name: "off" }));
    expect(saved()).toEqual({ id: "ag1", skillIds: ["secret-gate", "rubric", "off"] });
  });

  it("detaches a skill", () => {
    renderTab();
    fireEvent.click(screen.getByRole("checkbox", { name: "secret-gate" }));
    expect(saved()).toEqual({ id: "ag1", skillIds: ["rubric"] });
  });

  it("sends one save at a time, then only the latest change", () => {
    renderTab();
    fireEvent.click(screen.getByRole("checkbox", { name: "off" }));
    // While the first save is in flight, two more changes build on it...
    fireEvent.click(screen.getByRole("checkbox", { name: "secret-gate" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "rubric" }));
    expect(mutate).toHaveBeenCalledTimes(1);
    // ...and when it finishes, only the last list is sent.
    const [, { onSettled }] = mutate.mock.calls[0]!;
    act(() => onSettled());
    expect(mutate).toHaveBeenCalledTimes(2);
    expect(saved()).toEqual({ id: "ag1", skillIds: ["off"] });
  });

  it("reorders with the arrows", () => {
    renderTab();
    expect(screen.getByLabelText("Move secret-gate up")).toBeDisabled();
    fireEvent.click(screen.getByLabelText("Move secret-gate down"));
    expect(saved()).toEqual({ id: "ag1", skillIds: ["rubric", "secret-gate"] });
  });

  it("reorders by dragging", () => {
    renderTab();
    const row = (name: string) => screen.getByRole("checkbox", { name }).closest("[draggable]")!;
    fireEvent.dragStart(row("rubric"));
    fireEvent.dragOver(row("secret-gate"));
    fireEvent.drop(row("secret-gate"));
    expect(saved()).toEqual({ id: "ag1", skillIds: ["rubric", "secret-gate"] });
  });

  it("doesn't reorder a filtered list", () => {
    renderTab();
    fireEvent.change(screen.getByPlaceholderText("Filter skills…"), { target: { value: "gate" } });
    expect(screen.getByText("Clear the filter to reorder.")).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "rubric" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("Move secret-gate down")).toBeDisabled();
  });
});
