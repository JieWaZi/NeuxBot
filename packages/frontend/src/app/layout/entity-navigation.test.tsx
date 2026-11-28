import { act, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, onTestFinished, vi } from "vitest";

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
      onReorder={() => undefined}
      onSelectPartner={onSelectPartner}
      onSelectTeam={onSelectTeam}
      selectedKey="waker:waker-1"
    />
  );
}

function measureDragRows(entries: [HTMLElement, number][]) {
  const rows = entries.map(([button, top]) => {
    const row = button.parentElement;
    if (!row)
      throw new Error(`找不到伙伴行 ${button.getAttribute("aria-label")}`);
    vi.spyOn(row, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, top, 220, 52),
    );
    return row;
  });
  const getRect = HTMLElement.prototype.getBoundingClientRect;
  const measureOverlay = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockImplementation(function (this: HTMLElement) {
      const { position, left, top, width, height } =
        this.parentElement?.style ?? {};
      return position === "fixed" && left && top && width && height
        ? new DOMRect(
            parseFloat(left),
            parseFloat(top),
            parseFloat(width),
            parseFloat(height),
          )
        : getRect.call(this);
    });
  onTestFinished(() => measureOverlay.mockRestore());
  return rows;
}

describe("EntityNavigation", () => {
  it("悬停伙伴名称和行内操作时保持同一头像表情，离开整行后恢复", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    const user = userEvent.setup();
    const { container } = render(
      <EntityNavigation
        collapsed={false}
        items={[
          {
            id: "waker-1",
            key: "waker:waker-1",
            kind: "waker",
            waker: {
              id: "waker-1",
              name: "伙伴一",
              avatar: "bloub:plush:nuage:curieux:38bdf8",
            } as Waker,
          },
        ]}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        selectedKey="waker:waker-1"
      />,
    );
    const avatar = container.querySelector("svg[data-companion-avatar]");
    expect(avatar).toHaveAttribute("data-expression", "curieux");

    await user.hover(screen.getByRole("button", { name: "伙伴一" }));
    const expression = avatar?.getAttribute("data-expression");
    expect(expression).not.toBe("curieux");

    const profile = screen.getByRole("button", { name: "查看 伙伴一 的资料" });
    await user.hover(profile);
    expect(avatar).toHaveAttribute("data-expression", expression);

    await user.unhover(profile);
    expect(avatar).toHaveAttribute("data-expression", "curieux");
  });

  it.each([
    { collapsed: false, source: "普通聊天" },
    { collapsed: true, source: "普通聊天" },
    { collapsed: false, source: "主会话" },
    { collapsed: true, source: "主会话" },
  ])(
    "伙伴头像显示$source的提问、审批、运行与空闲状态（折叠：$collapsed）",
    ({ collapsed, source }) => {
      // jsdom 的帧回调与 Node 的 performance 时钟起点不同，测试统一推进。
      vi.useFakeTimers({
        toFake: [
          "performance",
          "requestAnimationFrame",
          "cancelAnimationFrame",
        ],
      });
      onTestFinished(() => {
        vi.useRealTimers();
      });
      const advanceAvatar = () => act(() => vi.advanceTimersByTime(750));
      const onSelectPartner = vi.fn();
      const renderEntity = (
        requestKinds: string[],
        runningChatCount: number,
        unreadMessageCount = 12,
        mainConversationPending = false,
      ) => (
        <EntityNavigation
          collapsed={collapsed}
          items={[
            {
              id: "waker-1",
              key: "waker:waker-1",
              kind: "waker",
              waker: {
                id: "waker-1",
                name: "伙伴一",
                activityState: "waiting",
              },
              attention: {
                executorId: "waker-1",
                waitingWorkCount: requestKinds.length,
                runningChatCount,
                mainConversationPending,
                unreadMessageCount,
                ...(source === "主会话"
                  ? { requestKinds, chats: [] }
                  : { chats: [{ requestKinds }] }),
              },
            },
          ]}
          mode="waker"
          onManageEntities={() => undefined}
          onModeChange={() => undefined}
          onReorder={() => undefined}
          onSelectPartner={onSelectPartner}
          onSelectTeam={() => undefined}
          selectedKey="waker:waker-1"
        />
      );
      const view = render(renderEntity(["input", "approval"], 3));
      const avatar = view.container.querySelector<SVGElement>(
        "svg[data-companion-avatar]",
      );
      const nativeMark = (kind: string) =>
        avatar?.querySelector(`[data-native-attention="${kind}"]`);
      advanceAvatar();
      expect(nativeMark("waiting-answer")).toBeInTheDocument();
      expect(avatar).toHaveAttribute("data-animation", "waiting-answer");
      expect(
        screen.getByRole("button", { name: "伙伴一，待输入" }),
      ).toBeInTheDocument();
      expect(
        view.container.querySelector(".lucide-message-circle-question-mark"),
      ).toBeNull();
      if (!collapsed) {
        fireEvent.click(screen.getByRole("button", { name: "待输入" }));
        expect(onSelectPartner).toHaveBeenCalledWith("waker-1");
      }

      view.rerender(renderEntity(["approval"], 3));
      advanceAvatar();
      expect(nativeMark("waiting-approval")).toBeInTheDocument();
      expect(view.container.querySelector(".lucide-shield-check")).toBeNull();

      view.rerender(renderEntity([], 3));
      advanceAvatar();
      expect(nativeMark("working")?.children).toHaveLength(3);
      expect(
        view.container.querySelector("[data-avatar-attention]"),
      ).toBeNull();

      if (source === "主会话") {
        view.rerender(renderEntity([], 0, 0, true));
        advanceAvatar();
        expect(nativeMark("working")?.children).toHaveLength(3);
        expect(
          screen.getByRole("button", { name: "伙伴一，工作中" }),
        ).toBeInTheDocument();
      }

      view.rerender(renderEntity([], 0));
      advanceAvatar();
      expect(nativeMark("unread")).toHaveAttribute("data-unread-count", "12");
      expect(
        screen.getByRole("button", { name: "伙伴一，12 条未读消息" }),
      ).toBeInTheDocument();

      view.rerender(renderEntity([], 0, 0));
      advanceAvatar();
      expect(avatar).toHaveAttribute("data-animation", "idle");
      expect(avatar?.querySelector("[data-native-attention]")).toBeNull();
    },
  );
  it.each([false, true])(
    "团队组合头像以独立状态点表达工作中，并保留待处理提示 (%s)",
    (collapsed) => {
      const renderTeam = (waiting: boolean) => (
        <EntityNavigation
          collapsed={collapsed}
          items={[
            {
              id: "team-1",
              key: "team:team-1",
              kind: "team",
              team: {
                channel: { id: "team-1", name: "团队一" },
                members: [
                  { waker: { id: "waker-1", name: "伙伴一" } },
                  { waker: { id: "waker-2", name: "伙伴二" } },
                ],
              },
              attention: {
                executorId: "team-1",
                waitingWorkCount: waiting ? 1 : 0,
                runningChatCount: 1,
                chats: waiting ? [{ requestKinds: ["input"] }] : [],
              },
            },
          ]}
          mode="team"
          onManageEntities={() => undefined}
          onModeChange={() => undefined}
          onReorder={() => undefined}
          onSelectPartner={() => undefined}
          onSelectTeam={() => undefined}
          selectedKey="team:team-1"
        />
      );
      const view = render(renderTeam(false));
      const mark = view.container.querySelector(
        "[data-avatar-attention] [data-conversation-attention='working']",
      );
      expect(mark).toHaveAccessibleName("工作中");
      expect(mark?.querySelectorAll("i")).toHaveLength(0);
      view.rerender(renderTeam(true));
      expect(
        view.container.querySelector(
          "[data-avatar-attention] [data-conversation-attention='action']",
        ),
      ).toHaveAccessibleName("待输入");
    },
  );
  it.each([true, false])(
    "伙伴上下文不再提供全局模块入口（折叠=%s）",
    (collapsed) => {
      render(
        <NavigationHarness
          collapsed={collapsed}
          onSelectPartner={() => undefined}
          onSelectTeam={() => undefined}
        />,
      );
      for (const name of ["资源库", "设置", "自动任务", "工作流"])
        expect(screen.queryByRole("button", { name })).toBeNull();
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
    expect(collapsedCapsule).toHaveClass("min-h-0", "flex-1");
    expect(
      screen
        .getByRole("navigation", { name: "伙伴" })
        .querySelector(".overflow-y-auto"),
    ).not.toBeNull();

    rerender(
      <NavigationHarness
        collapsed={false}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
      />,
    );
    expect(screen.getByRole("radiogroup")).toBeVisible();
  });

  it("折叠态为加载、错误和空列表提供可读状态", () => {
    const commonProps = {
      collapsed: true,
      items: [],
      mode: "team" as const,
      onManageEntities: () => undefined,
      onModeChange: () => undefined,
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

  it("跨组拖动时固定其他伙伴，并高亮目标分组和插入位置", () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    onTestFinished(() => {
      // The pointer sensor defers removing its document click listener.
      vi.runOnlyPendingTimers();
      vi.useRealTimers();
    });
    const partners = ["甲", "乙", "丙", "丁"].map((name, index) => ({
      id: `partner-${index}`,
      key: `waker:partner-${index}`,
      kind: "waker" as const,
      waker: { id: `partner-${index}`, name } as Waker,
    }));
    const onReorder = vi.fn();
    render(
      <EntityNavigation
        collapsed={false}
        items={partners}
        mode="waker"
        onManageEntities={vi.fn()}
        onModeChange={vi.fn()}
        onReorder={onReorder}
        onSelectPartner={vi.fn()}
        onSelectTeam={vi.fn()}
        selectedKey=""
        wakerGroups={[
          { name: "研究", wakerIds: ["partner-0", "partner-1"] },
          { name: "开发", wakerIds: ["partner-2", "partner-3"] },
        ]}
      />,
    );

    const source = screen.getByRole("button", { name: "甲" });
    const rows = measureDragRows(
      partners.map(({ waker }, index) => [
        screen.getByRole("button", { name: waker.name }),
        index * 54 + (index < 2 ? 24 : 48),
      ]),
    );
    fireEvent.pointerDown(source, {
      isPrimary: true,
      button: 0,
      clientX: 120,
      clientY: 50,
    });
    fireEvent.pointerMove(document, { clientX: 120, clientY: 70 });
    fireEvent.pointerMove(document, { clientX: 120, clientY: 92 });
    expect(rows[1]?.style.transform).toContain("-54px");
    fireEvent.pointerMove(document, { clientX: 120, clientY: 162 });
    expect(rows.slice(1).map((row) => row.style.transform)).toEqual([
      "",
      "",
      "",
    ]);
    const sourceGroup = screen.getByRole("heading", { name: "研究" });
    const targetGroup = screen.getByRole("heading", { name: "开发" });
    expect(sourceGroup).not.toHaveAttribute("data-drop-group");
    expect(targetGroup).toHaveAttribute("data-drop-group", "start");
    expect(rows[2]).toHaveAttribute("data-drop-group", "middle");
    expect(rows[3]).toHaveAttribute("data-drop-group", "end");
    expect(sourceGroup).not.toHaveClass("before:bg-accent-surface");
    expect(targetGroup).toHaveClass("before:bg-accent-surface");
    expect(rows[2]).toHaveClass("before:bg-accent-surface");
    expect(rows[3]).toHaveClass("before:bg-accent-surface");
    expect(rows[2]).toHaveAttribute("data-drop-placement", "before");

    fireEvent.pointerMove(document, { clientX: 120, clientY: 198 });
    expect(rows[2]).toHaveAttribute("data-drop-placement", "after");
    expect(onReorder).not.toHaveBeenCalled();
    fireEvent.pointerUp(document);
    expect(onReorder).toHaveBeenCalledExactlyOnceWith(
      "waker:partner-0",
      "waker:partner-2",
      "after",
    );
    expect(rows[2]).not.toHaveAttribute("data-drop-placement");
    expect(targetGroup).not.toHaveAttribute("data-drop-group");
    expect(targetGroup).not.toHaveClass("before:bg-accent-surface");
    expect(rows[2]).not.toHaveClass("before:bg-accent-surface");
  });

  it("键盘可把伙伴插入下方单人分组的第一行之前", () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const onReorder = vi.fn();
    render(
      <EntityNavigation
        collapsed={false}
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={vi.fn()}
        onModeChange={vi.fn()}
        onReorder={onReorder}
        onSelectPartner={vi.fn()}
        onSelectTeam={vi.fn()}
        selectedKey=""
        wakerGroups={[
          { name: "研究", wakerIds: ["waker-1"] },
          { name: "开发", wakerIds: ["waker-2"] },
        ]}
      />,
    );
    const source = screen.getByRole("button", { name: "伙伴一" });
    const target = screen.getByRole("button", { name: "伙伴二" });
    const [, targetRow] = measureDragRows([
      [source, 24],
      [target, 102],
    ]);
    source.focus();
    fireEvent.keyDown(source, { code: "Space", key: " " });
    act(() => vi.runOnlyPendingTimers());
    fireEvent.keyDown(source, { code: "ArrowDown", key: "ArrowDown" });
    expect(targetRow).toHaveAttribute("data-drop-placement", "after");
    fireEvent.keyDown(source, { code: "ArrowLeft", key: "ArrowLeft" });
    expect(targetRow).toHaveAttribute("data-drop-placement", "before");
    expect(screen.getByText("插入到「伙伴二」之前")).toHaveAttribute(
      "role",
      "status",
    );
    fireEvent.keyDown(source, { code: "ArrowRight", key: "ArrowRight" });
    expect(targetRow).toHaveAttribute("data-drop-placement", "after");
    fireEvent.keyDown(source, { code: "ArrowLeft", key: "ArrowLeft" });
    fireEvent.keyDown(source, { code: "Space", key: " " });
    expect(onReorder).toHaveBeenCalledExactlyOnceWith(
      "waker:waker-1",
      "waker:waker-2",
      "before",
    );
    expect(source).toHaveFocus();
  });

  it("展开态双击头像打开伙伴资料卡", () => {
    const onShowcaseWaker = vi.fn();
    render(
      <EntityNavigation
        collapsed={false}
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        onShowcaseWaker={onShowcaseWaker}
        selectedKey="waker:waker-1"
      />,
    );
    const avatar = screen.getByRole("button", { name: "查看 伙伴一 的资料" });
    fireEvent.doubleClick(avatar);
    expect(onShowcaseWaker).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ id: "waker-1", name: "伙伴一" }),
      avatar,
    );
  });

  it("展开态团队行保留头像槽，并在悬停整行时触发头像表情", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    const user = userEvent.setup();
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
        onReorder={() => undefined}
        onSelectPartner={() => undefined}
        onSelectTeam={() => undefined}
        selectedKey="team:team-1"
      />,
    );

    const row = screen.getByRole("button", { name: "测试" }).closest("div");
    const avatarSlot = row?.querySelector(".size-avatar-navigation");
    expect(row).toHaveClass("grid-cols-[auto_minmax(0,1fr)]");
    expect(avatarSlot?.parentElement).toHaveClass("inline-flex", "shrink-0");
    expect(avatarSlot).toHaveClass("inline-grid", "size-avatar-navigation");

    const avatar = avatarSlot?.querySelector("svg[data-companion-avatar]");
    const resting = avatar?.getAttribute("data-expression");
    await user.hover(screen.getByRole("button", { name: "测试" }));
    const expression = avatar?.getAttribute("data-expression");
    expect(expression).not.toBe(resting);

    const teamAvatar = screen.getByRole("img");
    await user.hover(teamAvatar);
    expect(avatar).toHaveAttribute("data-expression", expression);

    await user.unhover(teamAvatar);
    expect(avatar).toHaveAttribute("data-expression", resting);
  });

  it("悬浮身份选择后进入会话层并支持返回或关闭", async () => {
    const user = userEvent.setup();
    const onSelectPartner = vi.fn();
    render(
      <FloatingEntityNavigation
        items={items.filter((item) => item.kind === "waker")}
        mode="waker"
        onManageEntities={() => undefined}
        onModeChange={() => undefined}
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
