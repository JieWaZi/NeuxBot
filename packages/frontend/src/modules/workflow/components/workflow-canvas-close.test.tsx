import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { WorkflowCanvas } from "./workflow-canvas";
import { TooltipProvider } from "@/shared/ui/tooltip";

it.each(["button", "escape"])(
  "节点详情用 %s 关闭时立即移除浮层，不保留空框播放缩小动效",
  async (method) => {
    const getComputedStyle = window.getComputedStyle.bind(window);
    // jsdom 不运行 CSS 动画，补充浏览器的实时 animationName 让 Radix Presence
    // 走真实的退出挂载分支；没有 animationend 时仍应立刻移除节点详情。
    const styles = vi
      .spyOn(window, "getComputedStyle")
      .mockImplementation((element) => {
        const computed = getComputedStyle(element);
        return new Proxy(computed, {
          get(target, key) {
            if (key === "animationName") {
              return element.getAttribute("data-motion") === "popup"
                ? element.getAttribute("data-state") === "open"
                  ? "dialog-in"
                  : "dialog-out"
                : "none";
            }
            const value: unknown = Reflect.get(target, key);
            return typeof value === "function" ? value.bind(target) : value;
          },
        });
      });
    function Canvas() {
      const [selected, setSelected] = useState<string>();
      return (
        <TooltipProvider>
          <WorkflowCanvas
            listMode
            manifest={{
              meta: { phases: [{ id: "phase", title: "执行" }] },
              nodes: [
                {
                  id: "node",
                  phaseId: "phase",
                  kind: "functionTask",
                  labelTemplate: "核对评论",
                },
              ],
            }}
            selected={selected}
            onSelect={(node) => setSelected(node.id)}
            onDismiss={() => setSelected(undefined)}
            nodeContent={() => (selected ? <p>完整执行详情</p> : null)}
          />
        </TooltipProvider>
      );
    }
    try {
      render(<Canvas />);
      fireEvent.click(screen.getByRole("button", { name: "选择 核对评论" }));
      const detail = await screen.findByRole("dialog", { name: "核对评论" });
      await act(async () => {});
      expect(detail).toHaveAttribute("data-motion", "popup");
      if (method === "button")
        fireEvent.click(screen.getByRole("button", { name: "关闭" }));
      else fireEvent.keyDown(detail, { key: "Escape" });
      expect(
        screen.queryByRole("dialog", { name: "核对评论" }),
      ).not.toBeInTheDocument();
      expect(
        document.querySelector('[data-state="closed"][data-motion="popup"]'),
      ).toBeNull();
    } finally {
      styles.mockRestore();
    }
  },
);
