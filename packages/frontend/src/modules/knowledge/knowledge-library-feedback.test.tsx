import { act, render, renderHook, screen } from "@testing-library/react";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { CancelledError } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { LibraryList } from "./knowledge-library";
import { emptyLibrary } from "./knowledge-model";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";

afterEach(() => {
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});

it("刷新失败时保留缓存和搜索，并全局报告错误", async () => {
  const notices = renderHook(useErrorNotices);
  const retry = vi.fn();
  render(
    <TooltipProvider>
      <LibraryList
        libraries={[emptyLibrary("cached", "已缓存知识库", "general")]}
        open={vi.fn()}
        create={vi.fn()}
        state="error"
        error={new Error("refresh failed")}
        onRetry={retry}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("heading", { name: "已缓存知识库" })).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(notices.result.current).toHaveLength(1);
  expect(screen.getByRole("searchbox")).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
});

it("首次读取失败时保留列表壳，恢复前不展示空库", async () => {
  const notices = renderHook(useErrorNotices);
  const retry = vi.fn();
  const props = { open: vi.fn(), create: vi.fn(), onRetry: retry };
  const view = render(
    <TooltipProvider>
      <LibraryList
        {...props}
        libraries={[]}
        state="error"
        error={new Error("offline")}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("heading", { name: "知识库" })).toBeVisible();
  expect(screen.getByRole("searchbox")).toBeEnabled();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(notices.result.current).toHaveLength(1);
  expect(screen.queryByText("没有找到知识库")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  view.rerender(
    <TooltipProvider>
      <LibraryList
        {...props}
        libraries={[emptyLibrary("restored", "恢复的知识库", "general")]}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("heading", { name: "恢复的知识库" })).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it.each([false, true])(
  "内容读取失败全局报告，已有数据为 %s 时保留内容区",
  async (hasData) => {
    const notices = renderHook(useErrorNotices);
    const retry = vi.fn();
    const view = render(
      <KnowledgeReadFeedback
        hasData={hasData}
        state={{
          pending: false,
          error: new Error("failed"),
          stale: hasData,
          retry,
        }}
      />,
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(notices.result.current).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: "重试" }),
    ).not.toBeInTheDocument();
    view.rerender(
      <KnowledgeReadFeedback
        hasData={hasData}
        state={{ pending: false, error: undefined, retry }}
      />,
    );
    expect(view.container).toBeEmptyDOMElement();
  },
);

it.each([new DOMException("aborted", "AbortError"), new CancelledError()])(
  "取消读取不作为失败展示",
  (error) => {
    const view = render(
      <KnowledgeReadFeedback
        state={{ pending: false, error, retry: vi.fn() }}
      />,
    );
    expect(view.container).toBeEmptyDOMElement();
  },
);

it("后台刷新不插入提示或改变内容区域结构", () => {
  const view = render(
    <KnowledgeReadFeedback
      hasData
      state={{
        pending: false,
        error: undefined,
        refreshing: true,
        retry: vi.fn(),
      }}
    />,
  );
  expect(view.container).toBeEmptyDOMElement();
});
