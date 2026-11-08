import { render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { SkillCatalog } from "./skill-catalog";
import type { SkillSummary } from "../model/types";

vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("./skill-card", () => ({
  SkillCard: ({ skill }: { skill: SkillSummary }) => <span>{skill.name}</span>,
}));

it("shows a skill in each selected category and keeps untagged skills separate", () => {
  const base: SkillSummary = {
    id: "review",
    name: "审查",
    description: "",
    source: "zip",
    enabled: false,
    affectedWakerCount: 0,
    version: "1.0.0",
    updatedAt: "",
  };
  const props = {
    skills: [
      { ...base, categories: ["开发", "写作"] },
      { ...base, id: "other", name: "其他" },
    ],
    wakerId: "",
    paginationKey: "all",
    onCategoryChange: vi.fn(),
    onEnabledChange: vi.fn(),
  };
  const view = render(<SkillCatalog {...props} category={null} />);
  expect(
    within(screen.getByRole("region", { name: "开发" })).getByText("审查"),
  ).toBeVisible();
  expect(
    within(screen.getByRole("region", { name: "写作" })).getByText("审查"),
  ).toBeVisible();
  expect(
    within(screen.getByRole("region", { name: "未分类" })).getByText("其他"),
  ).toBeVisible();
  for (const name of ["开发", "写作", "未分类"]) {
    expect(
      within(screen.getByRole("region", { name })).getByRole("heading"),
    ).toHaveTextContent(`${name}1`);
  }
  view.rerender(<SkillCatalog {...props} category="写作" />);
  expect(screen.getAllByText("审查")).toHaveLength(1);
  expect(screen.queryByText("其他")).not.toBeInTheDocument();
});
