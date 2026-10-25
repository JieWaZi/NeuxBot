import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import type { Waker } from "@/modules/waker";
import "@/shared/i18n";

import {
  EntityNavigation,
  FloatingEntityNavigation,
  type NavigationEntity,
} from "./entity-navigation";
import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";

const items: NavigationEntity[] = [
  {
    id: "waker-1",
    key: "waker:waker-1",
    kind: "waker",
    waker: { id: "waker-1", name: "伙伴一" } as Waker,
  },
  {
    id: "waker-2",
    key: "waker:waker-2",
    kind: "waker",
    waker: { id: "waker-2", name: "伙伴二" } as Waker,
  },
  {
    id: "team-1",
    key: "team:team-1",
    kind: "team",
    team: {
      channel: { id: "team-1", name: "团队一" },
      members: [],
    },
  },
];

function NavigationHarness({
  collapsed = true,
  onSelectPartner,
  onSelectTeam,
}: {
  collapsed?: boolean;
  onSelectPartner: (id: string) => void;
  onSelectTeam: (id: string) => void;
}) {
  const [mode, setMode] =
    useState<PartnerNavigationPreferences["entityMode"]>("waker");
  return (
    <EntityNavigation
      collapsed={collapsed}
      items={items.filter((item) => item.kind === mode)}
      mode={mode}
      onManageEntities={() => undefined}
      onModeChange={setMode}
      onNewChat={() => undefined}
      onOpenSettings={() => undefined}
      onReorder={() => undefined}
      onSelectPartner={onSelectPartner}
      onSelectTeam={onSelectTeam}
      selectedKey="waker:waker-1"
    />
  );
}

describe("EntityNavigation", () => {
  it.each([true, false])(
    "工作流合并后仅保留资源库入口（侧栏收起=%s）",
    async (collapsed) => {
      const user = userEvent.setup();
      const resources = vi.fn();
      const automatic = vi.fn();
      const settings = vi.fn();
      render(
        <EntityNavigation
          collapsed={collapsed}
          items={items}
          mode="waker"
          onManageEntities={() => undefined}
          onModeChange={() => undefined}
          onNewChat={() => undefined}
          onOpenSettings={settings}
          onOpenAutomaticTasks={automatic}
          onOpenResources={resources}
          onReorder={() => undefined}
          onSelectPartner={() => undefined}
          onSelectTeam={() => undefined}
          selectedKey="waker:waker-1"
        />,
      );
      for (const [name, callback] of [
        ["资源库", resources],
        ["自动任务", automatic],
        ["设置", settings],
      ] as const) {
        await user.click(screen.getByRole("button", { name }));
        expect(callback).toHaveBeenCalledTimes(1);
      }
      expect(screen.queryByRole("button", { name: "工作流" })).toBeNull();
      expect(
        screen.queryByRole("button", { name: /展开侧栏菜单|收起侧栏菜单/ }),
      ).toBeNull();
    },
  );

  it("悬浮入口展开实体胶囊并在选择后关闭", async () => {
    const user = userEvent.setup();
    const onSelectPartner = vi.fn();
    render(
      <FloatingEntityNavigation
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onNewChat={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={onSelectPartner}
        onSelectTeam={() => undefined}
        selectedItem={items[0]}
        selectedKey="waker:waker-1"
      />,
    );

    const trigger = screen.getByRole("button", {
      name: "打开身份切换器，当前：伙伴一",
    });
    expect(trigger).toHaveClass("rounded-tl-lg");
    expect(trigger).toHaveClass(
      "hover:w-control-launcher",
      "focus-visible:w-control-launcher",
    );
    expect(trigger).not.toHaveAttribute("title");
    expect(trigger.querySelector(".lucide-users-round")).not.toBeNull();
    expect(trigger.querySelector("img")).toBeNull();
    expect(within(trigger).getByText("选择伙伴")).toHaveClass(
      "opacity-0",
      "group-hover/launcher:opacity-100",
    );
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();

    await user.click(trigger);
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴" })).getByRole(
        "button",
        { name: "伙伴二" },
      ),
    );

    expect(onSelectPartner).toHaveBeenCalledWith("waker-2");
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
  });

  it("切换伙伴与团队列表时不改变当前选择", async () => {
    const user = userEvent.setup();
    const onSelectPartner = vi.fn();
    const onSelectTeam = vi.fn();
    render(
      <NavigationHarness
        onSelectPartner={onSelectPartner}
        onSelectTeam={onSelectTeam}
      />,
    );

    expect(screen.getByRole("button", { name: "伙伴一" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    await user.click(screen.getByRole("button", { name: "切换到团队" }));

    expect(screen.getByRole("button", { name: "团队一" })).not.toHaveAttribute(
      "aria-current",
    );
    expect(screen.getByRole("button", { name: "切换到伙伴" })).toBeDisabled();
    expect(onSelectPartner).not.toHaveBeenCalled();
    expect(onSelectTeam).not.toHaveBeenCalled();
  });

  it("折叠态胶囊与展开态分段控件共用相同顶部间距", () => {
    const { rerender } = render(
      <NavigationHarness
        collapsed
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );
    const collapsedCapsule = screen
      .getByRole("button", { name: "切换到团队" })
      .closest("[data-collapsed='true'] > div");
    expect(collapsedCapsule).not.toHaveClass("mt-2");
    expect(collapsedCapsule).toHaveClass("shrink-0");
    expect(collapsedCapsule).not.toHaveClass("flex-1");

    rerender(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );
    expect(screen.getByRole("radiogroup")).toBeVisible();
  });

  it("设置和自动任务入口不在侧栏标记为当前页", () => {
    render(
      <EntityNavigation
        collapsed={false}
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onOpenAutomaticTasks={() => undefined}
        onNewChat={() => undefined}
        onOpenSettings={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        selectedKey="waker:waker-1"
      />,
    );

    expect(screen.getByRole("button", { name: "设置" })).not.toHaveAttribute(
      "aria-current",
    );
    expect(
      screen.getByRole("button", { name: "自动任务" }),
    ).not.toHaveAttribute("aria-current");
  });

  it("折叠态将工具区固定在侧栏底部", () => {
    render(
      <div className="h-80">
        <NavigationHarness
          collapsed
          onSelectPartner={() => undefined}
          onSelectTeam={() => undefined}
        />
      </div>,
    );

    const settings = screen.getByRole("button", { name: "设置" });
    expect(settings.closest("div.border-t")).toHaveClass("mt-auto", "border-t");
  });

  it("折叠态为加载、错误和空列表提供可读状态", () => {
    const commonProps = {
      collapsed: true,
      items: [],
      mode: "team" as const,
      onManageEntities: () => undefined,
      onModeChange: () => undefined,
      onNewChat: () => undefined,
      onOpenSettings: () => undefined,
      onReorder: () => undefined,
      onSelectPartner: () => undefined,
      onSelectTeam: () => undefined,
      selectedKey: "",
    };
    const { rerender } = render(
      <EntityNavigation {...commonProps} entityStatus="loading" />,
    );
    expect(screen.getByRole("status", { name: "正在载入团队" })).toBeVisible();

    rerender(<EntityNavigation {...commonProps} entityStatus="error" />);
    expect(
      screen.getByRole("alert", { name: "团队暂时无法载入" }),
    ).toBeVisible();

    rerender(<EntityNavigation {...commonProps} entityStatus="ready" />);
    expect(screen.getByRole("status", { name: "还没有团队" })).toBeVisible();
  });

  it("展开态工具区使用全宽分隔线并与列表共用对齐基准", () => {
    render(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );

    expect(
      screen.getByRole("button", { name: "设置" }).closest("div.border-t"),
    ).toHaveClass("border-t");
    expect(screen.getByRole("navigation", { name: "伙伴" })).toHaveClass(
      "overflow-x-hidden",
    );
    expect(screen.getByRole("radiogroup").closest(".px-1")).toBeNull();
  });

  it("展开态单击伙伴后直接切换且不内嵌会话", async () => {
    const user = userEvent.setup();
    const onSelectPartner = vi.fn();
    render(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={onSelectPartner}
        onSelectTeam={() => undefined}
      />,
    );

    await user.click(screen.getByRole("button", { name: "伙伴二" }));

    expect(onSelectPartner).toHaveBeenCalledWith("waker-2");
    expect(screen.getByRole("button", { name: "伙伴一" })).not.toHaveAttribute(
      "aria-expanded",
    );
  });

  it("展开态连点两次头像打开伙伴卡片", () => {
    const onShowcaseWaker = vi.fn();
    render(
      <EntityNavigation
        collapsed={false}
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onNewChat={() => undefined}
        onOpenSettings={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        onShowcaseWaker={onShowcaseWaker}
        selectedKey="waker:waker-1"
      />,
    );

    const avatar = screen.getByRole("button", { name: "与 伙伴一 互动" });
    fireEvent.click(avatar);
    fireEvent.click(avatar);

    expect(onShowcaseWaker).toHaveBeenCalledWith(
      expect.objectContaining({ id: "waker-1", name: "伙伴一" }),
      expect.any(HTMLElement),
    );
  });

  it("展开态团队行把组合头像约束在导航头像槽内", () => {
    render(
      <EntityNavigation
        collapsed={false}
        items={[
          {
            id: "team-1",
            key: "team:team-1",
            kind: "team",
            team: {
              channel: { id: "team-1", name: "测试" },
              members: [
                {
                  waker: { id: "waker-1", name: "伙伴一" },
                },
              ],
            },
          },
        ]}
        mode="team"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onNewChat={() => undefined}
        onOpenSettings={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        selectedKey="team:team-1"
      />,
    );

    const row = screen.getByRole("button", { name: "测试" }).closest("div");
    const avatarSlot = row?.querySelector(".size-avatar-sm");
    expect(row).toHaveClass("grid-cols-[auto_minmax(0,1fr)_auto]");
    expect(avatarSlot?.parentElement).toHaveClass("flex", "shrink-0");
    expect(avatarSlot).toHaveClass("inline-flex", "size-avatar-sm");
  });

  it.each(["waker", "team"] as const)(
    "%s 行加号打开新会话，且不触发行选择",
    async (mode) => {
      const user = userEvent.setup();
      const onNewChat = vi.fn();
      const onSelectPartner = vi.fn();
      const onSelectTeam = vi.fn();
      const entityItems = items.filter((item) => item.kind === mode);
      render(
        <EntityNavigation
          collapsed={false}
          items={entityItems}
          mode={mode}
          onManageEntities={() => undefined}
          onModeChange={() => undefined}
          onNewChat={onNewChat}
          onOpenSettings={() => undefined}
          onReorder={() => undefined}
          onSelectPartner={onSelectPartner}
          onSelectTeam={onSelectTeam}
          selectedKey="waker:waker-1"
        />,
      );

      const firstButton = screen.getAllByRole("button", {
        name: "新建会话",
      })[0];
      if (!firstButton) throw new Error("缺少新建会话按钮");
      await user.click(firstButton);
      expect(onNewChat).toHaveBeenCalledExactlyOnceWith(entityItems[0]);
      expect(onSelectPartner).not.toHaveBeenCalled();
      expect(onSelectTeam).not.toHaveBeenCalled();
    },
  );

  it("悬浮身份选择后进入会话层并支持返回或关闭", async () => {
    const user = userEvent.setup();
    const onSelectPartner = vi.fn();
    render(
      <FloatingEntityNavigation
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onNewChat={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={onSelectPartner}
        onSelectTeam={() => undefined}
        renderEntityChats={(item, onBack, onClose) => (
          <div>
            <p>{item.kind === "waker" ? item.waker.name : "团队一"}历史会话</p>
            <button type="button" onClick={onBack}>
              返回身份
            </button>
            <button type="button" onClick={onClose}>
              打开会话
            </button>
          </div>
        )}
        selectedItem={items[0]}
        selectedKey="waker:waker-1"
      />,
    );

    await user.click(
      screen.getByRole("button", {
        name: "打开身份切换器，当前：伙伴一",
      }),
    );
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴" })).getByRole(
        "button",
        { name: "伙伴二" },
      ),
    );

    expect(screen.getByText("伙伴二历史会话")).toBeVisible();
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(onSelectPartner).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "返回身份" }));
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();

    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await user.click(screen.getByRole("button", { name: "打开会话" }));
    expect(screen.queryByText("伙伴一历史会话")).toBeNull();

    await user.click(
      screen.getByRole("button", {
        name: "打开身份切换器，当前：伙伴一",
      }),
    );
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();
    expect(screen.queryByText("伙伴一历史会话")).toBeNull();
  });

  it("悬浮胶囊限制最大高度并在内部滚动完整实体列表", async () => {
    const user = userEvent.setup();
    const manyItems: NavigationEntity[] = Array.from(
      { length: 6 },
      (_, index) => ({
        id: `waker-${index + 1}`,
        key: `waker:waker-${index + 1}`,
        kind: "waker",
        waker: { id: `waker-${index + 1}`, name: `伙伴 ${index + 1}` } as Waker,
      }),
    );

    render(
      <FloatingEntityNavigation
        items={manyItems}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onNewChat={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        selectedItem={manyItems[5]}
        selectedKey="waker:waker-6"
      />,
    );

    await user.click(
      screen.getByRole("button", {
        name: "打开身份切换器，当前：伙伴 6",
      }),
    );

    const switcher = screen.getByLabelText("身份切换器");
    const rail = within(switcher).getByRole("navigation", { name: "伙伴" });
    expect(switcher).toHaveClass(
      "max-h-[var(--radix-popover-content-available-height)]",
    );
    expect(rail.querySelector(".overflow-y-auto")).not.toBeNull();
    expect(within(rail).getByRole("button", { name: "伙伴 6" })).toBeVisible();
    expect(
      within(rail).queryByRole("button", { name: "展开全部伙伴" }),
    ).toBeNull();

    const modeSwitch = within(switcher).getByRole("button", {
      name: "切换到团队",
    });
    expect(modeSwitch).not.toHaveAttribute("title");
    await user.hover(modeSwitch);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("展开态使用新建与搜索工具，并按当前模式过滤实体", async () => {
    const user = userEvent.setup();
    render(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );

    expect(screen.getByRole("button", { name: "新建伙伴" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "搜索伙伴" }));
    await user.type(
      screen.getByRole("searchbox", { name: "搜索伙伴" }),
      "伙伴二",
    );

    expect(screen.queryByRole("button", { name: "伙伴一" })).toBeNull();
    expect(screen.getByRole("button", { name: "伙伴二" })).toBeVisible();
  });

  it("搜索栏失去焦点后恢复为新建按钮和搜索图标", async () => {
    const user = userEvent.setup();
    render(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );

    await user.click(screen.getByRole("button", { name: "搜索伙伴" }));
    const searchbox = screen.getByRole("searchbox", { name: "搜索伙伴" });
    await user.type(searchbox, "伙伴二");
    expect(screen.queryByRole("button", { name: "伙伴一" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "新建伙伴" }));
    expect(screen.queryByRole("searchbox", { name: "搜索伙伴" })).toBeNull();
    expect(screen.getByRole("button", { name: "搜索伙伴" })).toBeVisible();
    expect(screen.getByRole("button", { name: "新建伙伴" })).toHaveTextContent(
      "新建伙伴",
    );
    expect(screen.getByRole("button", { name: "伙伴一" })).toBeVisible();
  });
});
