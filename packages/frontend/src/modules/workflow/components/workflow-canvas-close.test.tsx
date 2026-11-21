import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { WorkflowCanvas } from "./workflow-canvas";
import { TooltipProvider } from "@/shared/ui/tooltip";

it.each(["button", "escape", "chat"])(
  "节点详情响应所属画布的关闭操作，关闭后立即移除浮层（%s）",
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
          <button type="button">聊天选项</button>
          <WorkflowCanvas
            listMode
            dismissWithinCanvas={method === "chat"}
            manifest={{
              sdk: "bot.workflow",
              title: "测试工作流",
              description: "",
              controls: [],
              edges: [],
              scripts: [],
              phases: [{ detail: "", id: "phase", title: "执行" }],
              nodes: [
                {
                  execution: {
                    codeDigest: "fixture",
                    executionMode: "pure",
                    language: "javascript",
                  },
                  inputs: [],
                  id: "node",
                  phaseId: "phase",
                  kind: "functionTask",
                  label: "核对评论",
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
      if (method === "chat") {
        await userEvent
          .setup()
          .click(screen.getByRole("button", { name: "聊天选项" }));
        expect(detail).toBeInTheDocument();
      }
      if (method !== "escape")
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
