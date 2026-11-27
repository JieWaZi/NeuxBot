import { useLayoutEffect } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SceneChromeActionsProvider } from "./scene-chrome-actions";
import { i18n } from "@/shared/i18n";
import fixedTabStyles from "@/shared/styles/fixed-scene-tab.css?raw";
import sharedStyles from "@/shared/styles/index.css?raw";
import {
  SceneChromePortalProvider,
  useSceneChromeAccessoryWidthSetter,
} from "./scene-chrome-portal";
import { SceneTabStrip } from "./scene-tab-strip";
import { TooltipProvider } from "./tooltip";

function AccessoryWidth({ side = "right" }: { side?: "left" | "right" }) {
  const setWidth = useSceneChromeAccessoryWidthSetter();
  useLayoutEffect(() => {
    setWidth?.("test", 320, false, side);
    return () => setWidth?.("test", null);
  }, [setWidth, side]);
  return null;
}

describe("SceneTabStrip", () => {
  it("固定伙伴按身份取宽，普通对话保留标准宽度并在窄栏按下限收缩", () => {
    const { container } = render(
      <TooltipProvider>
        <style>
          {sharedStyles.match(/^\.shell-header-tab \{[^}]+\}/m)?.[0]}
        </style>
        <style>{fixedTabStyles}</style>
        <SceneTabStrip
          activeTabId="lumi"
          ariaLabel="聊天标签"
          tabs={[
            {
              id: "lumi",
              label: "lumi",
              fixed: true,
              indicator: <span>头像</span>,
            },
            { id: "draft", label: "新会话", closeable: true },
          ]}
          onSelect={() => {}}
          onClose={() => {}}
          trailing={<button>新建会话</button>}
        />
      </TooltipProvider>,
    );
    const [main, ordinary] = Array.from(
      container.querySelectorAll("[data-scene-tab-item]"),
      (element) => getComputedStyle(element),
    );
    expect(main?.width).toBe("max-content");
    expect(main?.flexBasis).toBe("auto");
    expect(main?.minWidth).toBe("var(--spacing-control-md)");
    expect(main?.position).toBe("sticky");
    expect(main?.insetInlineStart).toBe("0px");
    expect(main?.marginInlineEnd).toBe("var(--spacing)");
    expect(ordinary?.width).toBe("auto");
    expect(ordinary?.minWidth).toBe("var(--spacing-scene-tab-min)");
    expect(ordinary?.maxWidth).toBe("var(--spacing-scene-tab)");
    const tablist = getComputedStyle(screen.getByRole("tablist"));
    expect(tablist.minWidth).toBe("min-content");
    expect(tablist.gridTemplateColumns).toBe("max-content");
    expect(tablist.gridAutoColumns.replace(/\s+/g, "")).toBe(
      "minmax(var(--spacing-scene-tab-min),var(--spacing-scene-tab))",
    );
  });

  it.each([
    ["zh-CN", "重命名", "关闭其他页签", "页签名称", "保存"],
    ["en-US", "Rename", "Close other tabs", "Tab name", "Save"],
  ])(
    "%s 右键保留目标页签并提供批量关闭入口",
    async (language, renameLabel, othersLabel, nameLabel, saveLabel) => {
      await i18n.changeLanguage(language);
      try {
        const onRename = vi.fn();
        const onCloseOthers = vi.fn();
        const onSelect = vi.fn();
        render(
          <SceneTabStrip
            activeTabId="chat"
            ariaLabel="场景标签"
            onClose={() => {}}
            onRename={onRename}
            onCloseOthers={onCloseOthers}
            onSelect={onSelect}
            tabs={[
              { id: "chat", label: "对话", closeable: true },
              { id: "settings", label: "设置", closeable: true },
            ]}
          />,
        );
        const tab = screen.getByRole("tab", { name: "设置" });
        fireEvent.contextMenu(tab);
        await userEvent
          .setup()
          .click(await screen.findByRole("menuitem", { name: othersLabel }));
        expect(onCloseOthers).toHaveBeenCalledExactlyOnceWith("settings");
        expect(onSelect).not.toHaveBeenCalled();
        fireEvent.contextMenu(tab);
        await userEvent
          .setup()
          .click(await screen.findByRole("menuitem", { name: renameLabel }));
        const input = await screen.findByRole("textbox", { name: nameLabel });
        const user = userEvent.setup();
        await user.clear(input);
        expect(screen.getByRole("button", { name: saveLabel })).toBeDisabled();
        await user.type(input, "  自定义页签  ");
        await user.click(screen.getByRole("button", { name: saveLabel }));
        expect(onRename).toHaveBeenCalledExactlyOnceWith(
          "settings",
          "自定义页签",
        );
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      } finally {
        await i18n.changeLanguage("zh-CN");
      }
    },
  );

  it("无可关闭页签时禁用菜单操作，Shift+F10 支持键盘打开", async () => {
    render(
      <SceneTabStrip
        activeTabId="fixed"
        ariaLabel="场景标签"
        onClose={() => {}}
        onRename={() => {}}
        onCloseOthers={() => {}}
        onSelect={() => {}}
        tabs={[{ id: "fixed", label: "固定页签" }]}
      />,
    );
    const user = userEvent.setup();
    screen.getByRole("tab", { name: "固定页签" }).focus();
    await user.keyboard("{Shift>}{F10}{/Shift}");
    expect(
      await screen.findByRole("menuitem", { name: "重命名" }),
    ).not.toHaveAttribute("aria-disabled", "true");
    expect(screen.queryByRole("menuitem", { name: "关闭全部" })).toBeNull();
    expect(
      screen.getByRole("menuitem", { name: "关闭其他页签" }),
    ).toHaveAttribute("aria-disabled", "true");
  });

  it("失效标签在未选中时保留错误说明、感叹号与悬停提示，并可关闭", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onSelect = vi.fn();
    render(
      <TooltipProvider delayDuration={0}>
        <SceneTabStrip
          activeTabId="chat"
          ariaLabel="场景标签"
          onClose={onClose}
          onSelect={onSelect}
          tabs={[
            { id: "chat", label: "对话" },
            { id: "missing", label: "详情", closeable: true, notFound: true },
          ]}
        />
      </TooltipProvider>,
    );
    const tab = screen.getByRole("tab", { name: "详情" });
    expect(tab).toHaveAttribute("aria-selected", "false");
    expect(tab).toHaveAccessibleDescription("数据可能已被删除，请关闭该页面");
    expect(tab.querySelector(".lucide-circle-alert")).not.toBeNull();
    await user.hover(tab);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "数据可能已被删除，请关闭该页面",
    );
    await user.click(tab);
    expect(onSelect).toHaveBeenCalledWith("missing");
    await user.click(screen.getByRole("button", { name: "关闭 详情" }));
    expect(onClose).toHaveBeenCalledWith("missing");
  });

  it("工作台顶栏不会盖住聊天与工作台之间的分割线", () => {
    render(
      <SceneChromePortalProvider>
        <AccessoryWidth />
        <SceneTabStrip
          activeTabId="chat"
          ariaLabel="场景标签"
          onClose={() => {}}
          onSelect={() => {}}
          tabs={[{ id: "chat", label: "对话" }]}
        />
      </SceneChromePortalProvider>,
    );

    const accessory = screen
      .getByRole("tablist", { name: "场景标签" })
      .closest("[data-scene-tab-strip]")
      ?.querySelector("[data-scene-chrome-accessory]");
    if (!accessory) throw new Error("工作台顶栏未挂载");
    const separator = accessory?.parentElement?.querySelector(
      "[data-scene-chrome-divider]",
    );
    if (!separator) throw new Error("聊天与工作台之间的分割线未挂载");
    expect(
      separator.compareDocumentPosition(accessory) &
        Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy();
  });

  it("工作台移到左侧时，顶栏也排在聊天标签之前", () => {
    render(
      <SceneChromePortalProvider>
        <AccessoryWidth side="left" />
        <SceneTabStrip
          activeTabId="chat"
          ariaLabel="场景标签"
          onClose={() => {}}
          onSelect={() => {}}
          tabs={[{ id: "chat", label: "对话" }]}
        />
      </SceneChromePortalProvider>,
    );
    const strip = screen
      .getByRole("tablist", { name: "场景标签" })
      .closest("[data-scene-tab-strip]") as HTMLElement;
    const accessory = strip.querySelector("[data-scene-chrome-accessory]");
    const tablist = screen.getByRole("tablist", { name: "场景标签" });
    const divider = strip.querySelector("[data-scene-chrome-divider]");
    if (!accessory || !divider) throw new Error("工作台顶栏或分隔线未挂载");
    expect(strip).toHaveAttribute("data-accessory-left");
    expect(
      accessory.compareDocumentPosition(tablist) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      accessory.compareDocumentPosition(divider) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});

it.each([
  [2, false],
  [20, false],
  [20, true],
] as const)(
  "%s 个页签、首项固定为 %s 时，末尾新建随同一轨道滚动",
  async (count, fixed) => {
    const onSelect = vi.fn();
    const onNew = vi.fn();
    render(
      <TooltipProvider>
        <SceneChromeActionsProvider actions={<button>工作区开关</button>}>
          <SceneTabStrip
            activeTabId="chat-1"
            ariaLabel="聊天标签"
            tabs={Array.from({ length: count }, (_, index) => ({
              id: `chat-${index + 1}`,
              label: `聊天${index + 1}`,
              fixed: index === 0 && fixed,
            }))}
            onSelect={onSelect}
            onClose={() => {}}
            paneActions={<button>摘要</button>}
            trailing={<button onClick={onNew}>新建会话</button>}
          />
        </SceneChromeActionsProvider>
      </TooltipProvider>,
    );
    const first = screen.getByRole("tab", { name: "聊天1" });
    const last = screen.getByRole("tab", { name: `聊天${count}` });
    const add = screen.getByRole("button", { name: "新建会话" });
    const scroll = first.closest("[data-scene-tab-scroll]");
    expect(scroll).not.toBeNull();
    expect(first.closest("[data-fixed]") !== null).toBe(fixed);
    expect(scroll).toContainElement(last);
    expect(scroll).toContainElement(add);
    expect(scroll).not.toContainElement(
      screen.getByRole("button", { name: "摘要" }),
    );
    expect(scroll).not.toContainElement(
      screen.getByRole("button", { name: "工作区开关" }),
    );
    const user = userEvent.setup();
    first.focus();
    await user.keyboard("{End}");
    expect(last).toHaveFocus();
    expect(onSelect).toHaveBeenLastCalledWith(`chat-${count}`);
    await user.tab();
    expect(add).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(onNew).toHaveBeenCalledOnce();
  },
);
