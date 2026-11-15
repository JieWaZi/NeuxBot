import { useEffect, useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppSceneActivity } from "./app-scene-activity";

describe("保留 Scene 的后台生命周期", () => {
  it("暂停策略隐藏时释放消费，恢复时保留同一输入节点与草稿", () => {
    const start = vi.fn();
    const stop = vi.fn();
    function Draft() {
      const [text, setText] = useState("");
      useEffect(() => {
        start();
        return stop;
      }, []);
      return (
        <input
          aria-label="草稿"
          value={text}
          onChange={(event) => setText(event.target.value)}
        />
      );
    }
    const view = (active: boolean) => (
      <AppSceneActivity background="suspend" active={active}>
        <Draft />
      </AppSceneActivity>
    );
    const { rerender, unmount } = render(view(true));
    const input = screen.getByRole("textbox", { name: "草稿" });
    fireEvent.change(input, { target: { value: "保留未提交内容" } });
    rerender(view(false));
    expect(input).not.toBeVisible();
    expect(start).toHaveBeenCalledTimes(1);
    expect(stop).toHaveBeenCalledTimes(1);
    rerender(view(true));
    expect(screen.getByRole("textbox", { name: "草稿" })).toBe(input);
    expect(input).toHaveValue("保留未提交内容");
    expect(start).toHaveBeenCalledTimes(2);
    unmount();
    expect(stop).toHaveBeenCalledTimes(2);
  });

  it.each(["preserve", undefined] as const)(
    "策略 %s 隐藏不改变原生资源的挂载生命周期",
    (background) => {
      const stop = vi.fn();
      function NativeView() {
        useEffect(() => stop, []);
        return <span>原生视图身份</span>;
      }
      const view = (active: boolean) => (
        <AppSceneActivity background={background} active={active}>
          <NativeView />
        </AppSceneActivity>
      );
      const { rerender, unmount } = render(view(true));
      rerender(view(false));
      rerender(view(true));
      expect(stop).not.toHaveBeenCalled();
      unmount();
      expect(stop).toHaveBeenCalledOnce();
    },
  );
});
