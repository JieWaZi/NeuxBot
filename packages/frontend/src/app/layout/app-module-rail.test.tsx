import { useState } from "react";
import type { AppModuleId } from "./app-navigation-model";
import {
  pinnedAppModules,
  type AppPinnableModuleId,
} from "./partner-navigation-preferences";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { AppModuleRail } from "./app-module-rail";

describe("AppModuleRail", () => {
  it("固定提供全部模块入口，不承载通知入口", async () => {
    const user = userEvent.setup();
    const onNavigate = vi.fn();
    render(<AppModuleRail activeModule="resources" onNavigate={onNavigate} />);
    const rail = screen.getByRole("navigation", { name: "模块导航" });
    for (const [label, id] of [
      ["伙伴", "partners"],
      ["任务看板", "work-board"],
      ["用量统计", "usage"],
      ["资源库", "resources"],
      ["设置", "settings"],
    ] as const) {
      await user.click(within(rail).getByRole("button", { name: label }));
      expect(onNavigate).toHaveBeenLastCalledWith(id);
    }
    expect(
      within(rail).getByRole("button", { name: "资源库" }),
    ).toHaveAttribute("aria-current", "page");
    expect(
      screen.queryByRole("button", { name: "通知" }),
    ).not.toBeInTheDocument();
    expect(rail.querySelector("img")).toBeNull();
    await user.click(within(rail).getByRole("button", { name: "更多模块" }));
    expect(screen.queryByRole("menuitem", { name: "工作流" })).toBeNull();
    expect(screen.queryByRole("button", { name: "工作流" })).toBeNull();
    await user.click(screen.getByRole("menuitem", { name: "知识库" }));
    expect(onNavigate).toHaveBeenLastCalledWith("knowledge");
    await user.click(within(rail).getByRole("button", { name: "更多模块" }));
    await user.click(screen.getByRole("menuitem", { name: "自动任务" }));
    expect(onNavigate).toHaveBeenLastCalledWith("automatic-tasks");
  });

  it("更多子模块保留选中语义并在 Escape 后恢复触发按钮焦点", async () => {
    const user = userEvent.setup();
    render(
      <AppModuleRail activeModule="knowledge" onNavigate={() => undefined} />,
    );
    const more = screen.getByRole("button", { name: "更多模块" });
    expect(more).toHaveAttribute("aria-current", "page");
    more.focus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("menuitem", { name: "知识库" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await user.keyboard("{Escape}");
    expect(more).toHaveFocus();
  });
});

function PinRail({
  activeModule = "knowledge",
}: {
  activeModule?: AppModuleId;
}) {
  const [choices, setChoices] = useState<Record<string, boolean>>({});
  return (
    <AppModuleRail
      activeModule={activeModule}
      onNavigate={() => undefined}
      pinnedModules={pinnedAppModules(choices)}
      onPinChange={(id: AppPinnableModuleId, pinned: boolean) =>
        setChoices((current) => ({ ...current, [id]: pinned }))
      }
    />
  );
}
it("More列出所有可pin模块，pin/unpin不导航、不关闭菜单并保持固定顺序", async () => {
  const user = userEvent.setup();
  render(<PinRail />);
  const rail = screen.getByRole("navigation", { name: "模块导航" });
  expect(within(rail).queryByRole("button", { name: "知识库" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "更多模块" }));
  const pin = screen.getByRole("menuitemcheckbox", { name: "固定知识库" });
  await user.click(pin);
  expect(pin).toHaveAttribute("aria-checked", "true");
  expect(screen.getByRole("menu")).toBeVisible();
  expect(
    within(rail).getByRole("button", { name: "知识库", hidden: true }),
  ).toHaveAttribute("aria-current", "page");
  expect(
    within(rail).getByRole("button", { name: "更多模块", hidden: true }),
  ).not.toHaveAttribute("aria-current");
  expect(
    screen.queryByRole("menuitemcheckbox", { name: "固定工作流" }),
  ).toBeNull();
  await user.click(
    screen.getByRole("menuitemcheckbox", { name: "固定知识库" }),
  );
  expect(
    within(rail).queryByRole("button", { name: "知识库", hidden: true }),
  ).toBeNull();
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "更多模块" })).toHaveFocus();
  expect(screen.getByRole("button", { name: "更多模块" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});
