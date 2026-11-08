import { render, screen, cleanup } from "@testing-library/react";
import { expect, it, afterEach } from "vitest";
import "@/shared/i18n";
import { SkillInstructionsPanel } from "./skill-instructions-panel";
afterEach(cleanup);
it("previews the body without rendering YAML metadata or a source switch", async () => {
  const content =
    "---\nname: test-skill\ndescription: private-metadata\n---\n# Guide\n\n**Usage**";
  render(<SkillInstructionsPanel content={content} />);
  expect(screen.getByRole("heading", { name: "SKILL.md" })).toBeVisible();
  expect(
    await screen.findByRole("heading", { name: "Guide" }, { timeout: 5000 }),
  ).toBeVisible();
  expect(screen.queryByText(/private-metadata/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("radio", { name: "源文件" }),
  ).not.toBeInTheDocument();
});

it("omits the first heading when it only repeats the skill name", async () => {
  render(
    <SkillInstructionsPanel
      name="Guide"
      content={"---\nname: guide\n---\n# Guide\n\nRead the real instructions."}
    />,
  );
  expect(await screen.findByText("Read the real instructions.")).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "Guide" }),
  ).not.toBeInTheDocument();
});

it("preserves an ordinary opening paragraph that matches the skill name", async () => {
  render(
    <SkillInstructionsPanel
      name="Guide"
      content={"Guide\n\nRead the real instructions."}
    />,
  );
  expect(await screen.findByText("Guide")).toBeVisible();
  expect(screen.getByText("Read the real instructions.")).toBeVisible();
});
