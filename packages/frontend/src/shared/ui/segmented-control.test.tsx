import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { SegmentedControl } from "./segmented-control";

it("SegmentedControl 使用统一轨道样式并支持键盘切换", async () => {
  const user = userEvent.setup();
  const onValueChange = vi.fn();
  render(
    <SegmentedControl
      ariaLabel="实体模式"
      distribution="fill"
      onValueChange={onValueChange}
      options={[
        { label: "伙伴", value: "waker" },
        { label: "团队", value: "team" },
      ]}
      size="sm"
      value="waker"
    />,
  );

  const group = screen.getByRole("radiogroup", { name: "实体模式" });
  expect(group).toHaveClass("bg-button-fill", "rounded-control");
  expect(group).not.toHaveClass("border");

  const waker = screen.getByRole("radio", { name: "伙伴" });
  const team = screen.getByRole("radio", { name: "团队" });
  expect(waker).toHaveAttribute("aria-checked", "true");
  expect(waker).toHaveClass(
    "bg-surface-raised",
    "rounded-control-inner",
    "font-medium",
  );
  expect(waker).not.toHaveClass("shadow-xs");
  expect(team).toHaveAttribute("aria-checked", "false");
  expect(team).toHaveClass("text-button-content");

  waker.focus();
  await user.keyboard("{ArrowRight}");
  expect(onValueChange).toHaveBeenCalledWith("team");
});

it("卡片选项保留简短名称与说明，键盘切换跳过不可用项", async () => {
  const user = userEvent.setup();
  const onValueChange = vi.fn();
  render(
    <SegmentedControl
      appearance="cards"
      ariaLabel="触发方式"
      onValueChange={onValueChange}
      options={[
        { label: "定时", value: "schedule", description: "按计划自动运行" },
        { label: "事件", value: "event", disabled: true },
        { label: "本机 API", value: "api", description: "收到本机请求时运行" },
      ]}
      value="schedule"
    />,
  );
  const scheduled = screen.getByRole("radio", { name: "定时" });
  expect(scheduled).toHaveAccessibleDescription("按计划自动运行");
  scheduled.focus();
  await user.keyboard("{ArrowRight}");
  expect(onValueChange).toHaveBeenCalledWith("api");
  expect(screen.getByRole("radio", { name: "本机 API" })).toHaveFocus();
});

it("资源分类卡片保留单选语义、说明和键盘焦点，跳过禁用分类", async () => {
  const user = userEvent.setup();
  const onValueChange = vi.fn();
  render(
    <SegmentedControl
      appearance="resources"
      ariaLabel="资源分类"
      onValueChange={onValueChange}
      value="plugins"
      options={[
        { label: "插件", value: "plugins", description: "扩展伙伴能力" },
        { label: "技能", value: "skills", disabled: true },
        { label: "连接器", value: "connectors", description: "连接外部工具" },
        { label: "工作流", value: "workflows", description: "复用工作流程" },
      ]}
    />,
  );
  const plugins = screen.getByRole("radio", { name: "插件" });
  const connectors = screen.getByRole("radio", { name: "连接器" });
  expect(plugins).toHaveAttribute("aria-checked", "true");
  expect(plugins).not.toHaveAttribute("aria-pressed");
  expect(plugins).toHaveAccessibleDescription("扩展伙伴能力");
  expect(connectors).toHaveAccessibleDescription("连接外部工具");
  plugins.focus();
  await user.keyboard("{ArrowRight}");
  expect(onValueChange).toHaveBeenLastCalledWith("connectors");
  expect(connectors).toHaveFocus();
  await user.keyboard("{End}");
  const workflows = screen.getByRole("radio", { name: "工作流" });
  expect(workflows).toHaveFocus();
  expect(workflows).toHaveAccessibleDescription("复用工作流程");
  expect(onValueChange).toHaveBeenLastCalledWith("workflows");
  await user.keyboard("{Home}");
  expect(plugins).toHaveFocus();
  await user.click(connectors);
  expect(onValueChange).toHaveBeenLastCalledWith("connectors");
});
