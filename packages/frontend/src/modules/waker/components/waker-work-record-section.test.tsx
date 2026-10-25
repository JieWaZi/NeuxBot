import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerWorkRecordSection } from "./waker-work-record-section";

vi.mock("../api/waker-queries", () => ({
  getWakerWorkRecordOptions: () => ({
    queryKey: ["work-record", "waker-1"],
    queryFn: async () => ({
      activeChats: 1,
      totalChats: 2,
      totalAutomaticTasks: 3,
      dailyChats: [],
    }),
  }),
}));

describe("WakerWorkRecordSection", () => {
  it("展示工作记录接口返回的自动任务总数", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <WakerWorkRecordSection wakerId="waker-1" />
      </QueryClientProvider>,
    );

    const taskLabel = await screen.findByText("自动任务");
    const taskStat = taskLabel.closest("div.min-w-0");
    expect(taskStat).toBeInstanceOf(HTMLElement);
    if (!(taskStat instanceof HTMLElement)) return;
    expect(within(taskStat).getByText("3")).toBeInTheDocument();
  });
});
