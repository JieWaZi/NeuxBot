import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import {
  dismissErrorNotice,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import {
  reportKnowledgeError,
  reportKnowledgeReadError,
} from "./knowledge-error-notice";

afterEach(() => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => {
    for (const notice of result.current) dismissErrorNotice(notice.id);
  });
  unmount();
});

it("预检错误进入全局通知，已由 API 报告和取消的错误不重复提示", () => {
  const { result } = renderHook(useErrorNotices);
  const reported = new ApiError("远端失败", "KnowledgeRemoteUnreachable");
  reported.noticeId = "api:reported";
  act(() => {
    reportErrorNotice({
      id: reported.noticeId ?? "",
      scope: "global",
      message: reported.message,
    });
    reportKnowledgeError(
      new ApiError("能力未支持", "KnowledgeUnsupported"),
      "open-settings",
      "无法打开设置",
    );
    reportKnowledgeError(reported, "open-settings", "无法打开设置");
    reportKnowledgeError(
      new DOMException("aborted", "AbortError"),
      "open-settings",
      "无法打开设置",
    );
  });
  expect(result.current.map(({ id, message }) => ({ id, message }))).toEqual([
    { id: "api:reported", message: "远端失败" },
    {
      id: "knowledge:open-settings:KnowledgeUnsupported",
      message: "能力未支持",
    },
  ]);
});

it("读取网络错误沿用瞬态通知身份，重启后不恢复断连提示", async () => {
  act(() =>
    reportKnowledgeReadError(
      new ApiError("服务器连接已断开", "NETWORK"),
      "读取失败",
      "knowledge:read:network-test",
    ),
  );
  const { result } = renderHook(useErrorNotices);
  expect(result.current).toHaveLength(1);
  vi.resetModules();
  const restarted = await import("@/shared/api/error-notices");
  const restored = renderHook(restarted.useErrorNotices);
  expect(restored.result.current).toHaveLength(0);
  restored.unmount();
});
