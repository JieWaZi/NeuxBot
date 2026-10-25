import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ElasticMeshOptions } from "../../../../../vendor/elastic-mesh/renderer";
import ElasticLogo from "./elastic-logo";

const runtime = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  theme: "light" as "light" | "dark",
}));
vi.mock("../../../../../vendor/elastic-mesh/renderer", () => ({
  mountElasticMesh: runtime.mount,
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: runtime.theme }),
}));
beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  runtime.theme = "light";
});
function prepare(available = true) {
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  runtime.mount.mockReturnValue({
    available,
    destroy: runtime.destroy,
    update: vi.fn(),
  });
}
it("图形不可用时显示静态字标", async () => {
  prepare(false);
  render(<ElasticLogo />);
  await waitFor(() => expect(screen.getByText("NeuxBot")).toBeVisible());
});
it("不重播欢迎时，图形尚未绘制也保留完整静态字标，首帧后无空白替换", async () => {
  prepare();
  const view = render(<ElasticLogo intro={false} />);
  expect(screen.getByText("NeuxBot")).toBeVisible();
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledOnce());
  const options = runtime.mount.mock.calls[0]?.[1] as ElasticMeshOptions;
  act(() => options.onReady?.());
  expect(view.container.querySelector("canvas")).toBeVisible();
  expect(screen.getByText("NeuxBot")).not.toBeVisible();
});
it("首次绘制完成后才显示 Canvas，上下文丢失释放资源并恢复静态字标", async () => {
  prepare();
  const view = render(<ElasticLogo />);
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledOnce());
  const options = runtime.mount.mock.calls[0]?.[1] as ElasticMeshOptions;
  const canvas = view.container.querySelector("canvas");
  expect(canvas).not.toBeVisible();
  act(() => options.onReady?.());
  expect(canvas).toBeVisible();
  if (!canvas) throw new Error("缺少品牌 Canvas");
  fireEvent(canvas, new Event("webglcontextlost", { cancelable: true }));
  expect(runtime.destroy).toHaveBeenCalledOnce();
  expect(screen.getByText("NeuxBot")).toBeVisible();
  view.unmount();
  expect(runtime.destroy).toHaveBeenCalledOnce();
});
it("主题切换和卸载释放旧渲染器", async () => {
  prepare();
  const view = render(<ElasticLogo />);
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledOnce());
  runtime.theme = "dark";
  view.rerender(<ElasticLogo />);
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledTimes(2));
  expect(runtime.destroy).toHaveBeenCalledOnce();
  view.unmount();
  expect(runtime.destroy).toHaveBeenCalledTimes(2);
});

it("首次绘制不表示入场结束，真实完成后只通知一次", async () => {
  prepare();
  const complete = vi.fn();
  render(<ElasticLogo onIntroComplete={complete} />);
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledOnce());
  const options = runtime.mount.mock.calls[0]?.[1] as ElasticMeshOptions;
  act(() => options.onReady?.());
  expect(complete).not.toHaveBeenCalled();
  act(() => {
    options.onIntroComplete?.();
    options.onIntroComplete?.();
  });
  expect(complete).toHaveBeenCalledOnce();
});
it("图形不可用时完成静态降级，不永久阻塞启动", async () => {
  prepare(false);
  const complete = vi.fn();
  render(<ElasticLogo onIntroComplete={complete} />);
  await waitFor(() => expect(complete).toHaveBeenCalledOnce());
  expect(screen.getByText("NeuxBot")).toBeVisible();
});

it("欢迎已完成后服务才就绪，同一字标更新为非入场状态不销毁已绘制画布", async () => {
  prepare();
  const view = render(<ElasticLogo intro />);
  await waitFor(() => expect(runtime.mount).toHaveBeenCalledOnce());
  const options = runtime.mount.mock.calls[0]?.[1] as ElasticMeshOptions;
  act(() => {
    options.onReady?.();
    options.onIntroComplete?.();
  });
  const canvas = view.container.querySelector("canvas");
  expect(canvas).toBeVisible();
  view.rerender(<ElasticLogo intro={false} />);
  await act(async () => {});
  expect(runtime.mount).toHaveBeenCalledOnce();
  expect(runtime.destroy).not.toHaveBeenCalled();
  expect(view.container.querySelector("canvas")).toBe(canvas);
  expect(canvas).toBeVisible();
});
