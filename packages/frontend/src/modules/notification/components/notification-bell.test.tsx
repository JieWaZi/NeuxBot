import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { getNotificationSnapshotQueryKey } from "../api/notification-queries";
import type { NotificationSnapshot } from "../model/notification";
import { NotificationBell } from "./notification-bell";

const originalAnimate = Object.getOwnPropertyDescriptor(
  SVGElement.prototype,
  "animate",
);
const cancel = vi.fn();
const animate = vi.fn<SVGElement["animate"]>(
  () => ({ cancel }) as unknown as Animation,
);
let reducedMotion = false;
beforeEach(() => {
  reducedMotion = false;
  animate.mockClear();
  cancel.mockClear();
  Object.defineProperty(SVGElement.prototype, "animate", {
    configurable: true,
    value: animate,
  });
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: reducedMotion,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  document.documentElement.style.setProperty("--duration-slow", "420ms");
});
afterEach(() => {
  if (originalAnimate) {
    Object.defineProperty(SVGElement.prototype, "animate", originalAnimate);
  } else {
    Reflect.deleteProperty(SVGElement.prototype, "animate");
  }
  document.documentElement.style.removeProperty("--duration-slow");
  vi.unstubAllGlobals();
});

function mount(count = 2, active = false) {
  const queryClient = new QueryClient();
  const snapshot: NotificationSnapshot = {
    epoch: "a".repeat(32),
    revision: 1,
    synced: true,
    actionCount: count,
    unreadResultCount: 0,
    unreadFailureCount: 0,
    totalCount: count,
    actionPreview: [],
    unreadPreview: [],
    generatedAt: 100,
  };
  queryClient.setQueryData(getNotificationSnapshotQueryKey(), snapshot);
  const onOpen = vi.fn();
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <NotificationBell active={active} connected onOpen={onOpen} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, onOpen, queryClient };
}

it("点击和新通知有反馈，重复点击及卸载释放动画，初始快照不摇铃", () => {
  const view = mount();
  const bell = screen.getByRole("button", { name: "通知 2 项，打开任务看板" });
  expect(animate).not.toHaveBeenCalled();
  fireEvent.click(bell);
  expect(view.onOpen).toHaveBeenCalledTimes(1);
  expect(animate).toHaveBeenCalledTimes(2);
  expect(animate.mock.calls[0]?.[1]).toMatchObject({ duration: 420 });
  fireEvent.click(bell);
  expect(view.onOpen).toHaveBeenCalledTimes(2);
  expect(cancel).toHaveBeenCalledTimes(2);
  act(() => window.dispatchEvent(new Event("ally:notification-arrived")));
  expect(animate).toHaveBeenCalledTimes(6);
  expect(
    view.queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.totalCount,
  ).toBe(2);
  view.unmount();
  expect(cancel).toHaveBeenCalledTimes(6);
  act(() => window.dispatchEvent(new Event("ally:notification-arrived")));
  expect(animate).toHaveBeenCalledTimes(6);
});

it("减少动态时关闭摇铃与声波，零通知仍能通过键盘进入看板", async () => {
  reducedMotion = true;
  const user = userEvent.setup();
  const { onOpen } = mount(0);
  const bell = screen.getByRole("button", { name: "通知 0 项，打开任务看板" });
  await user.tab();
  expect(bell).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(onOpen).toHaveBeenCalledTimes(1);
  expect(animate).not.toHaveBeenCalled();
  expect(screen.queryByText("0")).toBeNull();
  expect(bell).not.toHaveAttribute("aria-pressed");
});

it("看板激活使用当前页导航语义，保留准确计数与99+视觉上限", () => {
  mount(120, true);
  expect(
    screen.getByRole("button", { name: "通知 120 项，打开任务看板" }),
  ).toHaveAttribute("aria-current", "page");
  expect(screen.getByText("99+")).toBeInTheDocument();
  expect(screen.queryByText("看板")).not.toBeInTheDocument();
});
