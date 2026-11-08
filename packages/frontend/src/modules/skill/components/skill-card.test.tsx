import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { toSkillSummary } from "../model/skill-view";
import { SkillCard } from "./skill-card";

afterEach(cleanup);

it("目录行只展示名称和简介，打开详情并独立处理安装", () => {
  const skill = toSkillSummary({
    skill: {
      id: "writing",
      name: "writing-skills",
      description: "写作方法",
      source: "git",
      gitUrl: "https://github.com/example/writing.git",
    },
    affectedWakerCount: 6,
  });
  if (!skill) throw new Error("missing skill fixture");
  const install = vi.fn();
  render(
    <MemoryRouter>
      <SkillCard
        skill={skill}
        wakerId=""
        onEnabledChange={vi.fn()}
        onInstall={install}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/来自/)).not.toBeInTheDocument();
  expect(screen.getByText("写作方法")).toBeVisible();
  expect(
    screen.getByRole("link", { name: "查看技能 writing-skills" }),
  ).toHaveAttribute("href", "/resources/skills/writing");
  expect(screen.getByRole("article")).toHaveAttribute("data-appearance", "row");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByText("6 个伙伴使用")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /更多操作/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "安装" }));
  expect(install).toHaveBeenCalledOnce();
});

it("伙伴目录行保留真实启停能力与伙伴详情地址", () => {
  const skill = toSkillSummary({
    skill: { id: "writing", name: "写作技能", source: "zip" },
    enabled: true,
  });
  if (!skill) throw new Error("missing skill fixture");
  const toggle = vi.fn();
  render(
    <MemoryRouter>
      <SkillCard skill={skill} wakerId="w1" onEnabledChange={toggle} />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/来自/)).not.toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "查看技能 写作技能" }),
  ).toHaveAttribute("href", "/wakers/w1/skills/writing");
  fireEvent.click(screen.getByRole("switch", { name: "停用 写作技能" }));
  expect(toggle).toHaveBeenCalledWith(false);
});
