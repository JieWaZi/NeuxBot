import {
  dismissErrorNotice,
  runErrorNoticeAction,
  useErrorNotices,
  reportErrorNotice,
} from "@/shared/api/error-notices";
const { retryExecutionMock } = vi.hoisted(() => ({
  retryExecutionMock: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  retryChatExecution: retryExecutionMock,
}));
import type { AppendMessage, ExternalStoreAdapter } from "@assistant-ui/react";
import { act, render, renderHook, waitFor } from "@testing-library/react";
import { createElement, useLayoutEffect, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { chatMessagesKey, chatMessageSource } from "../api/chat-queries";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatRuntime as useProductionChatRuntime } from "./use-chat-runtime";
import { ChatQueueProvider, useChatQueueStore } from "./chat-queue-store";
import { classifyChatSendFailure } from "./chat-send-failure";
import { conversationKeyOf } from "../model/conversation-identity";
import { ApiError } from "@/shared/api/errors";
import {
  clearStableCreateRequest,
  withStableCreateRequest,
} from "@/shared/lib/create-request-idempotency";
import type { GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as MessageResult } from "@/shared/api/generated/types.gen";
import type { ChatEvent } from "@/shared/api/events";

type TestRuntimeAdapter = ExternalStoreAdapter & {
  messages?: readonly unknown[];
  convertMessage?: (message: unknown, index: number) => unknown;
};

const {
  projectedMessages,
  runtimeAdapter,
  subscriptionOptions,
  subscribeChatMock,
} = vi.hoisted(() => ({
  projectedMessages: { current: [] as unknown[] },
  runtimeAdapter: { current: undefined as TestRuntimeAdapter | undefined },
  subscriptionOptions: {
    current: undefined as
      | {
          onEvent: (event: ChatEvent) => void;
          onReconnect?: () => void;
          onGap?: (expected: number, received: number) => void;
          onState?: (
            state:
              "connecting" | "open" | "reconnecting" | "unavailable" | "closed",
          ) => void;
          reconcileCursor?: (
            signal: AbortSignal,
          ) => Promise<number | undefined>;
          onUnavailable?: () => void;
        }
      | undefined,
  },
  subscribeChatMock: vi.fn(
    (
      _chatId: string,
      options: {
        onEvent: (event: ChatEvent) => void;
        onReconnect?: () => void;
        onUnavailable?: () => void;
        after?: number;
      },
    ) => {
      subscriptionOptions.current = options;
      return vi.fn();
    },
  ),
}));

vi.mock("@assistant-ui/react", () => ({
  useExternalStoreRuntime: (adapter: TestRuntimeAdapter) => {
    runtimeAdapter.current = adapter;
    projectedMessages.current = (adapter.messages ?? []).map(
      (message, index) => adapter.convertMessage?.(message, index) ?? message,
    );
    return {};
  },
}));

vi.mock("@/shared/api/events", () => ({
  subscribeChat: subscribeChatMock,
}));

const queuedMessage: AppendMessage = {
  role: "user",
  content: [{ type: "text", text: "排队消息" }],
  attachments: [],
  createdAt: new Date(0),
  metadata: { custom: {} },
  parentId: null,
  sourceId: null,
  runConfig: undefined,
};

type RuntimeOptions = Omit<
  Parameters<typeof useProductionChatRuntime>[0],
  "prepareSubmission" | "messageSource"
> & {
  messages: Parameters<
    typeof useProductionChatRuntime
  >[0]["messageSource"]["snapshot"]["messages"];
  latestSequence: number;
  isHistoryReady: boolean;
  send: (
    text: string,
    chatId?: string,
    paths?: string[],
    selection?: Parameters<
      Parameters<typeof useProductionChatRuntime>[0]["prepareSubmission"]
    >[3],
  ) => Promise<MessageResult>;
};
function useChatRuntime(options: RuntimeOptions) {
  return useProductionChatRuntime({
    ...options,
    messageSource: chatMessageSource(queryClient, options.chatId, {
      messages: options.messages,
      latestSequence: options.latestSequence,
      ready: options.isHistoryReady,
    }),
    prepareSubmission: (...args) => ({
      submissionId: `input:${options.queueKey}`,
      accept: () => options.send(...args),
    }),
  });
}

function runtimeOptions(queueKey: string): RuntimeOptions {
  return {
    conversationKey: conversationKeyOf(`scope:${queueKey}`, queueKey),
    queueKey,
    chatId: queueKey,
    messages: [],
    latestSequence: 1,
    activeExecutionId: "execution-1" as string | undefined,
    isHistoryReady: true,
    isLoading: false,
    isSendDisabled: false,
    attachmentOnlyText: "请查看附件。",
    incompleteErrorText: "执行未完成",
    selection: {
      runtimeId: "codex",
      model: "gpt-5",
      reasoning: "high",
      permissionLevel: "auto",
    },
    send: vi.fn(),
    cancel: vi.fn(),
    onStarted: vi.fn(),
    onTerminal: vi.fn(),
  };
}

let queryClient: QueryClient;

function TestProviders({ children }: { children: ReactNode }) {
  return createElement(
    QueryClientProvider,
    { client: queryClient },
    createElement(ChatQueueProvider, null, children),
  );
}

function RuntimeHarness({ options }: { options: RuntimeOptions }) {
  useChatRuntime(options);
  return null;
}

function RuntimeRemountHarness({
  hookKey,
  options,
}: {
  hookKey: string;
  options: RuntimeOptions;
}) {
  return createElement(
    TestProviders,
    null,
    createElement(RuntimeHarness, { key: hookKey, options }),
  );
}

describe("useChatRuntime queue", () => {
  const wrapper = TestProviders;
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    runtimeAdapter.current = undefined;
    projectedMessages.current = [];
    subscriptionOptions.current = undefined;
    subscribeChatMock.mockClear();
    retryExecutionMock.mockReset();
    const notices = renderHook(useErrorNotices);
    act(() => {
      for (const notice of notices.result.current)
        dismissErrorNotice(notice.id);
    });
    notices.unmount();
  });

  it("隐藏会话暂停实时历史消费，重新可见时恢复原会话订阅", () => {
    const options = runtimeOptions("chat-hidden");
    const { rerender } = renderHook(
      ({ active }) => useChatRuntime({ ...options, queueActive: active }),
      { wrapper, initialProps: { active: false } },
    );
    expect(subscribeChatMock).not.toHaveBeenCalled();
    rerender({ active: true });
    expect(subscribeChatMock).toHaveBeenCalledTimes(1);
    const close = subscribeChatMock.mock.results[0]?.value;
    rerender({ active: false });
    expect(close).toHaveBeenCalledTimes(1);
    rerender({ active: true });
    expect(subscribeChatMock).toHaveBeenCalledTimes(2);
    expect(subscribeChatMock.mock.calls[1]?.[0]).toBe("chat-hidden");
  });

  it("事件到达后立即隐藏不会推进尚未投影的历史游标", () => {
    vi.useFakeTimers();
    const options = runtimeOptions("chat-batch-hidden");
    const { rerender, unmount } = renderHook(
      ({ active }) => useChatRuntime({ ...options, queueActive: active }),
      { wrapper, initialProps: { active: true } },
    );
    try {
      act(() => {
        subscriptionOptions.current?.onEvent({
          sequence: 2,
          type: "runtime.event",
          message: {
            id: "execution-1",
            executionId: "execution-1",
            role: "assistant",
            status: "running",
            content: "未提交批次",
            revision: 2,
          },
        });
      });
      rerender({ active: false });
      rerender({ active: true });
      expect(subscribeChatMock.mock.calls[1]?.[1].after).toBe(1);
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("消息重试绑定失败执行，连点只调用一次接口，结果触发权威刷新", async () => {
    let resolve!: (value: { data: MessageResult }) => void;
    retryExecutionMock.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const options = {
      ...runtimeOptions("chat-retry"),
      activeExecutionId: undefined,
    };
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });
    let pending!: Promise<void>;
    act(() => {
      pending = result.current.retryExecution("failed-1");
      void result.current.retryExecution("failed-1");
    });
    expect(retryExecutionMock).toHaveBeenCalledTimes(1);
    expect(retryExecutionMock).toHaveBeenCalledWith({
      path: { chatId: "chat-retry", executionId: "failed-1" },
      throwOnError: true,
    });
    await act(async () => {
      resolve({
        data: {
          chat: { id: "chat-retry" },
          execution: { id: "child-1", status: "running" },
        },
      });
      await pending;
    });
    expect(options.onTerminal).toHaveBeenCalled();
    expect(result.current.isRunning).toBe(true);
  });

  it("响应未知时只检查状态，不重新发送；后续发送成功清除发送错误", async () => {
    const invalidations = vi.spyOn(queryClient, "invalidateQueries");
    const send = vi
      .fn()
      .mockRejectedValueOnce(new ApiError("Failed to fetch", "NETWORK"))
      .mockResolvedValue({
        chat: { id: "chat-unknown" },
        execution: { id: "new", status: "running" },
      });
    const options = {
      ...runtimeOptions("chat-unknown"),
      activeExecutionId: undefined,
      send,
    };
    const { result } = renderHook(
      () => ({ chat: useChatRuntime(options), notices: useErrorNotices() }),
      { wrapper },
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    await waitFor(() => expect(invalidations).toHaveBeenCalledTimes(1));
    const notice = result.current.notices[0];
    expect(notice).toMatchObject({
      message: expect.stringContaining("发送结果未知"),
      scope: "chat-unknown",
      action: "check",
    });
    await act(async () => {
      if (!notice) throw new Error("发送错误提示缺失");
      await runErrorNoticeAction(notice.id);
    });
    expect(send).toHaveBeenCalledTimes(1);
    expect(result.current.notices).toHaveLength(1);
    act(() =>
      reportErrorNotice({
        id: "other-query",
        scope: "chat-unknown",
        operation: "query:chat-unknown",
        message: "查询失败",
      }),
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    expect(send).toHaveBeenCalledTimes(2);
    expect(result.current.notices.map((item) => item.id)).toEqual([
      "other-query",
    ]);
  });

  it("流恢复成功只清理连接错误，其他操作错误继续保留", () => {
    const { result } = renderHook(
      () => ({
        chat: useChatRuntime(runtimeOptions("stream-chat")),
        notices: useErrorNotices(),
      }),
      { wrapper },
    );
    act(() => {
      subscriptionOptions.current?.onUnavailable?.();
      reportErrorNotice({
        id: "query-error",
        scope: "chat:stream-chat",
        operation: "query:stream-chat",
        message: "查询失败",
      });
    });
    expect(result.current.notices).toHaveLength(2);
    act(() => subscriptionOptions.current?.onReconnect?.());
    expect(result.current.notices.map((item) => item.id)).toEqual([
      "query-error",
    ]);
  });

  it("Codex 已丢失的会话给出恢复建议，不提供无效的状态检查", async () => {
    const raw = JSON.stringify({
      code: -32603,
      message: "Internal error",
      data: {
        error:
          "resuming codex thread abc: codex app-server error -32600: no rollout found for thread id abc",
      },
    });
    const options = {
      ...runtimeOptions("chat-missing-rollout"),
      send: vi
        .fn()
        .mockRejectedValue(new ApiError(raw, "RuntimeExecutionFailed", 500)),
    };
    const { result } = renderHook(
      () => ({ chat: useChatRuntime(options), notices: useErrorNotices() }),
      { wrapper },
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    expect(result.current.notices[0]).toMatchObject({
      message: "当前智能体无法继续这段对话。确认后可携带近期消息，在这里继续。",
      action: undefined,
    });
    expect(options.onTerminal).not.toHaveBeenCalled();
  });

  it("明确的发送限流可重试，并保留原输入和选择", async () => {
    const send = vi
      .fn()
      .mockRejectedValueOnce(new ApiError("请求过于频繁", "RateLimited", 429))
      .mockResolvedValue({
        chat: { id: "chat-rate" },
        execution: { id: "new", status: "running" },
      });
    const options = {
      ...runtimeOptions("chat-rate"),
      activeExecutionId: undefined,
      send,
    };
    const { result } = renderHook(
      () => ({ chat: useChatRuntime(options), notices: useErrorNotices() }),
      { wrapper },
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    expect(result.current.notices[0]?.action).toBe("retry");
    await act(async () => {
      const notice = result.current.notices[0];
      if (!notice) throw new Error("限流错误提示缺失");
      await runErrorNoticeAction(notice.id);
    });
    expect(send).toHaveBeenCalledTimes(2);
    expect(send.mock.calls[1]).toEqual(send.mock.calls[0]);
    expect(result.current.notices).toHaveLength(0);
  });

  it("收到执行终态后立即结束运行状态", async () => {
    const { result } = renderHook(
      () => useChatRuntime(runtimeOptions("chat-1")),
      { wrapper },
    );
    expect(result.current.isRunning).toBe(true);

    act(() => {
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "execution.completed",
        message: {
          id: "assistant-1",
          role: "assistant",
          executionId: "execution-1",
          content: "",
          status: "completed",
          revision: 2,
        },
      });
    });

    await waitFor(() => expect(result.current.isRunning).toBe(false));
  });

  it("连续持久事件合并原历史缓存，终态只刷新摘要并保留旧分页", async () => {
    const options = runtimeOptions("chat-contiguous");
    const olderPage = {
      resources: [{ id: "old-user", role: "user", content: "更早输入" }],
      hasMore: true,
      nextBeforeSequence: 1,
    };
    queryClient.setQueryData(chatMessagesKey("chat-contiguous"), {
      pages: [
        {
          resources: [],
          latestSequence: 1,
          hasMore: true,
          nextBeforeSequence: 10,
        },
        olderPage,
      ],
      pageParams: [undefined, { beforeSequence: 10 }],
    });
    const { result, rerender } = renderHook(
      ({ executionId, active }) =>
        useChatRuntime({
          ...options,
          activeExecutionId: executionId,
          queueActive: active,
        }),
      { wrapper, initialProps: { executionId: "execution-1", active: true } },
    );
    act(() => {
      for (const event of [
        {
          sequence: 2,
          type: "user.message",
          message: { id: "user-1", role: "user", content: "输入", revision: 2 },
        },
        {
          sequence: 3,
          type: "execution.started",
          message: {
            id: "execution-1",
            role: "assistant",
            executionId: "execution-1",
            status: "running",
            revision: 3,
          },
        },
        {
          sequence: 4,
          type: "runtime.event",
          message: {
            id: "execution-1",
            role: "assistant",
            executionId: "execution-1",
            status: "running",
            revision: 4,
            displayParts: [
              { id: "tool-1", type: "tool", toolStatus: "completed" },
            ],
          },
        },
        {
          sequence: 5,
          type: "assistant.message",
          message: {
            id: "assistant-extra",
            role: "assistant",
            content: "过程消息",
            revision: 5,
          },
        },
        {
          sequence: 6,
          type: "execution.completed",
          message: {
            id: "execution-1",
            role: "assistant",
            executionId: "execution-1",
            status: "completed",
            revision: 6,
            displayParts: [
              { id: "tool-1", type: "tool", toolStatus: "completed" },
              { type: "text", text: "结果" },
            ],
          },
        },
      ] satisfies ChatEvent[])
        subscriptionOptions.current?.onEvent(event);
    });
    expect(result.current.isRunning).toBe(false);
    expect(
      queryClient.getQueryData(chatMessagesKey("chat-contiguous")),
    ).toMatchObject({
      pages: [
        {
          latestSequence: 6,
          hasMore: true,
          nextBeforeSequence: 10,
          resources: [
            { id: "user-1", content: "输入" },
            {
              id: "execution-1",
              status: "completed",
              displayParts: [
                { id: "tool-1", toolStatus: "completed" },
                { text: "结果" },
              ],
            },
            { id: "assistant-extra", content: "过程消息" },
          ],
        },
        olderPage,
      ],
      pageParams: [undefined, { beforeSequence: 10 }],
    });
    expect(options.onTerminal).toHaveBeenCalledExactlyOnceWith();
    rerender({ executionId: "execution-2", active: false });
    rerender({ executionId: "execution-2", active: true });
    expect(subscribeChatMock.mock.calls.at(-1)?.[1].after).toBe(6);
  });

  it("transient 的意外 sequence 也不推进持久历史游标", () => {
    vi.useFakeTimers();
    const options = runtimeOptions("chat-transient-cursor");
    const { rerender, unmount } = renderHook(
      ({ active }) => useChatRuntime({ ...options, queueActive: active }),
      { wrapper, initialProps: { active: true } },
    );
    try {
      act(() => {
        subscriptionOptions.current?.onEvent({
          transient: true,
          sequence: 99,
          type: "runtime.event",
          message: {
            id: "execution-1",
            role: "assistant",
            status: "running",
            revision: 99,
          },
        });
        vi.advanceTimersByTime(16);
      });
      rerender({ active: false });
      rerender({ active: true });
      expect(subscribeChatMock.mock.calls.at(-1)?.[1].after).toBe(1);
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("首发 handoff 已有执行身份时终态立即结束，不等待历史清除交接", async () => {
    const options = {
      ...runtimeOptions("chat-handoff-terminal"),
      activeExecutionId: undefined,
    };
    const { result } = renderHook(
      () => {
        const store = useChatQueueStore();
        useLayoutEffect(() => {
          store.setHandoff("chat-handoff-terminal", [
            {
              id: "user-1",
              role: "user",
              executionId: "execution-1",
            },
            {
              id: "execution-1",
              role: "assistant",
              executionId: "execution-1",
              status: "pending",
            },
          ]);
        }, [store]);
        return useChatRuntime(options);
      },
      { wrapper },
    );
    await waitFor(() => expect(result.current.isRunning).toBe(true));
    act(() =>
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "execution.canceled",
        message: {
          id: "execution-1",
          role: "assistant",
          executionId: "execution-1",
          status: "canceled",
          revision: 2,
        },
      }),
    );
    expect(result.current.isRunning).toBe(false);
  });

  it("无法证明连续性的终态、缺口与重连仍请求权威历史恢复", () => {
    const options = runtimeOptions("chat-recover");
    queryClient.setQueryData(chatMessagesKey("chat-recover"), {
      pages: [{ resources: [], latestSequence: 1, hasMore: false }],
      pageParams: [undefined],
    });
    renderHook(() => useChatRuntime(options), { wrapper });
    act(() => subscriptionOptions.current?.onReconnect?.());
    expect(options.onTerminal).toHaveBeenLastCalledWith();
    act(() =>
      subscriptionOptions.current?.onEvent({
        sequence: 8,
        type: "execution.failed",
        message: {
          id: "execution-1",
          executionId: "execution-1",
          role: "assistant",
          status: "failed",
          revision: 8,
        },
      }),
    );
    expect(options.onTerminal).toHaveBeenLastCalledWith();
    expect(
      queryClient.getQueryData(chatMessagesKey("chat-recover")),
    ).toMatchObject({ pages: [{ latestSequence: 1, resources: [] }] });
  });

  it("恢复快照后已收到的新持久批次不会在onReconnect中被丢弃", () => {
    vi.useFakeTimers();
    const options = runtimeOptions("chat-recovery-batch");
    queryClient.setQueryData(chatMessagesKey("chat-recovery-batch"), {
      pages: [{ resources: [], latestSequence: 1, hasMore: false }],
      pageParams: [undefined],
    });
    const { unmount } = renderHook(() => useChatRuntime(options), { wrapper });
    try {
      act(() => {
        subscriptionOptions.current?.onEvent({
          sequence: 2,
          type: "message.user_created",
          message: {
            id: "user-after-snapshot",
            role: "user",
            content: "恢复读取之后的新输入",
            revision: 2,
          },
        });
        subscriptionOptions.current?.onReconnect?.();
        vi.advanceTimersByTime(16);
      });
      expect(
        queryClient.getQueryData(chatMessagesKey("chat-recovery-batch")),
      ).toMatchObject({
        pages: [
          { latestSequence: 2, resources: [{ id: "user-after-snapshot" }] },
        ],
      });
      expect(projectedMessages.current).toContainEqual(
        expect.objectContaining({
          id: "user-after-snapshot",
          content: [{ type: "text", text: "恢复读取之后的新输入" }],
        }),
      );
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("空闲会话仍消费跨窗口消息，快速执行与终态不依赖详情查询再触发订阅", () => {
    const options = {
      ...runtimeOptions("chat-idle-stream"),
      activeExecutionId: undefined,
    };
    queryClient.setQueryData(chatMessagesKey("chat-idle-stream"), {
      pages: [{ resources: [], latestSequence: 1, hasMore: false }],
      pageParams: [undefined],
    });
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });
    expect(subscribeChatMock).toHaveBeenCalledExactlyOnceWith(
      "chat-idle-stream",
      expect.objectContaining({ after: 1 }),
    );
    act(() => {
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "message.user_created",
        message: {
          id: "external-user",
          role: "user",
          submissionId: "external-input",
          revision: 2,
        },
      });
      subscriptionOptions.current?.onEvent({
        sequence: 3,
        type: "execution.completed",
        message: {
          id: "external-assistant",
          executionId: "external-assistant",
          role: "assistant",
          submissionId: "external-input",
          status: "completed",
          revision: 3,
          displayParts: [{ type: "text", text: "另一个窗口的结果" }],
        },
      });
    });
    expect(result.current.isRunning).toBe(false);
    expect(
      queryClient.getQueryData(chatMessagesKey("chat-idle-stream")),
    ).toMatchObject({
      pages: [
        {
          latestSequence: 3,
          resources: [
            { id: "external-user" },
            { id: "external-assistant", status: "completed" },
          ],
        },
      ],
    });
    expect(subscribeChatMock).toHaveBeenCalledTimes(1);
  });

  it("收到 transient 文本快照后立即更新运行中消息", async () => {
    renderHook(() => useChatRuntime(runtimeOptions("chat-1")), { wrapper });

    act(() => {
      subscriptionOptions.current?.onEvent({
        transient: true,
        type: "runtime.event",
        message: {
          id: "assistant-1",
          role: "assistant",
          executionId: "execution-1",
          status: "running",
          revision: 2,
          displayParts: [{ type: "text", text: "第一段增量" }],
        },
      });
    });

    await waitFor(() =>
      expect(projectedMessages.current).toContainEqual(
        expect.objectContaining({
          id: "assistant-1",
          status: { type: "running" },
          content: [{ type: "text", text: "第一段增量" }],
        }),
      ),
    );
  });

  it("终态事件即使 revision 落后也立即接管流式状态和完整内容", async () => {
    const { result } = renderHook(
      () => useChatRuntime(runtimeOptions("chat-1")),
      { wrapper },
    );

    act(() => {
      subscriptionOptions.current?.onEvent({
        transient: true,
        type: "runtime.event",
        message: {
          id: "assistant-1",
          executionId: "execution-1",
          role: "assistant",
          status: "running",
          revision: 6,
          displayParts: [
            { type: "text", text: "```mermaid\nsequenceDiagram\nA->>B: 请求" },
          ],
        },
      });
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "execution.completed",
        executionStatus: "completed",
        message: {
          id: "assistant-1",
          executionId: "execution-1",
          role: "assistant",
          status: "completed",
          revision: 5,
          displayParts: [
            {
              type: "text",
              text: "```mermaid\nsequenceDiagram\nA->>B: 请求\nB-->>A: 响应\n```",
            },
          ],
        },
      });
    });

    await waitFor(() => expect(result.current.isRunning).toBe(false));
    await waitFor(() =>
      expect(projectedMessages.current).toContainEqual(
        expect.objectContaining({
          id: "assistant-1",
          status: { type: "complete", reason: "stop" },
          content: [
            {
              type: "text",
              text: "```mermaid\nsequenceDiagram\nA->>B: 请求\nB-->>A: 响应\n```",
            },
          ],
        }),
      ),
    );
  });

  it("普通停止不会清空待发送队列", () => {
    renderHook(() => useChatRuntime(runtimeOptions("chat-1")), {
      wrapper,
    });

    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);

    act(() => {
      runtimeAdapter.current?.queue?.clear("cancel-run");
    });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);
  });

  it("空闲时由 assistant-ui queue adapter 立即发送", async () => {
    const options = runtimeOptions("chat-1");
    const send = vi.fn().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-1",
        userMessageId: "user-1",
        status: "pending",
      },
    });
    options.activeExecutionId = undefined;
    options.send = send;
    renderHook(() => useChatRuntime(options), { wrapper });

    await act(async () => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
      await Promise.resolve();
    });

    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);
  });

  it("按 Chat 隔离并保留未发送队列", () => {
    const { rerender } = renderHook(
      ({ queueKey }: { queueKey: string }) =>
        useChatRuntime(runtimeOptions(queueKey)),
      { initialProps: { queueKey: "chat-1" }, wrapper },
    );

    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);

    rerender({ queueKey: "chat-2" });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);

    rerender({ queueKey: "chat-1" });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);
  });

  it("保留的 Scene 隐藏后暂停出队，当前执行结束不取消运行，返回时只发送一次", async () => {
    const options = runtimeOptions("chat-hidden");
    options.send = vi.fn().mockResolvedValue({
      chat: { id: "chat-hidden" },
      execution: { id: "execution-2", status: "running" },
    });
    const { rerender } = renderHook(
      ({ visible, running }: { visible: boolean; running: boolean }) =>
        useChatRuntime({
          ...options,
          queueActive: visible,
          activeExecutionId: running ? "execution-1" : undefined,
        }),
      { initialProps: { visible: true, running: true }, wrapper },
    );
    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    rerender({ visible: false, running: true });
    expect(options.cancel).not.toHaveBeenCalled();
    rerender({ visible: false, running: false });
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 20));
    });
    expect(options.send).not.toHaveBeenCalled();
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);
    rerender({ visible: true, running: false });
    await waitFor(() => expect(options.send).toHaveBeenCalledOnce());
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);
  });

  it("隐藏且空闲时入队也保留消息，返回后才开始执行", async () => {
    const options = runtimeOptions("chat-hidden-idle");
    options.activeExecutionId = undefined;
    options.send = vi.fn().mockResolvedValue({
      chat: { id: "chat-hidden-idle" },
      execution: { id: "execution-2", status: "running" },
    });
    const { rerender } = renderHook(
      ({ visible }: { visible: boolean }) =>
        useChatRuntime({ ...options, queueActive: visible }),
      { initialProps: { visible: false }, wrapper },
    );
    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    expect(options.send).not.toHaveBeenCalled();
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);
    rerender({ visible: true });
    await waitFor(() => expect(options.send).toHaveBeenCalledOnce());
  });

  it("切换团队成员后保留原成员队列，回到已结束执行的成员才恢复出队", async () => {
    const send = vi.fn().mockResolvedValue({
      chat: { id: "member-1" },
      execution: { id: "execution-2", status: "running" },
    });
    const { rerender } = renderHook(
      ({ member, running }: { member: string; running: boolean }) =>
        useChatRuntime({
          ...runtimeOptions(member),
          send,
          activeExecutionId: running ? "execution-1" : undefined,
        }),
      { initialProps: { member: "member-1", running: true }, wrapper },
    );
    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    rerender({ member: "member-2", running: false });
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 20));
    });
    expect(send).not.toHaveBeenCalled();
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);
    rerender({ member: "member-1", running: false });
    await waitFor(() => expect(send).toHaveBeenCalledOnce());
    expect(send).toHaveBeenCalledWith(
      "排队消息",
      "member-1",
      [],
      expect.objectContaining(runtimeOptions("member-1").selection),
    );
  });

  it("当前执行进入终态后自动发送队首消息", async () => {
    const options = runtimeOptions("chat-1");
    const send = vi.fn().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-2",
        userMessageId: "user-2",
        status: "pending",
      },
    });
    options.send = send;
    const { rerender } = renderHook(
      ({ active }: { active: boolean }) =>
        useChatRuntime({
          ...options,
          activeExecutionId: active ? "execution-1" : undefined,
        }),
      { initialProps: { active: true }, wrapper },
    );

    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    rerender({ active: false });

    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(send).toHaveBeenCalledWith("排队消息", "chat-1", [], {
      runtimeId: "codex",
      model: "gpt-5",
      reasoning: "high",
      permissionLevel: "auto",
    });
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);
  });

  it("切回空闲 Chat 后继续发送其队首消息", async () => {
    const options = runtimeOptions("chat-1");
    const send = vi.fn().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-2",
        userMessageId: "user-2",
        status: "pending",
      },
    });
    options.send = send;
    const { rerender } = renderHook(
      ({ queueKey, active }: { queueKey: string; active: boolean }) =>
        useChatRuntime({
          ...options,
          queueKey,
          chatId: queueKey,
          activeExecutionId: active ? "execution-1" : undefined,
        }),
      {
        initialProps: { queueKey: "chat-1", active: true },
        wrapper,
      },
    );

    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    rerender({ queueKey: "chat-2", active: false });
    expect(send).not.toHaveBeenCalled();

    rerender({ queueKey: "chat-1", active: false });

    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);
  });

  it("POST 返回前切换 Chat 不执行旧页面回调", async () => {
    let resolveSend: ((value: MessageResult) => void) | undefined;
    const send = vi.fn(
      () =>
        new Promise<MessageResult>((resolve) => {
          resolveSend = resolve;
        }),
    );
    const firstStarted = vi.fn();
    const secondStarted = vi.fn();
    const { rerender } = renderHook(
      ({ queueKey }: { queueKey: string }) =>
        useChatRuntime({
          ...runtimeOptions(queueKey),
          activeExecutionId: undefined,
          send,
          onStarted: queueKey === "chat-1" ? firstStarted : secondStarted,
        }),
      {
        initialProps: { queueKey: "chat-1" },
        wrapper,
      },
    );

    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    rerender({ queueKey: "chat-2" });
    await act(async () => {
      resolveSend?.({
        chat: { id: "chat-1" },
        execution: {
          id: "execution-2",
          userMessageId: "user-2",
          status: "pending",
        },
      });
      await Promise.resolve();
    });

    expect(firstStarted).not.toHaveBeenCalled();
    expect(secondStarted).not.toHaveBeenCalled();
  });

  it("相同 Chat ID 切换 workspace scope 后忽略旧订阅的晚到交互事件", async () => {
    const { result, rerender } = renderHook(
      ({ scopeKey }: { scopeKey: string }) =>
        useChatRuntime({
          ...runtimeOptions("shared-chat"),
          conversationKey: conversationKeyOf(scopeKey, "shared-chat"),
        }),
      {
        initialProps: { scopeKey: "team:one:thread:one" },
        wrapper,
      },
    );
    const staleSubscription = subscriptionOptions.current;

    rerender({ scopeKey: "team:two:thread:two" });
    const currentSubscription = subscriptionOptions.current;
    expect(currentSubscription).not.toBe(staleSubscription);

    act(() => {
      currentSubscription?.onEvent({
        sequence: 2,
        type: "approval.requested",
        approvalRequest: {
          approvalId: "approval-current",
          toolCallId: "tool-current",
          title: "当前会话审批",
          options: [],
        },
      });
    });
    await waitFor(() =>
      expect(result.current.pendingApprovals).toHaveLength(1),
    );

    act(() => {
      staleSubscription?.onEvent({
        sequence: 3,
        type: "approval.resolved",
        approvalResolution: {
          approvalId: "approval-current",
          toolCallId: "tool-current",
          result: "selected",
        },
      });
      staleSubscription?.onEvent({
        sequence: 4,
        type: "elicitation.requested",
        elicitationRequest: {
          elicitationId: "elicitation-stale",
          message: "旧会话输入",
          mode: "form",
        },
      });
      staleSubscription?.onUnavailable?.();
    });

    expect(result.current.pendingApprovals).toEqual([
      expect.objectContaining({ approvalId: "approval-current" }),
    ]);
    expect(result.current.pendingElicitations).toEqual([]);
  });

  it("A 到 B 再回到 A 后拒绝第一轮订阅的晚到事件", async () => {
    const { result, rerender } = renderHook(
      ({ scopeKey }: { scopeKey: string }) =>
        useChatRuntime({
          ...runtimeOptions("shared-chat"),
          conversationKey: conversationKeyOf(scopeKey, "shared-chat"),
        }),
      {
        initialProps: { scopeKey: "team:one:thread:one" },
        wrapper,
      },
    );
    const staleSubscription = subscriptionOptions.current;

    rerender({ scopeKey: "team:two:thread:two" });
    rerender({ scopeKey: "team:one:thread:one" });
    const currentSubscription = subscriptionOptions.current;
    expect(currentSubscription).not.toBe(staleSubscription);

    act(() => {
      currentSubscription?.onEvent({
        sequence: 2,
        type: "approval.requested",
        approvalRequest: {
          approvalId: "approval-current",
          toolCallId: "tool-current",
          title: "当前会话审批",
          options: [],
        },
      });
    });
    await waitFor(() =>
      expect(result.current.pendingApprovals).toHaveLength(1),
    );

    act(() => {
      staleSubscription?.onEvent({
        sequence: 3,
        type: "approval.resolved",
        approvalResolution: {
          approvalId: "approval-current",
          toolCallId: "tool-current",
          result: "selected",
        },
      });
      staleSubscription?.onEvent({
        sequence: 4,
        type: "elicitation.requested",
        elicitationRequest: {
          elicitationId: "elicitation-stale",
          message: "旧会话输入",
          mode: "form",
        },
      });
      staleSubscription?.onUnavailable?.();
    });

    expect(result.current.pendingApprovals).toEqual([
      expect.objectContaining({ approvalId: "approval-current" }),
    ]);
    expect(result.current.pendingElicitations).toEqual([]);
  });

  it("草稿首发创建正式 Chat 后重挂载仍保留可见消息", async () => {
    const send = vi.fn().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-1",
        userMessageId: "user-1",
        status: "pending",
      },
    });
    const draftOptions: RuntimeOptions = {
      ...runtimeOptions("draft:waker-1"),
      chatId: undefined,
      activeExecutionId: undefined,
      isHistoryReady: true,
      send,
    };
    const { rerender } = render(
      createElement(RuntimeRemountHarness, {
        hookKey: "draft",
        options: draftOptions,
      }),
    );

    await act(async () => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
      await Promise.resolve();
    });

    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(projectedMessages.current).toContainEqual(
        expect.objectContaining({
          id: "user-1",
          content: [{ type: "text", text: "排队消息" }],
        }),
      ),
    );

    rerender(
      createElement(RuntimeRemountHarness, {
        hookKey: "chat-1",
        options: {
          ...runtimeOptions("chat-1"),
          activeExecutionId: "execution-1",
          isHistoryReady: false,
          messages: [],
        },
      }),
    );

    expect(projectedMessages.current).toContainEqual(
      expect.objectContaining({
        id: "user-1",
        content: [{ type: "text", text: "排队消息" }],
      }),
    );
    expect(projectedMessages.current).toContainEqual(
      expect.objectContaining({
        id: "execution-1",
        status: { type: "running" },
      }),
    );
  });

  it("草稿首发返回执行后在详情刷新前持续保持运行态", async () => {
    const send = vi.fn().mockResolvedValue({
      chat: { id: "chat-created" },
      execution: {
        id: "execution-created",
        userMessageId: "user-created",
        status: "pending",
      },
    });
    const options: RuntimeOptions = {
      ...runtimeOptions("draft:waker-1"),
      chatId: undefined,
      activeExecutionId: undefined,
      send,
    };
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });

    await act(async () => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
      await Promise.resolve();
    });

    await waitFor(() => expect(send).toHaveBeenCalledOnce());
    expect(result.current.isRunning).toBe(true);
  });

  it.each(["主动续发", "队列出队"])(
    "终态先于POST回执时，%s相同正文也取得新输入身份与执行",
    async (mode) => {
      const chatId = `identity-${mode}`;
      const ids: string[] = [];
      const prepared = new Map<
        string,
        { resolve: (value: MessageResult) => void }
      >();
      const options = {
        ...runtimeOptions(chatId),
        activeExecutionId: undefined,
      };
      queryClient.setQueryData(chatMessagesKey(chatId), {
        pages: [{ resources: [], latestSequence: 1, hasMore: false }],
        pageParams: [undefined],
      });
      const { result } = renderHook(
        () =>
          useProductionChatRuntime({
            ...options,
            messageSource: chatMessageSource(queryClient, chatId, {
              messages: [],
              latestSequence: 1,
              ready: true,
            }),
            prepareSubmission: (text) => {
              const request = withStableCreateRequest(`send:${chatId}`, {
                text,
                runtimeId: "codex",
                model: "gpt-5",
                attachments: [],
              });
              const submissionId = request.clientRequestId;
              ids.push(submissionId);
              return {
                submissionId,
                confirmAccepted: () =>
                  clearStableCreateRequest(`send:${chatId}`, submissionId),
                accept: () =>
                  new Promise<MessageResult>((resolve) => {
                    prepared.set(submissionId, { resolve });
                  }),
              };
            },
          }),
        { wrapper },
      );
      act(() => {
        void runtimeAdapter.current?.onNew?.(queuedMessage);
      });
      if (mode === "队列出队")
        act(() =>
          runtimeAdapter.current?.queue?.enqueue(queuedMessage, {
            steer: false,
          }),
        );
      act(() => {
        subscriptionOptions.current?.onEvent({
          sequence: 2,
          type: "message.user_created",
          message: {
            id: "user-1",
            role: "user",
            submissionId: ids[0],
            content: "排队消息",
            revision: 2,
          },
        });
        subscriptionOptions.current?.onEvent({
          sequence: 3,
          type: "execution.created",
          message: {
            id: "exec-1",
            executionId: "exec-1",
            role: "assistant",
            submissionId: ids[0],
            status: "pending",
            revision: 3,
          },
        });
        subscriptionOptions.current?.onEvent({
          sequence: 4,
          type: "execution.completed",
          message: {
            id: "exec-1",
            executionId: "exec-1",
            role: "assistant",
            submissionId: ids[0],
            status: "completed",
            revision: 4,
          },
        });
      });
      expect(result.current.isRunning).toBe(false);
      if (mode === "主动续发")
        act(() => {
          void runtimeAdapter.current?.onNew?.(queuedMessage);
        });
      await waitFor(() => expect(ids).toHaveLength(2));
      expect(ids[1]).not.toBe(ids[0]);
      act(() => {
        subscriptionOptions.current?.onEvent({
          sequence: 5,
          type: "message.user_created",
          message: {
            id: "user-2",
            role: "user",
            submissionId: ids[1],
            content: "排队消息",
            revision: 5,
          },
        });
        subscriptionOptions.current?.onEvent({
          sequence: 6,
          type: "execution.created",
          message: {
            id: "exec-2",
            executionId: "exec-2",
            role: "assistant",
            submissionId: ids[1],
            status: "pending",
            revision: 6,
          },
        });
      });
      await waitFor(() =>
        expect(projectedMessages.current).toContainEqual(
          expect.objectContaining({
            id: "exec-2",
            status: { type: "running" },
          }),
        ),
      );
      expect(result.current.isRunning).toBe(true);
    },
  );

  it("早到持久user已证明接纳即可退役请求，旧HTTP成功不清后来准备记录", async () => {
    const chatId = "identity-user-first";
    const scope = `send:${chatId}`;
    const body = { text: "排队消息", runtimeId: "codex" };
    let accept!: (value: MessageResult) => void;
    let firstId = "";
    const options = { ...runtimeOptions(chatId), activeExecutionId: undefined };
    queryClient.setQueryData(chatMessagesKey(chatId), {
      pages: [{ resources: [], latestSequence: 1, hasMore: false }],
      pageParams: [undefined],
    });
    renderHook(
      () =>
        useProductionChatRuntime({
          ...options,
          messageSource: chatMessageSource(queryClient, chatId, {
            messages: [],
            latestSequence: 1,
            ready: true,
          }),
          prepareSubmission: () => {
            firstId = withStableCreateRequest(scope, body).clientRequestId;
            return {
              submissionId: firstId,
              confirmAccepted: () => clearStableCreateRequest(scope, firstId),
              accept: () =>
                new Promise<MessageResult>((resolve) => {
                  accept = resolve;
                }),
            };
          },
        }),
      { wrapper },
    );
    let pending: Promise<void> | void;
    act(() => {
      pending = runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    act(() =>
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "message.user_created",
        message: {
          id: "user-1",
          role: "user",
          submissionId: firstId,
          revision: 2,
        },
      }),
    );
    const next = withStableCreateRequest(scope, body);
    expect(next.clientRequestId).not.toBe(firstId);
    await act(async () => {
      accept({
        chat: { id: chatId },
        execution: { id: "exec-1", userMessageId: "user-1", status: "pending" },
      });
      await pending;
    });
    expect(withStableCreateRequest(scope, body).clientRequestId).toBe(
      next.clientRequestId,
    );
  });

  it("没有权威接纳事实的未知响应重试保留原输入身份", async () => {
    const chatId = "identity-unknown";
    const scope = `send:${chatId}`;
    const ids: string[] = [];
    const options = { ...runtimeOptions(chatId), activeExecutionId: undefined };
    renderHook(
      () =>
        useProductionChatRuntime({
          ...options,
          messageSource: chatMessageSource(queryClient, chatId, {
            messages: [],
            latestSequence: 1,
            ready: true,
          }),
          prepareSubmission: (text) => {
            const submissionId = withStableCreateRequest(scope, {
              text,
              runtimeId: "codex",
            }).clientRequestId;
            ids.push(submissionId);
            return {
              submissionId,
              confirmAccepted: () =>
                clearStableCreateRequest(scope, submissionId),
              accept: () =>
                Promise.reject(new ApiError("Failed to fetch", "NETWORK")),
            };
          },
        }),
      { wrapper },
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    expect(ids).toHaveLength(2);
    expect(ids[1]).toBe(ids[0]);
  });

  it.each(["成功", "失败"])(
    "A已由终态接纳后HTTP晚到%s不清除B输入状态或幂等身份",
    async (outcome) => {
      const chatId = `identity-late-${outcome}`;
      const scope = `send:${chatId}`;
      const body = (text: string) => ({ text, runtimeId: "codex" });
      const ids: string[] = [];
      let resolveA!: (value: MessageResult) => void;
      let rejectA!: (error: Error) => void;
      const options = {
        ...runtimeOptions(chatId),
        activeExecutionId: undefined,
      };
      queryClient.setQueryData(chatMessagesKey(chatId), {
        pages: [{ resources: [], latestSequence: 1, hasMore: false }],
        pageParams: [undefined],
      });
      const { result } = renderHook(
        () =>
          useProductionChatRuntime({
            ...options,
            messageSource: chatMessageSource(queryClient, chatId, {
              messages: [],
              latestSequence: 1,
              ready: true,
            }),
            prepareSubmission: (text) => {
              const submissionId = withStableCreateRequest(
                scope,
                body(text),
              ).clientRequestId;
              ids.push(submissionId);
              return {
                submissionId,
                confirmAccepted: () =>
                  clearStableCreateRequest(scope, submissionId),
                accept: () =>
                  ids.length === 1
                    ? new Promise<MessageResult>((resolve, reject) => {
                        resolveA = resolve;
                        rejectA = reject;
                      })
                    : new Promise<MessageResult>(() => {}),
              };
            },
          }),
        { wrapper },
      );
      let pendingA: Promise<void> | void;
      act(() => {
        pendingA = runtimeAdapter.current?.onNew?.(queuedMessage);
      });
      act(() =>
        subscriptionOptions.current?.onEvent({
          sequence: 2,
          type: "execution.completed",
          message: {
            id: "exec-1",
            executionId: "exec-1",
            role: "assistant",
            submissionId: ids[0],
            status: "completed",
            revision: 2,
          },
        }),
      );
      const second = {
        ...queuedMessage,
        content: [{ type: "text" as const, text: "第二轮仍未接纳" }],
      };
      act(() => {
        void runtimeAdapter.current?.onNew?.(second);
      });
      const secondMessages = projectedMessages.current.filter((item) =>
        (item as { content?: { text?: string }[] }).content?.some(
          (part) => part.text === "第二轮仍未接纳",
        ),
      );
      expect(secondMessages).toHaveLength(1);
      await act(async () => {
        if (outcome === "成功")
          resolveA({
            chat: { id: chatId },
            execution: {
              id: "exec-1",
              userMessageId: "user-1",
              status: "pending",
            },
          });
        else rejectA(new ApiError("Failed to fetch", "NETWORK"));
        await pendingA;
      });
      expect(result.current.isRunning).toBe(true);
      expect(result.current.sendFailure).toEqual({ kind: "none" });
      expect(projectedMessages.current).toEqual(
        expect.arrayContaining(secondMessages),
      );
      expect(
        withStableCreateRequest(scope, body("第二轮仍未接纳")).clientRequestId,
      ).toBe(ids[1]);
      act(() =>
        runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false }),
      );
      expect(ids).toHaveLength(2);
      expect(runtimeAdapter.current?.queue?.items).toHaveLength(1);
      await act(async () => {
        await runtimeAdapter.current?.onCancel?.();
      });
      expect(options.cancel).toHaveBeenCalledTimes(1);
    },
  );

  it("A错误安排的延后输入恢复不能在B开始后重放旧选择", async () => {
    vi.useFakeTimers();
    const chatId = "identity-delayed-restore";
    const onMessageRejected = vi.fn();
    let calls = 0;
    const options = {
      ...runtimeOptions(chatId),
      activeExecutionId: undefined,
      onMessageRejected,
    };
    const { result, unmount } = renderHook(
      () =>
        useProductionChatRuntime({
          ...options,
          messageSource: chatMessageSource(queryClient, chatId, {
            messages: [],
            latestSequence: 1,
            ready: true,
          }),
          prepareSubmission: (text) => {
            const submissionId = withStableCreateRequest(`send:${chatId}`, {
              text,
            }).clientRequestId;
            calls++;
            return {
              submissionId,
              confirmAccepted: () =>
                clearStableCreateRequest(`send:${chatId}`, submissionId),
              accept: () =>
                calls === 1
                  ? Promise.reject(new ApiError("Failed to fetch", "NETWORK"))
                  : new Promise<MessageResult>(() => {}),
            };
          },
        }),
      { wrapper },
    );
    try {
      await act(async () => {
        await runtimeAdapter.current?.onNew?.(queuedMessage);
      });
      act(() => {
        void runtimeAdapter.current?.onNew?.({
          ...queuedMessage,
          content: [{ type: "text", text: "B的新输入" }],
        });
      });
      act(() => vi.advanceTimersByTime(0));
      expect(result.current.isRunning).toBe(true);
      expect(result.current.sendFailure).toEqual({ kind: "none" });
      expect(onMessageRejected).not.toHaveBeenCalled();
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("持久用户与助手先于接纳回执时按submissionId接管乐观行，终态无需等待POST", async () => {
    let accept!: (result: MessageResult) => void;
    const options = {
      ...runtimeOptions("chat-pre-receipt"),
      activeExecutionId: undefined,
      send: vi.fn(
        () =>
          new Promise<MessageResult>((resolve) => {
            accept = resolve;
          }),
      ),
    };
    queryClient.setQueryData(chatMessagesKey("chat-pre-receipt"), {
      pages: [{ resources: [], latestSequence: 1, hasMore: false }],
      pageParams: [undefined],
    });
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });
    let pending: Promise<void> | void;
    act(() => {
      pending = runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    act(() => {
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "message.user_created",
        message: {
          id: "real-user",
          role: "user",
          submissionId: "input:chat-pre-receipt",
          content: "排队消息",
          revision: 2,
        },
      });
      subscriptionOptions.current?.onEvent({
        sequence: 3,
        type: "execution.created",
        message: {
          id: "real-assistant",
          executionId: "real-assistant",
          role: "assistant",
          submissionId: "input:chat-pre-receipt",
          status: "pending",
          revision: 3,
        },
      });
    });
    await waitFor(() =>
      expect(
        projectedMessages.current.map((item) => (item as { id: string }).id),
      ).toEqual(["real-user", "real-assistant"]),
    );
    act(() =>
      subscriptionOptions.current?.onEvent({
        sequence: 4,
        type: "execution.completed",
        message: {
          id: "real-assistant",
          executionId: "real-assistant",
          role: "assistant",
          submissionId: "input:chat-pre-receipt",
          status: "completed",
          revision: 4,
          displayParts: [{ type: "text", text: "已完成" }],
        },
      }),
    );
    expect(result.current.isRunning).toBe(false);
    await act(async () => {
      accept({
        chat: { id: "chat-pre-receipt" },
        execution: {
          id: "real-assistant",
          userMessageId: "real-user",
          status: "pending",
          submissionId: "input:chat-pre-receipt",
        },
      });
      await pending;
    });
    expect(result.current.isRunning).toBe(false);
    expect(
      projectedMessages.current.map((item) => (item as { id: string }).id),
    ).toEqual(["real-user", "real-assistant"]);
  });

  it("旧接纳回执晚于终态到达时保留完整结果和终态", async () => {
    let accept!: (result: MessageResult) => void;
    const options = {
      ...runtimeOptions("chat-late-receipt"),
      activeExecutionId: undefined,
      send: vi.fn(
        () =>
          new Promise<MessageResult>((resolve) => {
            accept = resolve;
          }),
      ),
    };
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });
    let pending: Promise<void> | void;
    act(() => {
      pending = runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    act(() =>
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "execution.completed",
        message: {
          id: "execution-1",
          executionId: "execution-1",
          role: "assistant",
          status: "completed",
          revision: 6,
          displayParts: [{ type: "text", text: "先到达的完整结果" }],
        },
      }),
    );
    await act(async () => {
      accept({
        chat: { id: "chat-late-receipt" },
        execution: {
          id: "execution-1",
          userMessageId: "user-1",
          status: "pending",
        },
      });
      await pending;
    });
    expect(result.current.isRunning).toBe(false);
    expect(
      projectedMessages.current.filter(
        (item) => (item as { id: string }).id === "execution-1",
      ),
    ).toEqual([
      expect.objectContaining({
        id: "execution-1",
        status: { type: "complete", reason: "stop" },
        content: [{ type: "text", text: "先到达的完整结果" }],
      }),
    ]);
  });

  it("首发首次历史已包含终态且没有后续推送时也结束运行", async () => {
    const options: RuntimeOptions = {
      ...runtimeOptions("draft:already-finished"),
      chatId: undefined,
      activeExecutionId: undefined,
      send: vi.fn().mockResolvedValue({
        chat: { id: "chat-already-finished" },
        execution: {
          id: "execution-1",
          userMessageId: "user-1",
          status: "pending",
        },
      }),
    };
    const { result, rerender } = renderHook(
      ({ current }) => useChatRuntime(current),
      { wrapper, initialProps: { current: options } },
    );
    await act(async () => {
      await runtimeAdapter.current?.onNew?.(queuedMessage);
    });
    expect(result.current.isRunning).toBe(true);
    rerender({
      current: {
        ...options,
        conversationKey: conversationKeyOf(
          "scope:finished",
          "chat-already-finished",
        ),
        queueKey: "chat-already-finished",
        chatId: "chat-already-finished",
        messages: [
          { id: "user-1", role: "user", content: "排队消息", revision: 2 },
          {
            id: "execution-1",
            role: "assistant",
            executionId: "execution-1",
            status: "completed",
            revision: 6,
          },
        ],
        latestSequence: 6,
      },
    });
    expect(result.current.isRunning).toBe(false);
  });

  it("等待审批时展示到达顺序并拒绝新增排队消息", async () => {
    const options = runtimeOptions("chat-1");
    const onApprovalRequested = vi.fn();
    options.onApprovalRequested = onApprovalRequested;
    const { result } = renderHook(() => useChatRuntime(options), {
      wrapper,
    });

    act(() => {
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        recordedAt: 200,
        type: "approval.requested",
        approvalRequest: {
          approvalId: "approval-1",
          toolCallId: "tool-1",
          title: "执行命令",
          options: [
            { id: "allow-once", name: "Allow once", kind: "allow_once" },
            { id: "reject-once", name: "Reject", kind: "reject_once" },
          ],
        },
      });
    });

    expect(result.current.pendingApprovals).toEqual([
      expect.objectContaining({
        approvalId: "approval-1",
        toolCallId: "tool-1",
        requestedAt: 200,
      }),
    ]);
    expect(onApprovalRequested).toHaveBeenCalledTimes(1);
    act(() => {
      runtimeAdapter.current?.queue?.enqueue(queuedMessage, { steer: false });
    });
    expect(options.send).not.toHaveBeenCalled();
    expect(runtimeAdapter.current?.queue?.items).toHaveLength(0);

    act(() => {
      subscriptionOptions.current?.onEvent({
        sequence: 3,
        type: "approval.resolved",
        approvalResolution: {
          approvalId: "approval-1",
          toolCallId: "tool-1",
          result: "selected",
          optionId: "allow-once",
        },
      });
    });
    expect(result.current.pendingApprovals).toEqual([]);
  });

  it("执行进行中时等待历史就绪后再订阅 SSE", async () => {
    const options = runtimeOptions("chat-1");
    options.isHistoryReady = false;
    options.activeExecutionId = "execution-1";
    const { rerender } = renderHook(() => useChatRuntime(options), { wrapper });

    expect(subscribeChatMock).not.toHaveBeenCalled();

    options.isHistoryReady = true;
    rerender();

    await waitFor(() =>
      expect(subscribeChatMock).toHaveBeenCalledWith(
        "chat-1",
        expect.objectContaining({ after: 1 }),
      ),
    );
  });

  it("SSE 重连或降级时清除仅由旧连接投影的结构化输入", async () => {
    const { result } = renderHook(
      () => useChatRuntime(runtimeOptions("chat-1")),
      { wrapper },
    );
    const request = () =>
      subscriptionOptions.current?.onEvent({
        sequence: 2,
        type: "elicitation.requested",
        elicitationRequest: {
          elicitationId: "elicitation-1",
          message: "请选择",
          mode: "form",
        },
      });

    act(request);
    await waitFor(() =>
      expect(result.current.pendingElicitations).toHaveLength(1),
    );
    act(() => {
      subscriptionOptions.current?.onState?.("reconnecting");
      subscriptionOptions.current?.onReconnect?.();
    });
    await waitFor(() => expect(result.current.pendingElicitations).toEqual([]));

    act(request);
    await waitFor(() =>
      expect(result.current.pendingElicitations).toHaveLength(1),
    );
    act(() => subscriptionOptions.current?.onUnavailable?.());
    await waitFor(() => expect(result.current.pendingElicitations).toEqual([]));
  });

  it("已有可见消息时报告 hasVisibleMessages", () => {
    const options = runtimeOptions("chat-1");
    options.isHistoryReady = false;
    options.messages = [
      {
        id: "message-1",
        role: "user",
        content: "历史消息",
        status: "completed",
      },
    ];
    const { result } = renderHook(() => useChatRuntime(options), { wrapper });

    expect(result.current.hasVisibleMessages).toBe(true);
  });

  it("权威历史首帧就绪后清除无稳定 ID 的 handoff", async () => {
    const handoffMessages = [
      {
        id: "optimistic:user",
        role: "user" as const,
        content: "首条消息",
        submissionId: "first-input",
        optimistic: true,
      },
      {
        id: "optimistic:assistant",
        role: "assistant" as const,
        submissionId: "first-input",
        optimistic: true,
        content: "",
        status: "pending" as const,
      },
    ];
    const options = runtimeOptions("chat-1");
    options.isHistoryReady = false;
    options.activeExecutionId = undefined;
    options.messages = [];

    const { rerender } = renderHook(
      ({
        runtimeOptions: currentOptions,
      }: {
        runtimeOptions: RuntimeOptions;
      }) => {
        const store = useChatQueueStore();
        useLayoutEffect(() => {
          store.setHandoff("chat-1", handoffMessages);
        }, [store]);
        return useChatRuntime(currentOptions);
      },
      {
        wrapper: TestProviders,
        initialProps: { runtimeOptions: options },
      },
    );

    rerender({
      runtimeOptions: {
        ...options,
        isHistoryReady: true,
        messages: [
          {
            id: "user-1",
            role: "user",
            content: "首条消息",
            submissionId: "first-input",
            status: "completed",
          },
          {
            id: "execution-1",
            role: "assistant",
            submissionId: "first-input",
            status: "completed",
          },
        ],
      },
    });

    await waitFor(() => expect(runtimeAdapter.current?.isRunning).toBe(false));
  });

  it("把稳定消息引用交给 assistant-ui 的增量转换缓存", () => {
    const options = runtimeOptions("chat-1");
    const historicalMessage = {
      id: "message-stable",
      role: "user",
      content: "不会随流式回复变化的历史消息",
    } as const;
    options.messages = [historicalMessage];

    renderHook(() => useChatRuntime(options), { wrapper });

    expect(runtimeAdapter.current?.messages?.[0]).toBe(historicalMessage);
    expect(runtimeAdapter.current?.convertMessage).toBeTypeOf("function");
  });
});

describe("chat send failure", () => {
  it("发送失败后切换到新草稿不会泄漏旧对话 overlay", async () => {
    const send = vi
      .fn()
      .mockRejectedValue(
        new ApiError(
          "原生 Runtime 会话已失效，请新建会话后继续",
          "RuntimeSessionNotFound",
          500,
        ),
      );
    const authoritativeMessages = [
      {
        id: "message-history",
        role: "user" as const,
        content: "旧对话历史",
      },
    ];
    const { rerender } = render(
      createElement(RuntimeRemountHarness, {
        hookKey: "chat-1",
        options: {
          ...runtimeOptions("chat-1"),
          messages: authoritativeMessages,
          send,
        },
      }),
    );

    await act(async () => {
      await runtimeAdapter.current?.onNew?.({
        ...queuedMessage,
        content: [{ type: "text", text: "继续发送" }],
      });
    });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(projectedMessages.current.length).toBeGreaterThan(0);

    rerender(
      createElement(RuntimeRemountHarness, {
        hookKey: "draft-1",
        options: {
          ...runtimeOptions("draft-1"),
          conversationKey: conversationKeyOf("scope:draft-1", undefined),
          chatId: undefined,
          messages: [],
          activeExecutionId: undefined,
          send,
        },
      }),
    );

    expect(projectedMessages.current).toEqual([]);
  });

  it("区分明确业务拒绝和无法判断是否已提交", () => {
    expect(
      classifyChatSendFailure(
        new ApiError("当前对话已有运行中的执行", "ChatHasActiveExecution", 409),
      ),
    ).toEqual({
      kind: "active-execution",
      message: "当前对话已有运行中的执行",
    });
    expect(
      classifyChatSendFailure(new ApiError("Failed to fetch", "NETWORK")),
    ).toEqual({ kind: "unknown" });
    expect(
      classifyChatSendFailure(new ApiError("响应解析失败", "UNKNOWN", 500)),
    ).toEqual({ kind: "unknown", message: "响应解析失败" });
    expect(
      classifyChatSendFailure(
        new ApiError("对话仍在创建", "CreateRequestInProgress", 409),
      ),
    ).toEqual({
      kind: "create-request-in-progress",
      message: "对话仍在创建",
    });
    expect(
      classifyChatSendFailure(
        new ApiError("发送标识已用于其他内容", "CreateRequestConflict", 409),
      ),
    ).toEqual({
      kind: "create-request-conflict",
      message: "发送标识已用于其他内容",
    });
  });

  it.each(["RuntimeUnavailable", "SkillUnavailable"])(
    "把 Prepare 前置失败 %s 识别为明确拒绝",
    (code) => {
      expect(
        classifyChatSendFailure(new ApiError("消息尚未提交", code, 500)),
      ).toEqual({ kind: "rejected", message: "消息尚未提交" });
    },
  );
});
