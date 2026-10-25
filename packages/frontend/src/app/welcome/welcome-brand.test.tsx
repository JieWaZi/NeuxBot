import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { WelcomeBrand } from "./welcome-brand";
const loading = vi.hoisted(() => ({ logo: vi.fn(), theme: "light" }));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: loading.theme }),
}));
vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: ({
    onReady,
    onError,
  }: {
    onReady: () => void;
    onError: () => void;
  }) => (
    <>
      <button onClick={onReady}>形象已绘制</button>
      <button onClick={onError}>形象绘制失败</button>
    </>
  ),
}));
vi.mock("./elastic-logo", () => {
  loading.logo();
  return {
    default: ({
      intro,
      onIntroComplete,
    }: {
      intro: boolean;
      onIntroComplete: () => void;
    }) => (
      <button data-intro={intro} onClick={onIntroComplete}>
        字标入场结束
      </button>
    ),
  };
});
afterEach(() => {
  loading.theme = "light";
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("形象加载失败后使用新品牌标志，并跟随当前浅暗主题", () => {
  const view = render(<WelcomeBrand intro={false} />);
  fireEvent.click(screen.getByText("形象绘制失败"));
  expect(
    view.container.querySelector(".welcome-character img"),
  ).toHaveAttribute("src", "/ally-logo.svg");
  loading.theme = "dark";
  view.rerender(<WelcomeBrand intro={false} />);
  expect(
    view.container.querySelector(".welcome-character img"),
  ).toHaveAttribute("src", "/ally-logo-dark.svg");
});
it("等待 IP 时已预取字标代码，但不提前展示或播放字标", async () => {
  render(<WelcomeBrand />);
  await waitFor(() => expect(loading.logo).toHaveBeenCalledOnce());
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
});
it("IP 落位后才开始字标，字标结束后再展开右侧引导", async () => {
  const done = vi.fn();
  const view = render(
    <WelcomeBrand onIntroComplete={done}>
      <h2>智能体设置</h2>
    </WelcomeBrand>,
  );
  expect(view.container.querySelector(".welcome-eyes")).toBeNull();
  const character = view.container.querySelector(".welcome-character");
  expect(character).toHaveAttribute("data-ready", "false");
  fireEvent.click(screen.getByText("形象已绘制"));
  expect(character).toHaveAttribute("data-ready", "true");
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
  // 子节点的动画结束不能提前放行字标。
  fireEvent(
    screen.getByText("形象已绘制"),
    new Event("webkitAnimationEnd", { bubbles: true }),
  );
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
  if (!character) throw new Error("缺少完整形象容器");
  fireEvent(character, new Event("webkitAnimationEnd", { bubbles: true }));
  fireEvent.click(await screen.findByText("字标入场结束"));
  expect(
    await screen.findByRole("heading", { name: "智能体设置" }),
  ).toBeVisible();
  expect(done).toHaveBeenCalledOnce();
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
  // 子节点动画冒泡不代表布局已经稳定。
  fireEvent(
    screen.getByRole("heading", { name: "智能体设置" }),
    new Event("webkitAnimationEnd", { bubbles: true }),
  );
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
  const guide = view.container.querySelector(".welcome-guide");
  if (!guide) throw new Error("缺少引导容器");
  fireEvent(guide, new Event("webkitAnimationEnd", { bubbles: true }));
  const replay = await screen.findByText("字标入场结束");
  expect(replay).toHaveAttribute("data-intro", "true");
  fireEvent.click(replay);
  expect(done).toHaveBeenCalledOnce();
  view.rerender(
    <WelcomeBrand onIntroComplete={done}>
      <h2>模型设置</h2>
    </WelcomeBrand>,
  );
  expect(screen.getByRole("heading", { name: "模型设置" })).toBeVisible();
  expect(screen.getByText("字标入场结束")).toBe(replay);
  expect(done).toHaveBeenCalledOnce();
});
it("直接进入引导同样等右侧入场后弹出字标，重新展开会再播放", async () => {
  const view = render(
    <WelcomeBrand intro={false}>
      <h2>智能体设置</h2>
    </WelcomeBrand>,
  );
  fireEvent.click(screen.getByText("形象已绘制"));
  expect(view.container.querySelector(".welcome-eyes")).toBeNull();
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
  const guide = view.container.querySelector(".welcome-guide");
  if (!guide) throw new Error("缺少引导容器");
  fireEvent(guide, new Event("webkitAnimationEnd", { bubbles: true }));
  expect(await screen.findByText("字标入场结束")).toHaveAttribute(
    "data-intro",
    "true",
  );
  view.rerender(<WelcomeBrand intro={false} />);
  expect(screen.getByText("字标入场结束")).toHaveAttribute(
    "data-intro",
    "false",
  );
  view.rerender(
    <WelcomeBrand intro={false}>
      <h2>智能体设置</h2>
    </WelcomeBrand>,
  );
  expect(screen.queryByText("字标入场结束")).not.toBeInTheDocument();
});
it("欢迎已播完后的界面立即挂载字标，不再次等待 IP 加载才显示品牌", async () => {
  const view = render(<WelcomeBrand intro={false} />);
  expect(await screen.findByText("字标入场结束")).toBeVisible();
  expect(
    view.container.querySelector(".welcome-character-placeholder"),
  ).not.toBeNull();
  expect(view.container.querySelector(".welcome-wordmark")).toHaveAttribute(
    "data-intro",
    "false",
  );
});
it("减少动态时，完整 IP 就绪后直接接续字标，不等待 CSS 动画", async () => {
  vi.stubGlobal("matchMedia", () => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  const view = render(<WelcomeBrand />);
  expect(view.container.querySelector(".welcome-eyes")).toBeNull();
  fireEvent.click(screen.getByText("形象已绘制"));
  expect(view.container.querySelector(".welcome-eyes")).toBeNull();
  expect(await screen.findByText("字标入场结束")).toBeVisible();
});
it("减少动态或形象失败时不等待不会触发的 CSS 动画", async () => {
  let reduced = true;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", () => ({
    matches: reduced,
    addEventListener: (_: string, listener: () => void) =>
      listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) =>
      listeners.delete(listener),
  }));
  const done = vi.fn();
  render(
    <WelcomeBrand onIntroComplete={done}>
      <h2>智能体设置</h2>
    </WelcomeBrand>,
  );
  fireEvent.click(screen.getByText("形象绘制失败"));
  fireEvent.click(await screen.findByText("字标入场结束"));
  await waitFor(() => expect(done).toHaveBeenCalledOnce());
  expect(screen.getByText("字标入场结束")).toBeVisible();
  act(() => {
    reduced = false;
    listeners.forEach((listener) => listener());
  });
  expect(screen.getByText("字标入场结束")).toBeVisible();
  expect(done).toHaveBeenCalledOnce();
});
