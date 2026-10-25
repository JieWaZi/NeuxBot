import { useLayoutEffect } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
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
  it("失效标签在未选中时保留淡红背景、感叹号与悬停提示，并可关闭", async () => {
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
    expect(tab.parentElement).toHaveClass("bg-destructive/10");
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
    const strip = screen.getByRole("tablist", { name: "场景标签" })
      .parentElement as HTMLElement;
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
