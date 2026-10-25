import { render, screen, cleanup } from "@testing-library/react";
import { expect, it, afterEach } from "vitest";
import "@/shared/i18n";
import { SkillInstructionsPanel } from "./skill-instructions-panel";
afterEach(cleanup);
it("previews the body without rendering YAML metadata or a source switch", async () => {
  const content =
    "---\nname: test-skill\ndescription: private-metadata\n---\n# Guide\n\n**Usage**";
  render(<SkillInstructionsPanel content={content} />);
  expect(screen.getByRole("heading", { name: "内容" })).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "SKILL.md" }),
  ).not.toBeInTheDocument();
  expect(
    await screen.findByRole("heading", { name: "Guide" }, { timeout: 5000 }),
  ).toBeVisible();
  expect(screen.queryByText(/private-metadata/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("radio", { name: "源文件" }),
  ).not.toBeInTheDocument();
});
