import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { ArchitectureContent } from "@/lib/types";
import messages from "../../../../../../../../../messages/en/onboardingTour.json";

import { ArchitectureSection } from "./ArchitectureSection";

afterEach(cleanup);

function renderSection(content: ArchitectureContent, fullName: string | null | undefined = "acme/payments-api") {
  return render(
    <NextIntlClientProvider locale="en" messages={{ onboardingTour: messages }}>
      <ArchitectureSection content={content} fullName={fullName} sha="a1b2c3d4e5f6" />
    </NextIntlClientProvider>,
  );
}

describe("ArchitectureSection", () => {
  it("renders text segments as plain text and path segments as a code chip with an Open link (AC-17/AC-20)", () => {
    renderSection({
      summary: [
        { type: "text", text: "The service is entered through " },
        { type: "path", path: "src/api/users.ts", is_dir: false },
        { type: "text", text: " and configured by " },
        { type: "path", path: "src/config", is_dir: true },
        { type: "text", text: "." },
      ],
      diagram: null,
    });

    expect(screen.getByText(/The service is entered through/)).toBeInTheDocument();
    expect(screen.getByText(/and configured by/)).toBeInTheDocument();
    expect(screen.getByText("src/api/users.ts")).toBeInTheDocument();
    expect(screen.getByText("src/config")).toBeInTheDocument();

    const links = screen.getAllByRole("link", { name: /Open/ });
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveAttribute(
      "href",
      "https://github.com/acme/payments-api/blob/a1b2c3d4e5f6/src/api/users.ts",
    );
    expect(links[0]).toHaveAttribute("target", "_blank");
    expect(links[0]).toHaveAttribute("rel", "noopener noreferrer");
    expect(links[1]).toHaveAttribute(
      "href",
      "https://github.com/acme/payments-api/tree/a1b2c3d4e5f6/src/config",
    );
  });

  it("omits the Open link when there is no repo full name", () => {
    renderSection(
      {
        summary: [{ type: "path", path: "src/api/users.ts", is_dir: false }],
        diagram: null,
      },
      null,
    );
    expect(screen.getByText("src/api/users.ts")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Open/ })).not.toBeInTheDocument();
  });

  it("shows 'Diagram unavailable' when the diagram is null (AC-23)", () => {
    renderSection({ summary: [{ type: "text", text: "No diagram here." }], diagram: null });
    expect(screen.getByText("Diagram unavailable")).toBeInTheDocument();
  });
});
