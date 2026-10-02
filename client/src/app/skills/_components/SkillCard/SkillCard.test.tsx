import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { Skill } from "@devdigest/shared";
import messages from "../../../../../messages/en/skills.json";

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));
vi.mock("../../../../lib/hooks/skills", () => ({
  useDeleteSkill: () => ({ mutate, isPending: false }),
}));

import { SkillCard } from "./SkillCard";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  mutate.mockReset();
});

const SKILL: Skill = {
  id: "sk1",
  name: "secret-leakage-gate",
  description: "Detects sk_live keys",
  type: "security",
  source: "community",
  body: "# Gate",
  enabled: true,
  version: 2,
  evidence_files: null,
  agent_count: 2,
  created_at: "2026-10-02T12:00:00.000Z",
};

function renderCard(skill: Skill = SKILL) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ skills: messages }}>
      <SkillCard skill={skill} />
    </NextIntlClientProvider>,
  );
}

describe("SkillCard", () => {
  it("shows the name, type, source and how many agents use it", () => {
    renderCard();
    expect(screen.getByText("secret-leakage-gate")).toBeInTheDocument();
    expect(screen.getByText("security")).toBeInTheDocument();
    expect(screen.getByText("Community")).toBeInTheDocument();
    expect(screen.getByText("2 agents")).toBeInTheDocument();
  });

  it("says when no agent uses it", () => {
    renderCard({ ...SKILL, agent_count: 0 });
    expect(screen.getByText("No agents")).toBeInTheDocument();
  });

  it("warns that deleting removes it from its agents, and deletes on confirm", () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    renderCard();
    fireEvent.click(screen.getByLabelText("Delete skill"));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("removed from 2 agents"));
    expect(mutate).toHaveBeenCalledWith("sk1", expect.anything());
  });

  it("keeps the skill when the delete isn't confirmed", () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    renderCard();
    fireEvent.click(screen.getByLabelText("Delete skill"));
    expect(mutate).not.toHaveBeenCalled();
  });
});
