import { createElement, type ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
} from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { CoreEventClient } from "@neuxbot/platform-contracts/core-event-client";
import type {
  GlobalEventDelivery,
  GlobalEventSubscription,
} from "@neuxbot/platform-contracts/core-event-protocol";
import { client } from "@/shared/api/generated/client.gen";
import { resetGlobalEvents } from "@/shared/api/global-events";
import {
  chatMessagesInfiniteOptions,
  chatMessagesKey,
  chatMessageSource,
  useChatHistoryVisibility,
} from "../api/chat-queries";
import { conversationKeyOf } from "../model/conversation-identity";
import { ChatQueueProvider } from "./chat-queue-store";
import { useChatRuntime } from "./use-chat-runtime";

const native = vi.hoisted(() => ({
  listeners: new Set<(value: GlobalEventDelivery) => void>(),
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
  resync: vi.fn(),
}));
vi.mock("@assistant-ui/react", () => ({
  useExternalStoreRuntime: () => ({}),
}));
vi.mock("@/shared/api/runtime", () => ({
  initializeApi: async () => ({}),
}));
vi.mock("@/shared/platform", () => ({
  getCoreConnection: vi.fn(),
}));
vi.mock("@/shared/platform/provider", () => {
  const events = {
    subscribe_core_events: native.subscribe,
    unsubscribe_core_events: native.unsubscribe,
    resync_core_events: native.resync,
    onCoreEvent: (listener: (value: GlobalEventDelivery) => void) => {
      native.listeners.add(listener);
      return () => native.listeners.delete(listener);
    },
  };
  return {
    configurePlatform: vi.fn(),
    platform: () => ({ events }),
    requirePlatformService: () => events,
  };
});

class Socket extends EventTarget {
  readyState = 0;
  sent: string[] = [];
  onopen?: () => void;
  onmessage?: (event: { data: string }) => void;
  send(value: string) {
    this.sent.push(value);
  }
  close() {
    this.readyState = 3;
    this.dispatchEvent(new Event("close"));
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
}

afterEach(async () => {
  await resetGlobalEvents();
  native.listeners.clear();
  vi.clearAllMocks();
});

it("快速切回聊天时，主进程重叠订阅恢复晚于 HTTP 快照提交的终态", async () => {
  const socket = new Socket();
  const owner = new CoreEventClient({
    connection: async () => ({
      baseUrl: "http://127.0.0.1:19881",
      token: "test",
    }),
    createSocket: () => socket as unknown as WebSocket,
  });
  const stops = new Map<string, () => void>();
  let releaseOldRegistration!: () => void;
  let releaseNewRegistration!: () => void;
  const oldRegistration = new Promise<void>((resolve) => {
    releaseOldRegistration = resolve;
  });
  const newRegistration = new Promise<void>((resolve) => {
    releaseNewRegistration = resolve;
  });
  const emit = (delivery: GlobalEventDelivery) => {
    for (const listener of native.listeners) listener(delivery);
  };
  let registrations = 0;
  native.subscribe.mockImplementation(async (args: GlobalEventSubscription) => {
    const first = ++registrations === 1;
    if (!first) await newRegistration;
    const { subscriptionId, topic, resource, after } = args;
    stops.set(
      subscriptionId,
      owner.subscribe(topic, resource, {
        after,
        onEvent: (frame) => emit({ subscriptionId, kind: "event", frame }),
        onState: (state) => emit({ subscriptionId, kind: "state", state }),
        onResync: (reason) => emit({ subscriptionId, kind: "resync", reason }),
      }),
    );
    if (first) await oldRegistration;
    return true;
  });
  native.unsubscribe.mockImplementation(async ({ subscriptionId }) => {
    stops.get(subscriptionId)?.();
    stops.delete(subscriptionId);
    return true;
  });
  const running = {
    id: "execution-1",
    executionId: "execution-1",
    role: "assistant",
    status: "running",
    revision: 243,
    liveRevision: 243,
    content: "已生成的正文",
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(chatMessagesKey("chat-1"), {
    pages: [{ resources: [running], latestSequence: 7, hasMore: false }],
    pageParams: [undefined],
  });
  let authoritativeTerminal = false;
  let reads = 0;
  let releaseOldSnapshot!: () => void;
  const oldSnapshot = new Promise<void>((resolve) => {
    releaseOldSnapshot = resolve;
  });
  const server = setupServer(
    http.get(
      "http://127.0.0.1:19881/api/v1/chats/chat-1/messages",
      async () => {
        const terminal = authoritativeTerminal;
        reads++;
        if (!terminal) await oldSnapshot;
        return HttpResponse.json({
          resources: [
            { ...running, status: terminal ? "completed" : "running" },
          ],
          latestSequence: terminal ? 13 : 7,
          hasMore: false,
        });
      },
    ),
  );
  client.setConfig({ baseUrl: "http://127.0.0.1:19881" });
  server.listen({ onUnhandledRequest: "error" });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(
      QueryClientProvider,
      { client: queryClient },
      createElement(ChatQueueProvider, null, children),
    );
  const view = renderHook(
    ({ active }) => {
      useChatHistoryVisibility("chat-1", active);
      const history = useInfiniteQuery({
        ...chatMessagesInfiniteOptions("chat-1"),
        enabled: active,
      });
      return useChatRuntime({
        conversationKey: conversationKeyOf("scope:chat-1", "chat-1"),
        queueKey: "chat-1",
        chatId: "chat-1",
        queueActive: active,
        messageSource: chatMessageSource(queryClient, "chat-1", {
          messages:
            history.data?.pages.flatMap((page) => page.resources ?? []) ?? [],
          latestSequence: history.data?.pages[0]?.latestSequence ?? 0,
          ready: Boolean(history.data),
        }),
        activeExecutionId: "execution-1",
        isLoading: false,
        isSendDisabled: false,
        attachmentOnlyText: "",
        incompleteErrorText: "",
        selection: {
          runtimeId: "workbuddy",
          model: "deepseek-v4.1-flash",
          reasoning: "",
          permissionLevel: "auto",
        },
        prepareSubmission: () => ({ accept: vi.fn() }),
        cancel: async () => {},
        onStarted: () => {},
        onTerminal: () => {},
      });
    },
    { wrapper, initialProps: { active: true } },
  );
  try {
    await waitFor(() => expect(socket.onopen).toBeTypeOf("function"));
    act(() => socket.open());
    expect(view.result.current.isRunning).toBe(true);
    view.rerender({ active: false });
    view.rerender({ active: true });
    // 可见性对账已经读到旧快照，原生订阅 IPC 尚在排队，终态在两者之间提交。
    await waitFor(() => expect(reads).toBe(1));
    authoritativeTerminal = true;
    act(() =>
      socket.onmessage?.({
        data: JSON.stringify({
          protocolVersion: 1,
          epoch: "epoch",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "chat",
          type: "chat.event",
          resource: { kind: "chat", id: "chat-1" },
          sourceCursor: 12,
          eventId: "terminal-12",
          payload: {
            type: "execution.completed",
            sequence: 12,
            message: { ...running, status: "completed" },
          },
        }),
      }),
    );
    await act(async () => {
      releaseNewRegistration();
      await newRegistration;
      releaseOldRegistration();
      releaseOldSnapshot();
    });
    await waitFor(() => expect(view.result.current.isRunning).toBe(false));
    expect(view.result.current.messages[0]?.status).toBe("completed");
    expect(
      socket.sent.filter((value) => JSON.parse(value).type === "subscribe"),
    ).toHaveLength(1);
  } finally {
    releaseOldRegistration();
    releaseNewRegistration();
    releaseOldSnapshot();
    view.unmount();
    await owner.close();
    queryClient.clear();
    server.close();
  }
});
