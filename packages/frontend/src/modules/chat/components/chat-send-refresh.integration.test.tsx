import { chatMessageSource } from "../api/chat-queries";
import {
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, useReducer, type ReactNode } from "react";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ChatQueueProvider } from "../runtime/chat-queue-store";
import { useChatRuntime as useProductionChatRuntime } from "../runtime/use-chat-runtime";
import { conversationKeyOf } from "../model/conversation-identity";
import { ChatThread } from "./chat-thread";
import type * as AssistantUI from "@assistant-ui/react";

type RuntimeFixtureOptions = Omit<
  Parameters<typeof useProductionChatRuntime>[0],
  "messageSource"
> & {
  messages: Parameters<
    typeof useProductionChatRuntime
  >[0]["messageSource"]["snapshot"]["messages"];
  latestSequence: number;
  isHistoryReady: boolean;
};
function useChatRuntime(options: RuntimeFixtureOptions) {
  const queryClient = useQueryClient();
  return useProductionChatRuntime({
    ...options,
    messageSource: chatMessageSource(queryClient, options.chatId, {
      messages: options.messages,
      latestSequence: options.latestSequence,
      ready: options.isHistoryReady,
    }),
  });
}
vi.mock("@/shared/api/events", () => ({ subscribeChat: () => () => {} }));
vi.mock("@assistant-ui/react", async (importOriginal) => {
  const original = await importOriginal<typeof AssistantUI>();
  const { useMemo } = await import("react");
  function DelayedNotifications({ children }: { children: ReactNode }) {
    const aui = original.useAui();
    // 暂停通知但保留真实运行时，复现只有父层更新后按钮才刷新的现象。
    const delayed = useMemo(
      () =>
        Object.assign(Object.create(aui) as typeof aui, {
          subscribe: () => () => undefined,
        }),
      [aui],
    );
    return (
      <original.AuiProvider value={delayed}>{children}</original.AuiProvider>
    );
  }
  return {
    ...original,
    AssistantRuntimeProvider: ({
      children,
      runtime,
    }: {
      children: ReactNode;
      runtime: AssistantUI.AssistantRuntime;
    }) => (
      <original.AssistantRuntimeProvider runtime={runtime}>
        <DelayedNotifications>{children}</DelayedNotifications>
      </original.AssistantRuntimeProvider>
    ),
  };
});

const originalScrollTo = HTMLElement.prototype.scrollTo;
beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
});
afterAll(() => {
  HTMLElement.prototype.scrollTo = originalScrollTo;
  vi.unstubAllGlobals();
});

it("聊天订阅通知延迟时，输入后无需切换 CLI 即可启用发送并提交当前正文", async () => {
  let composer: AssistantUI.ThreadComposerRuntime | undefined;
  const send = vi
    .fn<
      (
        ...args: Parameters<
          Parameters<typeof useChatRuntime>[0]["prepareSubmission"]
        >
      ) => ReturnType<
        ReturnType<
          Parameters<typeof useChatRuntime>[0]["prepareSubmission"]
        >["accept"]
      >
    >()
    .mockResolvedValue({
      chat: { id: "refresh-chat" },
      execution: { id: "refresh-execution", status: "running" },
    });
  function Harness() {
    const [clearRevision, clear] = useReducer(
      (revision: number) => revision + 1,
      0,
    );
    const state = useChatRuntime({
      conversationKey: conversationKeyOf("refresh-scope", "refresh-chat"),
      queueKey: "refresh-chat",
      chatId: "refresh-chat",
      messages: [],
      latestSequence: 0,
      isHistoryReady: true,
      isLoading: false,
      isSendDisabled: false,
      selection: {
        runtimeId: "codex",
        model: "gpt-6-sol",
        reasoning: "high",
        permissionLevel: "full-access",
      },
      attachmentOnlyText: "请查看附件。",
      incompleteErrorText: "执行未完成",
      prepareSubmission: (...args) => ({ accept: () => send(...args) }),
      cancel: async () => {},
      onMessageAccepted: clear,
      onStarted: () => {},
      onTerminal: () => {},
    });
    useEffect(() => {
      composer = state.runtime.thread.composer;
    }, [state.runtime]);
    return (
      <ChatThread
        runtime={state.runtime}
        wakerName="NeuxBot"
        emptyState={null}
        isRunning={state.isRunning}
        sendFailure={state.sendFailure}
        composerClearRevision={clearRevision}
        hasEarlierMessages={false}
        isLoadingEarlier={false}
        onLoadEarlier={async () => {}}
      />
    );
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <ChatQueueProvider>
        <TooltipProvider>
          <Harness />
        </TooltipProvider>
      </ChatQueueProvider>
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  const input = await screen.findByRole("textbox", { name: "输入消息" });
  await user.type(input, "获取传入工作项的评论");
  expect(input).toHaveTextContent("获取传入工作项的评论");
  expect(composer?.getState().text).toBe("获取传入工作项的评论");
  const button = screen.getByRole("button", { name: "发送消息" });
  await waitFor(() => expect(button).toBeEnabled());
  await user.clear(input);
  await waitFor(() => expect(button).toBeDisabled());
  await user.type(input, "获取传入工作项的评论");
  await waitFor(() => expect(button).toBeEnabled());
  await user.click(button);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith(
      "获取传入工作项的评论",
      "refresh-chat",
      [],
      expect.objectContaining({ runtimeId: "codex" }),
    ),
  );
  client.clear();
});
