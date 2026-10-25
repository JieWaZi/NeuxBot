import { createRef } from "react";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { CompanionAvatar, type CompanionControl } from "./companion-avatar";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  playback: vi.fn(),
}));
vi.mock("../../../../../vendor/companion/renderer", () => ({
  createCompanion: mocks.create,
  companionReactions: [],
}));
vi.mock("./playback", () => ({ companionPlayback: mocks.playback }));

function renderer() {
  const engine = {
    destroy: vi.fn(),
    renderStatic: vi.fn(),
    ball: { setReduced: vi.fn(), setResolution: vi.fn() },
  };
  mocks.create.mockImplementation((host: HTMLElement) => {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    const body = document.createElementNS(
      "http://www.w3.org/2000/svg",
      "image",
    );
    body.setAttribute("class", "ally-body-image");
    svg.append(body);
    host.append(svg);
    queueMicrotask(() => body.dispatchEvent(new Event("load")));
    return engine;
  });
  const playback = {
    visibility: vi.fn(),
    update: vi.fn(),
    interact: vi.fn(),
    wake: vi.fn(),
    destroy: vi.fn(() => engine.destroy()),
  };
  mocks.playback.mockReturnValue(playback);
  return { engine, playback };
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("共享头像静态展示", () => {
  it("实际绘制后通知入场交接，重新渲染不会重建引擎", async () => {
    const { engine } = renderer();
    const ready = vi.fn(() => expect(engine.renderStatic).toHaveBeenCalled());
    const view = render(<CompanionAvatar avatar="" onReady={ready} />);
    await waitFor(() => expect(ready).toHaveBeenCalledOnce());
    view.rerender(<CompanionAvatar avatar="" onReady={ready} />);
    expect(mocks.create).toHaveBeenCalledOnce();
  });
  it("按目标布局宽度和屏幕像素比选择图片，飞入缩小不降清晰度，卸载后停止监听", async () => {
    const { engine } = renderer();
    let width = 128;
    vi.stubGlobal("devicePixelRatio", 2);
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(
      () => width,
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, 0, width * 0.1, width * 0.1),
    );
    const view = render(<CompanionAvatar avatar="" animated={false} />);
    await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
    expect(mocks.create.mock.calls[0]?.[1]).toMatchObject({ resolution: 256 });
    width = 160;
    fireEvent(window, new Event("resize"));
    expect(engine.ball.setResolution).toHaveBeenLastCalledWith(512);
    width = 128;
    fireEvent(window, new Event("resize"));
    expect(engine.ball.setResolution).toHaveBeenLastCalledWith(256);
    view.unmount();
    const calls = engine.ball.setResolution.mock.calls.length;
    fireEvent(window, new Event("resize"));
    expect(engine.ball.setResolution).toHaveBeenCalledTimes(calls);
  });

  it("静态头像不启动状态或待机播放，活动更新、指针与互动也不会唤醒", async () => {
    const { engine } = renderer();
    const control = createRef<CompanionControl>();
    const view = render(
      <CompanionAvatar avatar="" animated={false} ref={control} />,
    );
    await waitFor(() => expect(engine.renderStatic).toHaveBeenCalledOnce());
    expect(engine.ball.setReduced).toHaveBeenCalledWith(true);
    vi.useFakeTimers();
    view.rerender(
      <CompanionAvatar
        avatar=""
        animated={false}
        activity="thinking"
        ref={control}
      />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("头像容器未渲染");
    fireEvent.pointerEnter(host);
    act(() => {
      control.current?.interact();
      vi.advanceTimersByTime(120_000);
    });
    expect(mocks.playback).not.toHaveBeenCalled();
    expect(engine.renderStatic).toHaveBeenCalledOnce();
    view.unmount();
    expect(engine.destroy).toHaveBeenCalledOnce();
  });

  it("默认动态模式仍播放工作状态和互动，切换静态释放原播放实例", async () => {
    const { engine, playback } = renderer();
    const control = createRef<CompanionControl>();
    const view = render(<CompanionAvatar avatar="" ref={control} />);
    await waitFor(() => expect(mocks.playback).toHaveBeenCalledOnce());
    view.rerender(
      <CompanionAvatar avatar="" activity="thinking" ref={control} />,
    );
    expect(playback.update).toHaveBeenCalledWith("thinking");
    act(() => control.current?.interact());
    expect(playback.interact).toHaveBeenCalledOnce();
    view.rerender(<CompanionAvatar avatar="" animated={false} ref={control} />);
    await waitFor(() => expect(engine.renderStatic).toHaveBeenCalledOnce());
    expect(playback.destroy).toHaveBeenCalledOnce();
    expect(mocks.playback).toHaveBeenCalledOnce();
    view.unmount();
    expect(engine.destroy).toHaveBeenCalledTimes(2);
  });

  it("选择器可直接使用静态 Avatar，不产生嵌套互动按钮；头像 SVG 与普通图标尺寸隔离", async () => {
    renderer();
    const { container } = render(
      <Button size="sm">
        <Avatar name="伙伴" size="xs" animated={false} />
        伙伴
      </Button>,
    );
    await waitFor(() =>
      expect(container.querySelector("svg")).toHaveAttribute(
        "data-companion-avatar",
      ),
    );
    expect(container.querySelectorAll("button")).toHaveLength(1);
    expect(container.querySelector("svg")).toHaveAttribute("width", "100%");
    expect(mocks.playback).not.toHaveBeenCalled();
  });
});
