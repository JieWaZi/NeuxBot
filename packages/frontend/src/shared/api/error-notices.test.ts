import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./errors";
import { i18n } from "@/shared/i18n";
import {
  apiErrorScope,
  clearErrorNoticeActions,
  dismissErrorNotice,
  dismissErrorNotices,
  reportChatError,
  reportErrorNotice,
  runErrorNoticeAction,
  useErrorNotices,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
  bindErrorNoticeRetry,
} from "./error-notices";

afterEach(async () => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => {
    for (const notice of result.current) dismissErrorNotice(notice.id);
  });
  unmount();
  vi.useRealTimers();
  await i18n.changeLanguage("zh-CN");
});

describe("持久错误通知", () => {
  it("页面释放恢复动作不改变失败版本，晚到成功仍能清除原错误", () => {
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportErrorNotice(
        {
          messageSource: "public",
          id: "leave-retry",
          scope: "chat:one",
          operation: "send:one",
          message: "发送失败",
          action: "retry",
        },
        vi.fn(),
      ),
    );
    const recovery = captureErrorNoticeRecovery("send:one");
    act(() => {
      clearErrorNoticeActions("chat:one");
      resolveErrorNoticeRecovery(recovery);
    });
    expect(result.current).toHaveLength(0);
  });
  it("恢复只移除开始操作时的对应错误，保留其他操作与其他会话", () => {
    const { result } = renderHook(useErrorNotices);
    act(() => {
      reportErrorNotice({
        messageSource: "public",
        id: "send-one",
        scope: "chat:one",
        operation: "send:one",
        message: "发送失败",
      });
      reportErrorNotice({
        messageSource: "public",
        id: "history-one",
        scope: "chat:one",
        operation: "history:one",
        message: "查询失败",
      });
      reportErrorNotice({
        messageSource: "public",
        id: "send-two",
        scope: "chat:two",
        operation: "send:two",
        message: "发送失败",
      });
    });
    const recovery = captureErrorNoticeRecovery("send:one");
    act(() => resolveErrorNoticeRecovery(recovery));
    expect(result.current.map((notice) => notice.id)).toEqual([
      "history-one",
      "send-two",
    ]);
  });

  it("迟到成功不能清除同一操作后来发生的同身份失败", () => {
    const { result } = renderHook(useErrorNotices);
    const failure = {
      id: "read",
      scope: "chat:one",
      operation: "read:one",
      message: "读取失败",
    };
    act(() => reportErrorNotice({ ...failure, messageSource: "public" }));
    const recovery = captureErrorNoticeRecovery(failure.operation);
    act(() => {
      reportErrorNotice({ ...failure, messageSource: "public" });
      resolveErrorNoticeRecovery(recovery);
    });
    expect(result.current).toHaveLength(1);
  });

  it("查询重试失败保留提示，真正成功后移除该查询错误", async () => {
    const { result } = renderHook(useErrorNotices);
    const retry = vi
      .fn()
      .mockRejectedValueOnce(new Error("仍然失败"))
      .mockResolvedValueOnce({});
    const error = new ApiError("查询失败");
    error.noticeId = "query-retry";
    act(() => {
      reportErrorNotice({
        messageSource: "public",
        id: "query-retry",
        scope: "chat:one",
        operation: "query:one",
        message: "查询失败",
      });
      bindErrorNoticeRetry(error, retry);
    });
    await act(async () => {
      await expect(runErrorNoticeAction("query-retry")).rejects.toThrow(
        "仍然失败",
      );
    });
    expect(result.current).toHaveLength(1);
    await act(async () => {
      await runErrorNoticeAction("query-retry");
    });
    expect(result.current).toHaveLength(0);
  });

  it("启动时不恢复之前持久化的后端网络断连提示", async () => {
    localStorage.setItem(
      "ally:error-notices",
      JSON.stringify([
        {
          id: "api:GET:/api/v1/chats/c1:NETWORK",
          scope: "chat:c1",
          operation: "api:GET:/api/v1/chats/c1",
          version: "network-failure",
          message: "Failed to fetch",
          createdAt: 1,
        },
        {
          id: "api:GET:/api/v1/chats/c1:ReadFailed",
          scope: "chat:c1",
          operation: "api:GET:/api/v1/chats/c1",
          version: "read-failure",
          message: "读取失败",
          createdAt: 2,
        },
      ]),
    );
    vi.resetModules();
    const restored = await import("./error-notices");
    const { result, unmount } = renderHook(restored.useErrorNotices);
    expect(result.current.map((notice) => notice.message)).toEqual([
      "本地服务返回了无法识别的错误",
    ]);
    unmount();
  });

  it("重载不恢复旧发送回执提示，其他持久错误仍可恢复", async () => {
    const notice = {
      id: "send:user-optimistic",
      scope: "chat-one",
      operation: "send:chat-one",
      version: "unconfirmed-send",
      messageSource: "public",
      message:
        "发送结果未知。输入已保留，历史正在重新同步；请核对后再决定是否发送新消息。",
      createdAt: 1,
    };
    localStorage.setItem(
      "ally:error-notices",
      JSON.stringify([
        notice,
        {
          ...notice,
          id: "permission",
          operation: "access:chat-one",
          message: "授权失败",
        },
      ]),
    );
    vi.resetModules();
    const restored = await import("./error-notices");
    const { result, unmount } = renderHook(restored.useErrorNotices);
    expect(result.current.map((item) => item.id)).toEqual(["permission"]);
    unmount();
  });

  it("新上报的底层网络异常使用客户可读文案", () => {
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "network",
        scope: "chat:one",
        message: "Failed to fetch",
      }),
    );
    expect(result.current[0]?.message).toBe(
      "暂时无法连接本地服务，请检查连接状态。",
    );
    expect(
      JSON.parse(localStorage.getItem("ally:error-notices") ?? "[]"),
    ).toEqual([
      expect.objectContaining({
        message: "",
        messageKey: "common:serviceConnectionFailed",
      }),
    ]);
  });

  it("切换语言后重新翻译已存在的网络通知", async () => {
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "network-locale",
        scope: "global",
        message: "Failed to fetch",
      }),
    );
    await act(async () => {
      await i18n.changeLanguage("en-US");
    });
    expect(result.current[0]?.message).toBe(
      "Cannot connect to the local service right now. Check the connection.",
    );
  });

  it("发送新提示和时间流逝不会清除旧错误，只有手动关闭才移除", () => {
    vi.useFakeTimers();
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "one",
        scope: "chat:one",
        message: "错误一",
      }),
    );
    act(() => {
      vi.advanceTimersByTime(120_000);
      reportErrorNotice({
        messageSource: "public",
        id: "two",
        scope: "global",
        message: "错误二",
      });
    });
    expect(result.current.map((notice) => notice.id)).toEqual(["one", "two"]);
    expect(
      JSON.parse(localStorage.getItem("ally:error-notices") ?? "[]"),
    ).toHaveLength(2);
    act(() => dismissErrorNotice("one"));
    expect(result.current.map((notice) => notice.id)).toEqual(["two"]);
  });

  it("同一次 API 失败只展示一条，重复查询去重，关闭后新失败可以再次提示", () => {
    const { result } = renderHook(useErrorNotices);
    const notice = { id: "api:get", scope: "chat:one", message: "读取失败" };
    const error = new ApiError("读取失败");
    error.noticeId = notice.id;
    act(() => {
      reportErrorNotice({ ...notice, messageSource: "public" });
      reportErrorNotice({ ...notice, messageSource: "public" });
      reportChatError(error, "chat:one", "失败");
    });
    expect(result.current).toHaveLength(1);
    act(() => {
      dismissErrorNotice(notice.id);
      reportErrorNotice({ ...notice, messageSource: "public" });
    });
    expect(result.current).toHaveLength(1);
  });

  it("相同聊天操作错误合并，手动关闭后再次失败仍可提示", () => {
    const { result } = renderHook(useErrorNotices);
    act(() => {
      reportChatError(new Error("failed"), "chat:one", "操作失败");
      reportChatError(new Error("failed"), "chat:one", "操作失败");
    });
    expect(result.current).toHaveLength(1);
    act(() => {
      dismissErrorNotices(result.current.map((notice) => notice.id));
      reportChatError(new Error("failed"), "chat:one", "操作失败");
    });
    expect(result.current).toHaveLength(1);
  });

  it("用户主动操作遇到后端断连时保留操作反馈但不展示浏览器原文", () => {
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportChatError(
        new ApiError("Failed to fetch", "NETWORK"),
        "chat:one",
        "操作未完成，请重试",
      ),
    );
    expect(result.current).toEqual([
      expect.objectContaining({ message: "操作未完成，请重试" }),
    ]);
  });

  it("页面离开只释放恢复回调，不会删除错误", async () => {
    const action = vi.fn();
    const { result } = renderHook(useErrorNotices);
    act(() =>
      reportErrorNotice(
        {
          messageSource: "public",
          id: "retry",
          scope: "chat:one",
          message: "限流",
          action: "retry",
        },
        action,
      ),
    );
    await runErrorNoticeAction("retry");
    expect(action).toHaveBeenCalledTimes(1);
    act(() => clearErrorNoticeActions("chat:one"));
    await runErrorNoticeAction("retry");
    expect(action).toHaveBeenCalledTimes(1);
    expect(result.current[0]).toMatchObject({
      message: "限流",
      action: undefined,
    });
  });

  it.each([
    ["/api/v1/chats/c1/messages", "chat:c1"],
    ["/api/v1/chats/c1/approvals/a1", "chat:c1"],
    ["/api/v1/channels/c1/threads/t1/tasks", "team:t1"],
    ["/api/v1/runtimes/codex", "global"],
  ])("%s 使用 %s 展示范围", (path, scope) => {
    expect(apiErrorScope(path)).toBe(scope);
  });
});

it("通知保留应用身份按当前语言重绘，并保留显式公开原因", async () => {
  const { result } = renderHook(useErrorNotices);
  act(() =>
    reportErrorNotice({
      id: "localized-contract",
      scope: "app",
      message: "server fixed language",
      errorCode: "RuntimeWorkspaceScopeUnsupported",
      errorParams: { MaxDirectories: 2 },
      messageSource: "localized",
    }),
  );
  expect(result.current[0]?.message).toBe(
    "当前智能体最多支持 2 个项目目录，请调整工作范围",
  );
  await act(() => i18n.changeLanguage("en-US"));
  expect(result.current[0]?.message).toBe(
    "the selected agent supports up to 2 project directories; adjust the workspace scope",
  );
  act(() =>
    reportErrorNotice({
      id: "public-contract",
      scope: "app",
      message: "Native quota exhausted",
      errorCode: "RuntimeQuotaExhausted",
      messageSource: "public",
    }),
  );
  expect(result.current[1]?.message).toBe("Native quota exhausted");
});

it("旧持久通知缺少来源时拒绝正文，已声明的本地提示仍可恢复", async () => {
  localStorage.setItem(
    "ally:error-notices",
    JSON.stringify([
      {
        id: "api:GET:/api/v1/chats/c1:RuntimeEventQueueOverflow",
        scope: "chat:c1",
        operation: "api:GET:/api/v1/chats/c1",
        version: "legacy",
        createdAt: 1,
        message: "[RuntimeEventQueueOverflow][50000000050]",
      },
      {
        id: "untyped-local",
        scope: "global",
        operation: "local",
        version: "legacy-local",
        createdAt: 2,
        message: "private diagnostic without an application identity",
      },
      {
        id: "declared-local",
        scope: "global",
        operation: "local",
        version: "local",
        createdAt: 3,
        message: "已知本地提示",
        messageSource: "public",
      },
    ]),
  );
  vi.resetModules();
  const restored = await import("./error-notices");
  const { result, unmount } = renderHook(restored.useErrorNotices);
  expect(result.current.map((notice) => notice.message)).toEqual([
    "本地服务返回了无法识别的错误",
    "本地服务返回了无法识别的错误",
    "已知本地提示",
  ]);
  act(() =>
    restored.dismissErrorNotices(result.current.map((notice) => notice.id)),
  );
  unmount();
});

it("会话提示即时可见但重载后无事件不会恢复", async () => {
  const view = renderHook(useErrorNotices);
  act(() =>
    reportErrorNotice({
      id: "background-failure",
      scope: "global",
      lifetime: "session",
      message: "后台同步失败",
      messageSource: "public",
    }),
  );
  expect(view.result.current).toHaveLength(1);
  expect(
    JSON.parse(localStorage.getItem("ally:error-notices") ?? "[]"),
  ).toEqual([]);
  const sessionNotice = view.result.current[0];
  view.unmount();
  // 即使上一版本或宿主恢复错误地保留了会话提示，也不能在新Renderer重放。
  localStorage.setItem("ally:error-notices", JSON.stringify([sessionNotice]));
  vi.resetModules();
  const restored = await import("./error-notices");
  const recovered = renderHook(restored.useErrorNotices);
  expect(recovered.result.current).toHaveLength(0);
  recovered.unmount();
});
