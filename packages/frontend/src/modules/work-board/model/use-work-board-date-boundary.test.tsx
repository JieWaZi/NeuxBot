import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useWorkBoardDateBoundary } from "./use-work-board-date-boundary";
const summary = ["board", "summary"] as const;
const records = ["board", "records", "current"] as const;
const other = ["board", "records", "other"] as const;
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
function fixture(active: boolean) {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 9, 14, 23, 59, 59, 500));
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
    },
  });
  client.setQueryData(summary, "旧统计");
  client.setQueryData(records, "旧记录");
  client.setQueryData(other, "其他筛选");
  const readSummary = vi.fn(async () => "新统计"),
    readRecords = vi.fn(async () => "新记录"),
    readOther = vi.fn(async () => "其他筛选");
  function Scene({ active }: { active: boolean }) {
    useQuery({ queryKey: summary, queryFn: readSummary, enabled: active });
    useQuery({ queryKey: records, queryFn: readRecords, enabled: active });
    useQuery({ queryKey: other, queryFn: readOther });
    useWorkBoardDateBoundary(active, [summary, records]);
    return null;
  }
  const renderScene = (visible: boolean) => (
    <QueryClientProvider client={client}>
      <Scene active={visible} />
    </QueryClientProvider>
  );
  const view = render(renderScene(active));
  return { ...view, client, readSummary, readRecords, readOther, renderScene };
}
it("仅本地零点失效概览与当前列表，普通时刻没有请求，卸载释放单次定时器", async () => {
  const view = fixture(true);
  await act(() => vi.advanceTimersByTimeAsync(499));
  expect(view.readSummary).not.toHaveBeenCalled();
  expect(view.readRecords).not.toHaveBeenCalled();
  await act(() => vi.advanceTimersByTimeAsync(1));
  expect(view.readSummary).toHaveBeenCalledTimes(1);
  expect(view.readRecords).toHaveBeenCalledTimes(1);
  expect(view.readOther).not.toHaveBeenCalled();
  view.unmount();
  await act(() => vi.advanceTimersByTimeAsync(24 * 60 * 60 * 1000));
  expect(view.readSummary).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
  view.client.clear();
});
it("隐藏时跨日只标记陈旧，恢复可见后对账一次并保留原数直到完成", async () => {
  const view = fixture(false);
  await act(() => vi.advanceTimersByTimeAsync(500));
  expect(view.readSummary).not.toHaveBeenCalled();
  expect(view.readRecords).not.toHaveBeenCalled();
  expect(view.client.getQueryData(summary)).toBe("旧统计");
  expect(view.client.getQueryState(summary)?.isInvalidated).toBe(true);
  view.rerender(view.renderScene(true));
  await act(() => vi.advanceTimersByTimeAsync(0));
  expect(view.readSummary).toHaveBeenCalledTimes(1);
  expect(view.readRecords).toHaveBeenCalledTimes(1);
  expect(view.readOther).not.toHaveBeenCalled();
  view.unmount();
  view.client.clear();
});
it("窗口在后台跨日后恢复可见也只对账一次", async () => {
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("hidden");
  const view = fixture(true);
  await act(() => vi.advanceTimersByTimeAsync(500));
  expect(view.readSummary).not.toHaveBeenCalled();
  expect(view.client.getQueryState(summary)?.isInvalidated).toBe(true);
  visibility.mockReturnValue("visible");
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(view.readSummary).toHaveBeenCalledTimes(1);
  expect(view.readRecords).toHaveBeenCalledTimes(1);
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(view.readSummary).toHaveBeenCalledTimes(1);
  view.unmount();
  view.client.clear();
});
