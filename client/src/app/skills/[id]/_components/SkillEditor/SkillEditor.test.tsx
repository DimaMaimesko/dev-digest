import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { Skill, SkillVersion } from "@devdigest/shared";
import messages from "../../../../../../messages/en/skills.json";
import { ToastProvider } from "../../../../../lib/toast";

const { update, restore } = vi.hoisted(() => ({ update: vi.fn(), restore: vi.fn() }));
const VERSIONS: SkillVersion[] = [
  { skill_id: "sk1", version: 2, body: "# Rubric v2", message: "Added tests", created_at: "2026-10-02T12:00:00.000Z" },
  { skill_id: "sk1", version: 1, body: "# Rubric", message: null, created_at: "2026-10-01T12:00:00.000Z" },
];
vi.mock("../../../../../lib/hooks/skills", () => ({
  useUpdateSkill: () => ({ mutate: update, isPending: false }),
  useSkillAgents: () => ({ data: [{ id: "ag1", name: "General Reviewer" }] }),
  useSkillVersions: () => ({ data: VERSIONS, isLoading: false, isError: false }),
  useRestoreSkillVersion: () => ({ mutate: restore, isPending: false }),
}));

import { SkillEditor } from "./SkillEditor";

afterEach(() => {
  cleanup();
  update.mockReset();
  restore.mockReset();
});

const SKILL: Skill = {
  id: "sk1",
  name: "pr-quality-rubric",
  description: "Overall PR quality",
  type: "rubric",
  source: "manual",
  body: "# Rubric v2",
  enabled: true,
  version: 2,
  evidence_files: null,
  agent_count: 1,
  created_at: "2026-10-01T12:00:00.000Z",
};

function renderTab(tab: string) {
  return render(
    <NextIntlClientProvider locale="en" messages={{ skills: messages }}>
      <ToastProvider>
        <SkillEditor skill={SKILL} tab={tab} onTab={() => {}} />
      </ToastProvider>
    </NextIntlClientProvider>,
  );
}

describe("SkillEditor", () => {
  it("Config: lists the agents using the skill", () => {
    renderTab("config");
    expect(screen.getByText("Configuration")).toBeInTheDocument();
    expect(screen.getByText("General Reviewer")).toBeInTheDocument();
  });

  it("Config: saving without a body change sends no version note", () => {
    renderTab("config");
    expect(screen.queryByText("Version note")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Save skill"));
    expect(update).toHaveBeenCalledWith(
      { id: "sk1", patch: { name: "pr-quality-rubric", description: "Overall PR quality", type: "rubric", body: "# Rubric v2", enabled: true } },
      expect.anything(),
    );
  });

  it("Config: a changed body is marked unsaved and saved with its note", () => {
    renderTab("config");
    fireEvent.change(screen.getByDisplayValue("# Rubric v2"), { target: { value: "# Rubric v3" } });
    expect(screen.getByText("unsaved")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("Tightened scope rule"), { target: { value: "Added scope" } });
    fireEvent.click(screen.getByText("Save skill"));
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({ patch: expect.objectContaining({ body: "# Rubric v3", message: "Added scope" }) }),
      expect.anything(),
    );
  });

  it("Preview: renders the skill under its name, as the prompt holds it", () => {
    renderTab("preview");
    expect(screen.getByRole("heading", { name: "pr-quality-rubric" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Rubric v2" })).toBeInTheDocument();
  });

  it("Versions: marks the current version and restores an older one", () => {
    renderTab("versions");
    expect(screen.getByText("2 versions")).toBeInTheDocument();
    expect(screen.getByText("Current")).toBeInTheDocument();
    expect(screen.getByText("No note")).toBeInTheDocument();
    // Only the older version can be restored: getByText fails on two matches.
    fireEvent.click(screen.getByText("Restore"));
    expect(restore).toHaveBeenCalledWith({ id: "sk1", version: 1 }, expect.anything());
  });
});
