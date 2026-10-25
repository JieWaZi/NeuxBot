import { act, render, renderHook, screen } from "@testing-library/react";
import { CancelledError } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import {
  dismissErrorNotice,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";

const scene = vi.hoisted(() => ({ active: true }));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => scene.active,
}));
afterEach(() => {
  scene.active = true;
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});

it("读取失败只投递全局错误，同一错误的多个内容消费者不重复投递，恢复清除原提示", () => {
  const notices = renderHook(useErrorNotices);
  const error = new ApiError("远端目录不可达", "KnowledgeRemoteUnreachable");
  const state = { pending: false, error, retry: vi.fn() };
  const view = render(
    <>
      <KnowledgeReadFeedback state={state} />
      <KnowledgeReadFeedback state={state} />
    </>,
  );
  expect(view.container).toBeEmptyDOMElement();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  expect(notices.result.current).toHaveLength(1);
  expect(notices.result.current[0]).toMatchObject({
    scope: "global",
    message: "远端目录不可达",
  });
  const version = notices.result.current[0]?.version;
  view.rerender(<KnowledgeReadFeedback state={{ ...state, retry: vi.fn() }} />);
  expect(notices.result.current[0]?.version).toBe(version);
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: undefined, retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(0);
});

it("隐藏 Scene 与取消读取不投递错误，激活后展示当前真实错误", () => {
  scene.active = false;
  const notices = renderHook(useErrorNotices);
  const error = new ApiError("读取失败", "KnowledgeRemoteUnreachable");
  const view = render(
    <KnowledgeReadFeedback state={{ pending: false, error, retry: vi.fn() }} />,
  );
  expect(notices.result.current).toHaveLength(0);
  scene.active = true;
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: new CancelledError(), retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(0);
  view.rerender(
    <KnowledgeReadFeedback
      state={{
        pending: false,
        error: new DOMException("cancel", "AbortError"),
        retry: vi.fn(),
      }}
    />,
  );
  expect(notices.result.current).toHaveLength(0);
  view.rerender(
    <KnowledgeReadFeedback state={{ pending: false, error, retry: vi.fn() }} />,
  );
  expect(notices.result.current).toHaveLength(1);
});

it("已由 API 报告的错误不重复投递，旧读取恢复不能清除较新失败", () => {
  const notices = renderHook(useErrorNotices);
  const error = new ApiError("读取失败", "KnowledgeRemoteUnreachable");
  error.noticeId = "api:existing";
  act(() =>
    reportErrorNotice({
      id: "api:existing",
      operation: "GET:pages",
      scope: "global",
      message: "读取失败",
    }),
  );
  const version = notices.result.current[0]?.version;
  const view = render(
    <KnowledgeReadFeedback state={{ pending: false, error, retry: vi.fn() }} />,
  );
  expect(notices.result.current[0]?.version).toBe(version);
  act(() =>
    reportErrorNotice({
      id: "api:existing",
      operation: "GET:pages",
      scope: "global",
      message: "新的失败",
    }),
  );
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: undefined, retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(1);
  expect(notices.result.current[0]?.message).toBe("新的失败");
});

it("同一读取连续失败替换原通知，恢复后再次遇到同一错误仍会报告", () => {
  const notices = renderHook(useErrorNotices);
  const first = new ApiError("第一次失败", "KnowledgeRemoteUnreachable");
  const second = new ApiError("第二次失败", "KnowledgeRemoteUnreachable");
  const view = render(
    <KnowledgeReadFeedback
      state={{ pending: false, error: first, retry: vi.fn() }}
    />,
  );
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: second, retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(1);
  expect(notices.result.current[0]?.message).toBe("第二次失败");
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: undefined, retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(0);
  view.rerender(
    <KnowledgeReadFeedback
      state={{ pending: false, error: second, retry: vi.fn() }}
    />,
  );
  expect(notices.result.current).toHaveLength(1);
});
