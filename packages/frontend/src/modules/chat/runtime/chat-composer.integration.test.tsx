import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  AuiProvider,
  AssistantRuntimeProvider,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAui,
} from "@assistant-ui/react";
import {
  act,
  fireEvent,
  render as renderUI,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  StrictMode,
  useEffect,
  useMemo,
  useReducer,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { MemoryRouter } from "react-router-dom";
import "@/shared/i18n";
import { useChatRuntime } from "./use-chat-runtime";
import {
  ChatQueueProvider,
  type ChatCodeReference,
  type ChatSendSelection,
} from "./chat-queue-store";
import type { GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as MessageResult } from "@/shared/api/generated/types.gen";
import type { GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage } from "@/shared/api/generated/types.gen";
import type { ChatEvent } from "@/shared/api/events";
import { ApiError } from "@/shared/api/errors";
import type { FileDropEvent } from "@/shared/platform";
import { TooltipProvider } from "@/shared/ui/tooltip";
import themeStyles from "@/shared/styles/index.css?raw";
import { ChatComposerInput } from "../components/chat-composer-input";
import { workspaceFileDragType } from "../model/workspace-file-drag";
import { ChatThread } from "../components/chat-thread";
import { TeamWorkStatus } from "../components/team-work-status";
import { conversationKeyOf } from "../model/conversation-identity";
import { pathFile } from "./chat-attachment-adapter";

function render(ui: Parameters<typeof renderUI>[0]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderUI(ui, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}

const chatSubscription = vi.hoisted(() => ({
  onEvent: undefined as ((event: ChatEvent) => void) | undefined,
}));

const nativeDrop = vi.hoisted(() => ({
  onEvent: undefined as ((event: FileDropEvent) => void) | undefined,
}));
const attachmentStaging = vi.hoisted(() =>
  vi.fn(async (file: File) => `/tmp/ally-paste/${file.name}`),
);
const scopedSearch = vi.hoisted(() =>
  vi.fn(async () => [] as { path: string; relativePath: string }[]),
);
const scopedResolve = vi.hoisted(() =>
  vi.fn(async () => ({
    status: "resolved" as const,
    canonicalPath: "/tmp/project/AGENTS.md",
    baseDirectory: "/tmp/ally",
    workspaceRoot: "/tmp/ally",
    sourceRoots: ["/tmp/project"],
  })),
);

const workspaceCapability = vi.hoisted(() => ({ available: false }));

vi.mock("@/shared/platform", async (importOriginal) => ({
  ...((await importOriginal()) as Record<string, unknown>),
  hasPlatformService: (name: string) =>
    name === "workspace" && workspaceCapability.available,
  stageAttachment: attachmentStaging,
  searchWorkspaceResourceFiles: scopedSearch,
  resolveWorkspaceResourceReference: scopedResolve,
  subscribeFileDrop: (handler: (event: FileDropEvent) => void) => {
    nativeDrop.onEvent = handler;
    return () => {
      if (nativeDrop.onEvent === handler) nativeDrop.onEvent = undefined;
    };
  },
}));

vi.mock("@/shared/api/events", () => ({
  subscribeChat: (
    _chatId: string,
    options: { onEvent: (event: ChatEvent) => void },
  ) => {
    chatSubscription.onEvent = options.onEvent;
    return () => {
      if (chatSubscription.onEvent === options.onEvent) {
        chatSubscription.onEvent = undefined;
      }
    };
  },
}));

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const originalScrollTo = HTMLElement.prototype.scrollTo;

beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  HTMLElement.prototype.scrollTo = vi.fn();
});

afterAll(() => {
  HTMLElement.prototype.scrollTo = originalScrollTo;
  vi.unstubAllGlobals();
});

type SendMessage = (
  text: string,
  chatId?: string,
  attachmentPaths?: string[],
  selection?: ChatSendSelection,
) => Promise<MessageResult>;

function ComposerControls({
  codeReferenceRequest,
  onCodeReferencesChange,
  clearRevision,
  restoreRevision,
}: {
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  clearRevision?: number;
  restoreRevision?: number;
} = {}) {
  const [, refreshComposer] = useReducer((revision: number) => revision + 1, 0);
  return (
    <ComposerPrimitive.Root>
      <ChatComposerInput
        placeholder="输入消息"
        ariaLabel="message"
        onValueChange={refreshComposer}
        codeReferenceRequest={codeReferenceRequest}
        onCodeReferencesChange={onCodeReferencesChange}
        clearRevision={clearRevision}
        restoreRevision={restoreRevision}
      />
      <ComposerPrimitive.Send>send</ComposerPrimitive.Send>
    </ComposerPrimitive.Root>
  );
}

function AttachmentComposerHarness({
  onOpenFile,
}: {
  onOpenFile?: (file: {
    name: string;
    path: string;
    rootPath?: string;
  }) => void;
} = {}) {
  const aui = useAui();
  const [, refreshComposer] = useReducer((revision: number) => revision + 1, 0);
  useEffect(() => {
    void aui.composer.addAttachment(pathFile("/tmp/ally/readme.md"));
  }, [aui]);
  return (
    <ComposerPrimitive.Root>
      <ChatComposerInput
        placeholder="输入消息"
        ariaLabel="message"
        onValueChange={refreshComposer}
        onOpenFile={onOpenFile}
      />
    </ComposerPrimitive.Root>
  );
}

function EmptyAttachmentComposerHarness() {
  return (
    <ComposerPrimitive.Root>
      <ChatComposerInput placeholder="输入消息" ariaLabel="message" />
    </ComposerPrimitive.Root>
  );
}

function AttachmentSubscriptionGapHarness({
  onComposerReady,
  deferRemoval = false,
}: {
  onComposerReady: (composer: ReturnType<typeof useAui>["composer"]) => void;
  deferRemoval?: boolean;
}) {
  const aui = useAui();
  const [, refreshOnTextInput] = useReducer(
    (revision: number) => revision + 1,
    0,
  );
  const silentAui = useMemo(
    () =>
      Object.assign(Object.create(aui) as typeof aui, {
        subscribe: () => () => undefined,
      }),
    [aui],
  );
  useEffect(() => onComposerReady(aui.composer), [aui, onComposerReady]);
  return (
    <AuiProvider value={silentAui}>
      <ChatComposerInput
        placeholder="输入消息"
        ariaLabel="message"
        onValueChange={refreshOnTextInput}
        onAttachmentAdd={(path) => aui.composer.addAttachment(pathFile(path))}
        onAttachmentsRemove={async (paths) => {
          const attachments = aui.composer.getState().attachments;
          for (let index = attachments.length - 1; index >= 0; index--) {
            const sourcePath = (
              attachments[index]?.file as
                ReturnType<typeof pathFile> | undefined
            )?.allySourcePath;
            if (sourcePath && paths.includes(sourcePath)) {
              if (deferRemoval) {
                window.setTimeout(() => {
                  void aui.composer.attachment({ index }).remove();
                }, 0);
              } else {
                await aui.composer.attachment({ index }).remove();
              }
            }
          }
        }}
      />
    </AuiProvider>
  );
}

function MemberMentionComposerHarness({
  onMemberMentionChange,
}: {
  onMemberMentionChange: (wakerId?: string) => void;
}) {
  return (
    <ComposerPrimitive.Root>
      <ChatComposerInput
        placeholder="输入消息"
        ariaLabel="message"
        members={[
          {
            description: "团队成员",
            id: "member-1",
            name: "项目经理",
          },
        ]}
        onMemberMentionChange={onMemberMentionChange}
      />
    </ComposerPrimitive.Root>
  );
}

function ComposerHarness({
  activeExecutionId,
  onMessageAccepted,
  onMessageRejected,
  selection = {
    runtimeId: "codex",
    model: "gpt-5",
    reasoning: "high",
    permissionLevel: "auto",
  },
  send,
  codeReferenceRequest,
  onCodeReferencesChange,
  isSendDisabled = false,
  serverManagedQueue = false,
  children,
}: {
  activeExecutionId?: string;
  onMessageAccepted?: () => void;
  onMessageRejected?: (selection: ChatSendSelection) => void;
  selection?: ChatSendSelection;
  send: SendMessage;
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  isSendDisabled?: boolean;
  serverManagedQueue?: boolean;
  children?: ReactNode;
}) {
  const [clearRevision, clearComposer] = useReducer(
    (revision: number) => revision + 1,
    0,
  );
  const [restoreRevision, restoreComposer] = useReducer(
    (revision: number) => revision + 1,
    0,
  );
  const { runtime } = useChatRuntime({
    conversationKey: conversationKeyOf("scope:chat-1", "chat-1"),
    queueKey: "chat-1",
    chatId: "chat-1",
    messages: [],
    latestSequence: 0,
    activeExecutionId,
    isHistoryReady: true,
    isLoading: false,
    isSendDisabled,
    serverManagedQueue,
    attachmentOnlyText: "请查看附件。",
    incompleteErrorText: "执行未完成",
    selection,
    onMessageAccepted: () => {
      clearComposer();
      onMessageAccepted?.();
    },
    onMessageRejected: (rejectedSelection) => {
      restoreComposer();
      onMessageRejected?.(rejectedSelection);
    },
    send,
    cancel: vi.fn(),
    onStarted: vi.fn(),
    onTerminal: vi.fn(),
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root>
        <ThreadPrimitive.Messages>
          {() => (
            <MessagePrimitive.Root>
              <MessagePrimitive.Parts />
            </MessagePrimitive.Root>
          )}
        </ThreadPrimitive.Messages>
        {children ?? (
          <ComposerControls
            codeReferenceRequest={codeReferenceRequest}
            onCodeReferencesChange={onCodeReferencesChange}
            clearRevision={clearRevision}
            restoreRevision={restoreRevision}
          />
        )}
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

function CodeReferenceComposerHarness({ send }: { send: SendMessage }) {
  const [codeReferences, setCodeReferences] = useState<ChatCodeReference[]>([]);
  return (
    <ComposerHarness
      send={send}
      selection={{
        runtimeId: "codex",
        model: "gpt-5",
        reasoning: "high",
        permissionLevel: "auto",
        codeReferences,
      }}
      codeReferenceRequest={{
        id: "reference-send",
        path: "server/main.go",
        startLine: 17,
        endLine: 18,
        content: "func main() {}",
      }}
      onCodeReferencesChange={setCodeReferences}
    />
  );
}

function HistoryHarness({
  chatId,
  messages,
  historyReady = true,
}: {
  chatId: string;
  messages: ChatMessage[];
  historyReady?: boolean;
}) {
  const { runtime } = useChatRuntime({
    conversationKey: conversationKeyOf(`scope:${chatId}`, chatId),
    queueKey: chatId,
    chatId,
    messages,
    latestSequence: 0,
    isHistoryReady: historyReady,
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
  });

  return (
    <AssistantRuntimeProvider
      key={`${chatId}:${historyReady ? "ready" : "loading"}`}
      runtime={runtime}
    >
      <ThreadPrimitive.Root>
        <ThreadPrimitive.Messages>
          {() => (
            <MessagePrimitive.Root>
              <MessagePrimitive.Parts />
            </MessagePrimitive.Root>
          )}
        </ThreadPrimitive.Messages>
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

function KeyedHistoryHarness(props: ComponentProps<typeof HistoryHarness>) {
  return <HistoryHarness key={props.chatId} {...props} />;
}

function ChatThreadLayoutHarness({
  activeExecutionId,
  activityStatus,
  isWorkRunning = false,
  isSendDisabled = false,
  cancel = vi.fn(),
  onStopWork,
  pendingApprovals = [],
  requestedInteractionId,
  send = vi.fn(),
  sendFailure = { kind: "none" },
  terminalEnabled = false,
  onOpenTerminal,
}: {
  activeExecutionId?: string;
  activityStatus?: ComponentProps<typeof ChatThread>["activityStatus"];
  cancel?: () => Promise<void>;
  isWorkRunning?: boolean;
  isSendDisabled?: boolean;
  onStopWork?: () => void;
  pendingApprovals?: ComponentProps<typeof ChatThread>["pendingApprovals"];
  requestedInteractionId?: string;
  send?: SendMessage;
  sendFailure?: ComponentProps<typeof ChatThread>["sendFailure"];
  terminalEnabled?: boolean;
  onOpenTerminal?: () => void;
}) {
  const [clearRevision, clearComposer] = useReducer(
    (revision: number) => revision + 1,
    0,
  );
  const { isRunning, runtime } = useChatRuntime({
    conversationKey: conversationKeyOf("scope:chat-layout", "chat-layout"),
    queueKey: "chat-layout",
    chatId: "chat-layout",
    messages: [],
    latestSequence: 0,
    activeExecutionId,
    isHistoryReady: true,
    isLoading: false,
    isSendDisabled,
    attachmentOnlyText: "请查看附件。",
    incompleteErrorText: "执行未完成",
    selection: {
      runtimeId: "codex",
      model: "gpt-5",
      reasoning: "high",
      permissionLevel: "auto",
    },
    send,
    cancel,
    onMessageAccepted: clearComposer,
    onStarted: vi.fn(),
    onTerminal: vi.fn(),
  });

  return (
    <ChatThread
      runtime={runtime}
      wakerName="测试伙伴"
      emptyState={<div>空对话</div>}
      renderComposerContextStrip={() => (
        <div data-chat-composer-context-strip>
          <span data-testid="composer-workspace-control" />
          <span data-testid="composer-permission-control" />
        </div>
      )}
      composerSettings={<span data-testid="composer-runtime-control" />}
      composerClearRevision={clearRevision}
      pendingApprovals={pendingApprovals}
      requestedInteractionId={requestedInteractionId}
      terminalEnabled={terminalEnabled}
      onOpenTerminal={onOpenTerminal}
      activityStatus={activityStatus}
      isRunning={isRunning}
      isWorkRunning={isWorkRunning}
      onStopWork={onStopWork}
      sendFailure={sendFailure}
      hasEarlierMessages={false}
      isLoadingEarlier={false}
      onLoadEarlier={async () => undefined}
    />
  );
}

function ChatThreadHistoryHarness({
  chatId = "chat-thread-history",
  messages,
  onOpenCodeReference,
  onOpenFile,
}: {
  chatId?: string;
  messages: ChatMessage[];
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: { name: string; path: string }) => void;
}) {
  const { runtime } = useChatRuntime({
    conversationKey: conversationKeyOf(`scope:${chatId}`, chatId),
    queueKey: chatId,
    chatId,
    messages,
    latestSequence: 0,
    activeExecutionId: messages.some((message) => message.status === "running")
      ? "execution-1"
      : undefined,
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
  });

  return (
    <ChatThread
      runtime={runtime}
      wakerName="测试伙伴"
      emptyState={<div>空对话</div>}
      renderComposerContextStrip={undefined}
      sendFailure={{ kind: "none" }}
      hasEarlierMessages={false}
      isLoadingEarlier={false}
      onLoadEarlier={async () => undefined}
      onOpenCodeReference={onOpenCodeReference}
      onOpenFile={onOpenFile}
    />
  );
}

describe("chat composer", () => {
  it("历史滚动区关闭浏览器原生锚定，避免与消息锚点补偿竞争", () => {
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness
          messages={[{ id: "message-1", role: "user", content: "历史消息" }]}
        />
      </ChatQueueProvider>,
    );
    expect(container.querySelector("[data-chat-scroll-viewport]")).toHaveClass(
      "[overflow-anchor:none]",
    );
  });
  it("@ 文件搜索期间显示加载状态而不提前展示无匹配", async () => {
    const user = userEvent.setup();
    let finishSearch: ((entries: []) => void) | undefined;
    scopedSearch.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishSearch = resolve;
        }),
    );
    workspaceCapability.available = true;
    try {
      render(
        <ChatQueueProvider>
          <ComposerHarness send={vi.fn()}>
            <ComposerPrimitive.Root>
              <ChatComposerInput
                placeholder="输入消息"
                ariaLabel="message"
                fileRoots={["/tmp/ally"]}
                fileScopePath="projects"
                fileScopeChatId="chat-1"
              />
            </ComposerPrimitive.Root>
          </ComposerHarness>
        </ChatQueueProvider>,
      );
      await user.type(
        await screen.findByRole("textbox", { name: "message" }),
        "@",
      );
      expect(await screen.findByRole("status")).toHaveTextContent(
        "正在扫描工作区文件",
      );
      expect(
        screen.queryByText("没有匹配的工作区文件"),
      ).not.toBeInTheDocument();
      finishSearch?.([]);
      expect(
        await screen.findByText("没有匹配的工作区文件"),
      ).toBeInTheDocument();
    } finally {
      workspaceCapability.available = false;
    }
  });

  it("将右侧文件拖入输入框会插入与 @ 相同的文件引用", async () => {
    workspaceCapability.available = true;
    try {
      render(
        <ChatQueueProvider>
          <ComposerHarness send={vi.fn()}>
            <ComposerPrimitive.Root>
              <ChatComposerInput
                placeholder="输入消息"
                ariaLabel="message"
                fileRoots={["/tmp/ally"]}
                fileScopeChatId="chat-1"
              />
            </ComposerPrimitive.Root>
          </ComposerHarness>
        </ChatQueueProvider>,
      );
      const target = await waitFor(() => {
        const element = document.querySelector<HTMLElement>(
          "[data-chat-composer-dropzone]",
        );
        expect(element).not.toBeNull();
        return element as HTMLElement;
      });
      const dataTransfer = {
        types: [workspaceFileDragType],
        dropEffect: "none",
        getData: () =>
          JSON.stringify({
            chatId: "chat-1",
            rootPath: "/tmp/ally",
            path: "projects/ally/AGENTS.md",
          }),
      };
      fireEvent.dragOver(target, { dataTransfer });
      fireEvent.drop(target, { dataTransfer });
      expect(scopedResolve).toHaveBeenCalledWith(
        "chat-1",
        "projects/ally/AGENTS.md",
        "/tmp/ally",
      );
      await waitFor(() =>
        expect(
          document.querySelector("[data-chat-composer-code-reference]"),
        ).toHaveAttribute(
          "data-chat-composer-code-reference",
          "/tmp/project/AGENTS.md",
        ),
      );
    } finally {
      workspaceCapability.available = false;
    }
  });

  it("Chat 的 @ 文件建议从授权资源范围搜索", async () => {
    const user = userEvent.setup();
    scopedResolve.mockClear();
    scopedSearch.mockImplementation(async () => [
      {
        path: "/tmp/project/AGENTS.md",
        relativePath: "projects/ally/AGENTS.md",
      },
    ]);
    workspaceCapability.available = true;
    try {
      render(
        <ChatQueueProvider>
          <ComposerHarness send={vi.fn()}>
            <ComposerPrimitive.Root>
              <ChatComposerInput
                placeholder="输入消息"
                ariaLabel="message"
                fileRoots={["/tmp/ally"]}
                fileScopePath="projects"
                fileScopeChatId="chat-1"
              />
            </ComposerPrimitive.Root>
          </ComposerHarness>
        </ChatQueueProvider>,
      );
      await user.type(
        await screen.findByRole("textbox", { name: "message" }),
        "@AGENTS",
      );
      await waitFor(() =>
        expect(scopedSearch).toHaveBeenCalledWith(
          "chat-1",
          "AGENTS",
          20,
          "projects",
          "/tmp/ally",
        ),
      );
      expect(
        await screen.findByRole("option", { name: /AGENTS\.md/ }),
      ).toBeInTheDocument();
      expect(scopedResolve).toHaveBeenCalledWith(
        "chat-1",
        "projects/ally/AGENTS.md",
        "/tmp/ally",
      );
    } finally {
      scopedSearch.mockImplementation(async () => []);
      workspaceCapability.available = false;
    }
  });

  it("在 @ 文件建议菜单内选择一个团队成员并输出结构化成员标识", async () => {
    const user = userEvent.setup();
    const onMemberMentionChange = vi.fn();
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <MemberMentionComposerHarness
            onMemberMentionChange={onMemberMentionChange}
          />
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    await user.type(
      await screen.findByRole("textbox", { name: "message" }),
      "@项目",
    );
    await user.click(
      await screen.findByRole("option", { name: /项目经理.*团队成员/ }),
    );

    expect(
      document.querySelector('[data-chat-composer-member="member-1"]'),
    ).toHaveTextContent("@项目经理");
    expect(onMemberMentionChange).toHaveBeenLastCalledWith("member-1");
  });

  it("添加本地文件后在输入框内显示并可移除附件", async () => {
    const user = userEvent.setup();
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <AttachmentComposerHarness />
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "message" });
    const attachment = await waitFor(() => {
      const element = document.querySelector<HTMLElement>(
        "[data-chat-composer-attachment]",
      );
      expect(element).toHaveTextContent("readme.md");
      return element;
    });
    expect(input.parentElement?.parentElement).toContainElement(attachment);

    await user.click(screen.getByRole("button", { name: "移除附件" }));
    await waitFor(() =>
      expect(
        document.querySelector("[data-chat-composer-attachment]"),
      ).not.toBeInTheDocument(),
    );
  });

  it("DOM 文件拖放交给原生路径事件处理，不重复加入无路径文件", async () => {
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <EmptyAttachmentComposerHarness />
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    const dropzone = await waitFor(() => {
      const element = document.querySelector<HTMLElement>(
        "[data-chat-composer-dropzone]",
      );
      expect(element).not.toBeNull();
      return element;
    });
    const dropEvent = new Event("drop", {
      bubbles: true,
      cancelable: true,
    });
    Object.defineProperty(dropEvent, "dataTransfer", {
      value: {
        files: [pathFile("/tmp/ally/dropped.txt")],
        types: ["Files"],
      },
    });
    await act(async () => {
      dropzone?.dispatchEvent(dropEvent);
    });

    expect(
      document.querySelector(
        '[data-chat-composer-attachment="/tmp/ally/dropped.txt"]',
      ),
    ).toBeNull();
  });

  it("原生文件拖入图片后立即显示，移除后立即消失", async () => {
    const user = userEvent.setup();
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    const dropzone = document.querySelector<HTMLElement>(
      "[data-chat-composer-surface]",
    );
    if (!dropzone) throw new Error("缺少附件拖入区域");
    vi.spyOn(dropzone, "getBoundingClientRect").mockReturnValue({
      bottom: 100,
      height: 100,
      left: 0,
      right: 100,
      top: 0,
      width: 100,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    nativeDrop.onEvent?.({
      type: "drop",
      paths: ["/tmp/ally/native.png"],
      position: { x: 50, y: 50 },
    });
    const domDrop = new Event("drop", { bubbles: true, cancelable: true });
    Object.defineProperty(domDrop, "dataTransfer", {
      value: { files: [new File([], "native.png")], types: ["Files"] },
    });
    document
      .querySelector("[data-chat-composer-dropzone]")
      ?.dispatchEvent(domDrop);

    expect(await screen.findByText("native.png")).toBeVisible();
    expect(
      document.querySelector("[data-chat-composer-surface]"),
    ).toHaveAttribute("data-expanded", "true");
    await user.click(screen.getByRole("button", { name: "移除附件" }));
    await waitFor(() => expect(screen.queryByText("native.png")).toBeNull());
    expect(
      document.querySelector("[data-chat-composer-surface]"),
    ).not.toHaveAttribute("data-expanded");
  });

  it("高分屏按 CSS 坐标命中输入框", async () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );
    const dropzone = document.querySelector<HTMLElement>(
      "[data-chat-composer-surface]",
    );
    if (!dropzone) throw new Error("缺少附件拖入区域");
    vi.spyOn(dropzone, "getBoundingClientRect").mockReturnValue({
      bottom: 100,
      height: 50,
      left: 50,
      right: 100,
      top: 50,
      width: 50,
      x: 50,
      y: 50,
      toJSON: () => ({}),
    });
    const pixelRatio = Object.getOwnPropertyDescriptor(
      window,
      "devicePixelRatio",
    );
    Object.defineProperty(window, "devicePixelRatio", {
      configurable: true,
      value: 2,
    });
    try {
      await act(async () => {
        nativeDrop.onEvent?.({
          type: "over",
          paths: [],
          position: { x: 75, y: 75 },
        });
      });
      expect(
        document.querySelector("[data-chat-composer-dropzone]"),
      ).toHaveAttribute("data-file-dragging", "true");
      nativeDrop.onEvent?.({
        type: "drop",
        paths: ["/tmp/ally/high-dpi.png"],
        position: { x: 75, y: 75 },
      });
      await waitFor(() =>
        expect(screen.getByText("high-dpi.png")).toBeVisible(),
      );
      expect(
        document.querySelector("[data-chat-composer-dropzone]"),
      ).toHaveAttribute("data-file-dragging", "false");
    } finally {
      if (pixelRatio) {
        Object.defineProperty(window, "devicePixelRatio", pixelRatio);
      }
    }
  });

  it("输入框底部操作区也能接收文件", async () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );
    const dropzone = document.querySelector<HTMLElement>(
      "[data-chat-composer-dropzone]",
    );
    const surface = document.querySelector<HTMLElement>(
      "[data-chat-composer-surface]",
    );
    if (!dropzone || !surface) throw new Error("缺少输入框拖入区域");
    vi.spyOn(dropzone, "getBoundingClientRect").mockReturnValue({
      bottom: 70,
      height: 70,
      left: 0,
      right: 100,
      top: 0,
      width: 100,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    vi.spyOn(surface, "getBoundingClientRect").mockReturnValue({
      bottom: 100,
      height: 100,
      left: 0,
      right: 100,
      top: 0,
      width: 100,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    nativeDrop.onEvent?.({
      type: "drop",
      paths: ["/tmp/ally/toolbar.txt"],
      position: { x: 50, y: 85 },
    });
    expect(await screen.findByText("toolbar.txt")).toBeVisible();
  });

  it("附件通知延迟时，拖入和删除仍立即刷新输入框", async () => {
    const user = userEvent.setup();
    let composer: ReturnType<typeof useAui>["composer"] | undefined;
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <ComposerPrimitive.Root>
            <AttachmentSubscriptionGapHarness
              onComposerReady={(value) => {
                composer = value;
              }}
            />
          </ComposerPrimitive.Root>
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    const dropzone = document.querySelector<HTMLElement>(
      "[data-chat-composer-dropzone]",
    );
    if (!dropzone) throw new Error("缺少附件拖入区域");
    vi.spyOn(dropzone, "getBoundingClientRect").mockReturnValue({
      bottom: 100,
      height: 100,
      left: 0,
      right: 100,
      top: 0,
      width: 100,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    nativeDrop.onEvent?.({
      type: "drop",
      paths: ["/tmp/ally/native.png"],
      position: { x: 50, y: 50 },
    });

    await waitFor(() =>
      expect(composer?.getState().attachments).toHaveLength(1),
    );
    expect(screen.getByText("native.png")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "移除附件" }));
    await waitFor(() =>
      expect(composer?.getState().attachments).toHaveLength(0),
    );
    expect(screen.queryByText("native.png")).toBeNull();
  });

  it("附件通知延迟时，点击 X 后无需再输入文字即可移除图片", async () => {
    const user = userEvent.setup();
    let composer: ReturnType<typeof useAui>["composer"] | undefined;
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <ComposerPrimitive.Root>
            <AttachmentSubscriptionGapHarness
              onComposerReady={(value) => {
                composer = value;
              }}
            />
          </ComposerPrimitive.Root>
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    await act(async () => {
      await composer?.addAttachment(pathFile("/tmp/ally/remove.png"));
    });
    await user.type(screen.getByRole("textbox", { name: "message" }), "看图");
    expect(await screen.findByText("remove.png")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "移除附件" }));
    await waitFor(() =>
      expect(composer?.getState().attachments).toHaveLength(0),
    );
    expect(screen.queryByText("remove.png")).toBeNull();
  });

  it("删除结果晚于回调返回时，单击 X 仍能移除附件", async () => {
    const user = userEvent.setup();
    let composer: ReturnType<typeof useAui>["composer"] | undefined;
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <ComposerPrimitive.Root>
            <AttachmentSubscriptionGapHarness
              deferRemoval
              onComposerReady={(value) => {
                composer = value;
              }}
            />
          </ComposerPrimitive.Root>
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    await act(async () => {
      await composer?.addAttachment(pathFile("/tmp/ally/remove-once.png"));
    });
    await user.type(screen.getByRole("textbox", { name: "message" }), "看图");
    expect(await screen.findByText("remove-once.png")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "移除附件" }));
    await waitFor(() => {
      expect(composer?.getState().attachments).toHaveLength(0);
      expect(screen.queryByText("remove-once.png")).toBeNull();
    });
  });

  it("点击输入框附件打开对应的本地文件预览请求", async () => {
    const onOpenFile = vi.fn();
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <AttachmentComposerHarness onOpenFile={onOpenFile} />
        </ComposerHarness>
      </ChatQueueProvider>,
    );

    const attachment = await screen.findByText("readme.md");
    await userEvent.setup().click(attachment);
    expect(onOpenFile).toHaveBeenCalledWith({
      name: "readme.md",
      path: "/tmp/ally/readme.md",
      rootPath: "/tmp/ally",
    });
  });

  it("点击已发送消息附件打开工作区文件预览请求", async () => {
    const onOpenFile = vi.fn();
    render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness
          onOpenFile={onOpenFile}
          messages={[
            {
              id: "user-attachment",
              role: "user",
              content: "请查看附件",
              attachments: [
                {
                  name: "report.md",
                  path: "attachments/user-attachment/report.md",
                },
              ],
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    await userEvent.setup().click(await screen.findByText("report.md"));
    expect(onOpenFile).toHaveBeenCalledWith({
      name: "report.md",
      path: "attachments/user-attachment/report.md",
    });
  });

  it("已发送整文件代码引用按引用渲染而不是消息附件", async () => {
    const onOpenCodeReference = vi.fn();
    const onOpenFile = vi.fn();
    render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness
          onOpenCodeReference={onOpenCodeReference}
          onOpenFile={onOpenFile}
          messages={[
            {
              id: "user-reference",
              role: "user",
              content: "检查文件",
              attachments: [],
              codeReferences: [
                {
                  path: "projects/ally/server/service.go",
                  startLine: 0,
                  endLine: 0,
                  content: "",
                },
              ],
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    await userEvent
      .setup()
      .click(await screen.findByText("projects/ally/server/service.go"));
    expect(onOpenCodeReference).toHaveBeenCalledWith(
      expect.objectContaining({ path: "projects/ally/server/service.go" }),
    );
    expect(onOpenFile).not.toHaveBeenCalled();
  });

  it("发送被运行时接受后清空 TipTap 输入框", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: { id: "execution-1" },
    } as Awaited<ReturnType<SendMessage>>);
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    await user.type(input, "这段内容应在发送后清空");
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(input).toHaveValue(""));
  });

  it("粘贴图片和文件形成附件，并能不输入正文直接发送", async () => {
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: { id: "execution-1" },
    } as Awaited<ReturnType<SendMessage>>);
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );
    const files = [
      new File(["image"], "截图.png", { type: "image/png" }),
      new File(["hello"], "资料.txt", { type: "text/plain" }),
    ];
    fireEvent.paste(screen.getByRole("textbox", { name: "message" }), {
      clipboardData: { files },
    });
    expect(await screen.findByText("截图.png")).toBeVisible();
    expect(await screen.findByText("资料.txt")).toBeVisible();
    const sendButton = screen.getByRole("button", { name: "send" });
    await waitFor(() => expect(sendButton).toBeEnabled());
    await userEvent.setup().click(sendButton);
    await waitFor(() =>
      expect(sendMessage).toHaveBeenCalledWith(
        "请查看附件。",
        "chat-1",
        ["/tmp/ally-paste/截图.png", "/tmp/ally-paste/资料.txt"],
        expect.any(Object),
      ),
    );
  });

  it("禁用的聊天输入框不接收剪贴板附件", async () => {
    attachmentStaging.mockClear();
    render(
      <ChatQueueProvider>
        <ComposerHarness send={vi.fn()}>
          <ComposerPrimitive.Root>
            <ChatComposerInput
              disabled
              placeholder="输入消息"
              ariaLabel="message"
            />
          </ComposerPrimitive.Root>
        </ComposerHarness>
      </ChatQueueProvider>,
    );
    fireEvent.paste(screen.getByRole("textbox", { name: "message" }), {
      clipboardData: {
        files: [new File(["image"], "image.png", { type: "image/png" })],
      },
    });
    expect(attachmentStaging).not.toHaveBeenCalled();
    expect(screen.queryByText("image.png")).toBeNull();
  });

  it("粘贴文本后立即启用发送并提交粘贴内容", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: { id: "execution-1" },
    } as Awaited<ReturnType<SendMessage>>);
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    await user.click(input);
    await user.paste("通过剪贴板粘贴的内容");
    const sendButton = screen.getByRole("button", { name: "send" });

    await waitFor(() => expect(sendButton).toBeEnabled());
    await user.click(sendButton);
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
  });

  it("先输入文本再解除工作区门禁时保留草稿并启用发送", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: { id: "execution-1" },
    } as Awaited<ReturnType<SendMessage>>);
    const view = render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} isSendDisabled />
      </ChatQueueProvider>,
    );
    const input = screen.getByRole("textbox", { name: "message" });
    await user.type(input, "先填写任务，再选择工作目录");
    expect(screen.getByRole("button", { name: "send" })).toBeDisabled();

    view.rerender(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} isSendDisabled={false} />
      </ChatQueueProvider>,
    );

    expect(input).toHaveValue("先填写任务，再选择工作目录");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "send" })).toBeEnabled(),
    );
  });

  it("工作区门禁生效时按回车不会清空 ChatThread 草稿", async () => {
    const user = userEvent.setup();
    const send = vi.fn<SendMessage>();
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness isSendDisabled send={send} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox");
    await user.type(input, "先保留这段草稿{Enter}");

    expect(input).toHaveValue("先保留这段草稿");
    expect(send).not.toHaveBeenCalled();
  });

  it("空输入时发送按钮保持禁用", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    expect(screen.getByRole("button", { name: "发送消息" })).toBeDisabled();
  });

  it("待发送消息和团队状态复用工作范围顶栏，在同一个 Composer 中向上扩展", async () => {
    const user = userEvent.setup();
    const cancel = vi.fn(async () => undefined);
    const send = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-layout" },
      execution: { id: "execution-2" },
    } as Awaited<ReturnType<SendMessage>>);
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          activeExecutionId="execution-1"
          activityStatus={<div role="status">团队仍在执行</div>}
          cancel={cancel}
          send={send}
        />
      </ChatQueueProvider>,
    );

    expect(
      container.querySelector('[data-chat-primary-action="stop"]'),
    ).toBeInTheDocument();
    expect(
      container.querySelector('[data-chat-primary-action="send"]'),
    ).not.toBeInTheDocument();

    const input = screen.getByRole("textbox");
    await user.type(input, "排入下一项任务");
    await waitFor(() =>
      expect(
        container.querySelector('[data-chat-primary-action="send"]'),
      ).toBeInTheDocument(),
    );
    expect(
      container.querySelector('[data-chat-primary-action="stop"]'),
    ).not.toBeInTheDocument();

    const queueButton = container.querySelector<HTMLButtonElement>(
      '[data-chat-primary-action="send"]',
    );
    if (!queueButton) throw new Error("入队按钮未渲染");
    await user.click(queueButton);
    await waitFor(() =>
      expect(
        container.querySelector("[data-chat-queued-message]"),
      ).toHaveTextContent("排入下一项任务"),
    );
    await user.type(input, "另一条待发送消息");
    await user.click(screen.getByRole("button", { name: "加入发送队列" }));
    await waitFor(() =>
      expect(
        container.querySelectorAll("[data-chat-queued-message]"),
      ).toHaveLength(2),
    );
    await user.type(input, "正在编辑的草稿");

    const stack = container.querySelector("[data-chat-composer-stack]");
    const queueList = container.querySelector("[data-chat-queue-list]");
    const queuedMessages = container.querySelectorAll<HTMLElement>(
      "[data-chat-queued-message]",
    );
    expect(queuedMessages).toHaveLength(2);
    const firstQueuedMessage = queuedMessages[0];
    const secondQueuedMessage = queuedMessages[1];
    if (!firstQueuedMessage || !secondQueuedMessage)
      throw new Error("待发送消息叠层未渲染");
    const composerSurface = container.querySelector(
      "[data-chat-composer-surface]",
    );
    expect(stack).toContainElement(screen.getByText("团队仍在执行"));
    expect(stack).toContainElement(queueList as HTMLElement);
    expect(stack).toContainElement(composerSurface as HTMLElement);
    expect(composerSurface).not.toContainElement(queueList as HTMLElement);
    expect(queueList).toHaveClass("max-h-60", "overflow-y-auto");
    const layers = container.querySelector("[data-chat-composer-layer-stack]");
    const frame = container.querySelector("[data-chat-composer-frame]");
    const workspaceRow = frame?.querySelector<HTMLElement>(
      ":scope > [data-chat-composer-context-bar]",
    );
    if (!workspaceRow) throw new Error("工作范围顶栏未渲染");
    expect(stack).toBe(frame);
    expect(frame).toHaveClass("bg-composer-context", "rounded-2xl");
    expect(frame).toContainElement(queueList as HTMLElement);
    expect(layers).not.toHaveClass("bg-surface-raised", "mx-2", "border");
    expect(layers).toContainElement(queueList as HTMLElement);
    expect(layers).toContainElement(
      container.querySelector("[data-chat-work-status]"),
    );
    for (const row of queuedMessages) {
      expect(row.className).toBe(workspaceRow.className);
    }
    expect(container.querySelector("[data-chat-work-status]")?.className).toBe(
      workspaceRow.className,
    );
    expect(container.querySelector("[data-chat-queue-panel]")).toBeNull();
    expect(
      Array.from(
        stack?.querySelectorAll("[data-chat-composer-context-bar]") ?? [],
      ),
    ).toEqual([
      ...queuedMessages,
      container.querySelector("[data-chat-work-status]"),
      container.querySelector(
        "[data-chat-composer-frame] > [data-chat-composer-context-bar]",
      ),
    ]);

    await user.click(
      within(secondQueuedMessage).getByRole("button", { name: "移出队列" }),
    );
    expect(queueList).not.toHaveTextContent("另一条待发送消息");
    expect(input).toHaveTextContent("正在编辑的草稿");
    expect(send).not.toHaveBeenCalled();

    await user.click(
      within(firstQueuedMessage).getByRole("button", { name: "立即发送" }),
    );
    await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
    act(() => {
      chatSubscription.onEvent?.({
        sequence: 1,
        type: "execution.canceled",
        executionStatus: "canceled",
      });
    });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(send.mock.calls[0]?.[0]).toBe("排入下一项任务");
    await waitFor(() =>
      expect(container.querySelector("[data-chat-queue-list]")).toBeNull(),
    );
    expect(input).toHaveTextContent("正在编辑的草稿");
  });

  it("超过三条待发送消息自动收拢，展开与内滚动不影响草稿，减少后重新收拢", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness activeExecutionId="execution-1" />
      </ChatQueueProvider>,
    );
    const input = screen.getByRole("textbox");
    const enqueue = async (text: string) => {
      await user.type(input, text);
      await user.click(screen.getByRole("button", { name: "加入发送队列" }));
      await waitFor(() => expect(input.textContent).toBe(""));
    };
    for (let index = 1; index <= 4; index++) await enqueue(`待发送${index}`);
    await user.type(input, "保留的草稿");
    const viewport = screen.getByRole("region", { name: "4 条待发送消息" });
    const foldButton = screen.getByRole("button", { name: "展开待发送消息" });
    expect(foldButton).toHaveAttribute("aria-expanded", "false");
    expect(foldButton).toHaveAttribute("aria-controls", viewport.id);
    expect(viewport).toHaveClass(
      "max-h-20",
      "overflow-y-auto",
      "overscroll-contain",
    );
    expect(
      viewport.querySelectorAll("[data-chat-queued-message]"),
    ).toHaveLength(4);
    expect(
      container.querySelector("[data-chat-composer-surface]"),
    ).not.toContainElement(viewport);
    fireEvent.scroll(viewport, { target: { scrollTop: 40 } });
    expect(viewport.scrollTop).toBe(40);
    expect(input).toHaveTextContent("保留的草稿");

    await user.click(foldButton);
    expect(
      screen.getByRole("button", { name: "收起待发送消息" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(viewport).toHaveClass("max-h-60", "overflow-y-auto");
    await user.click(screen.getByRole("button", { name: "收起待发送消息" }));
    expect(viewport).toHaveClass("max-h-20");
    await user.click(screen.getByRole("button", { name: "展开待发送消息" }));

    const removeButton = within(viewport).getAllByRole("button", {
      name: "移出队列",
    })[0];
    if (!removeButton) throw new Error("队列移除操作未渲染");
    await user.click(removeButton);
    expect(
      screen.queryByRole("button", { name: "收起待发送消息" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "3 条待发送消息" })).toHaveClass(
      "max-h-60",
    );
    expect(input).toHaveTextContent("保留的草稿");
    await user.clear(input);
    await enqueue("再次超过上限");
    expect(
      screen.getByRole("button", { name: "展开待发送消息" }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("region", { name: "4 条待发送消息" })).toHaveClass(
      "max-h-20",
    );
  });

  it("团队后台仍在执行时沿用普通输入框的停止与追加要求互斥逻辑", async () => {
    const user = userEvent.setup();
    const onStopWork = vi.fn();
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          activityStatus={
            <TeamWorkStatus
              progress={{
                phase: "executing",
                activeMemberName: "Aa",
                activeTaskSubject: "整理今日 AI 新闻简报",
                review: 0,
                terminal: 0,
                total: 1,
              }}
            />
          }
          isWorkRunning
          onStopWork={onStopWork}
        />
      </ChatQueueProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("团队仍在执行");
    expect(screen.getByRole("status")).toHaveTextContent("Aa正在处理");
    expect(screen.getByRole("status")).toHaveTextContent(
      "整理今日 AI 新闻简报",
    );
    expect(
      container.querySelector("[data-chat-composer-stack]"),
    ).toContainElement(screen.getByRole("status"));
    const stop = container.querySelector<HTMLButtonElement>(
      '[data-chat-primary-action="stop"]',
    );
    expect(stop).toBeInTheDocument();
    if (!stop) throw new Error("停止按钮未渲染");
    await user.click(stop);
    expect(onStopWork).toHaveBeenCalledTimes(1);

    await user.type(screen.getByRole("textbox"), "再补充一个约束");
    await waitFor(() =>
      expect(
        container.querySelector('[data-chat-primary-action="send"]'),
      ).toBeInTheDocument(),
    );
    expect(
      container.querySelector('[data-chat-primary-action="stop"]'),
    ).not.toBeInTheDocument();
  });

  it("服务端托管队列时把运行中的补充立即交给后端", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
    });
    const { container } = render(
      <ChatQueueProvider>
        <ComposerHarness
          activeExecutionId="execution-1"
          send={sendMessage}
          serverManagedQueue
        />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    await user.type(input, "补充原任务上下文");
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(container.querySelector("[data-chat-queue-panel]")).toBeNull();
    await waitFor(() =>
      expect(screen.queryByText("补充原任务上下文")).not.toBeInTheDocument(),
    );
  });

  it("把代码选区作为原子引用插入，并可用 Backspace 整体删除", async () => {
    const onCodeReferencesChange = vi.fn();
    render(
      <ChatQueueProvider>
        <ComposerHarness
          send={vi.fn()}
          codeReferenceRequest={{
            id: "reference-1",
            path: "server/main.go",
            startLine: 17,
            endLine: 18,
            content: "func main() {}",
          }}
          onCodeReferencesChange={onCodeReferencesChange}
        />
      </ChatQueueProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "message" });
    expect(
      input.querySelector('[data-chat-composer-reference-type="code"]'),
    ).toHaveTextContent("server/main.go (17-18)");
    expect(onCodeReferencesChange).toHaveBeenLastCalledWith([
      expect.objectContaining({
        path: "server/main.go",
        startLine: 17,
        endLine: 18,
      }),
    ]);

    fireEvent.keyDown(input, { key: "Backspace" });
    await waitFor(() =>
      expect(
        input.querySelector('[data-chat-composer-reference-type="code"]'),
      ).not.toBeInTheDocument(),
    );
    expect(onCodeReferencesChange).toHaveBeenLastCalledWith([]);
  });

  it("添加选区保留已有草稿并聚焦输入框，相同请求只插入一次", async () => {
    const user = userEvent.setup();
    const send = vi.fn();
    const onCodeReferencesChange = vi.fn();
    const reference = {
      id: "selected-lines",
      path: "server/main.go",
      startLine: 2,
      endLine: 3,
      content: "selected source",
    };
    const view = (request?: ChatCodeReference) => (
      <ChatQueueProvider>
        <ComposerHarness
          send={send}
          codeReferenceRequest={request}
          onCodeReferencesChange={onCodeReferencesChange}
        />
      </ChatQueueProvider>
    );
    const { rerender } = render(view());
    const input = await screen.findByRole("textbox", { name: "message" });
    await user.type(input, "解释这段代码");
    rerender(view(reference));
    await waitFor(() => {
      expect(input).toHaveTextContent("解释这段代码");
      expect(
        input.querySelectorAll('[data-chat-composer-reference-type="code"]'),
      ).toHaveLength(1);
      expect(input).toHaveFocus();
    });
    rerender(view({ ...reference }));
    expect(
      input.querySelectorAll('[data-chat-composer-reference-type="code"]'),
    ).toHaveLength(1);
    expect(onCodeReferencesChange).toHaveBeenLastCalledWith([
      expect.objectContaining(reference),
    ]);
  });

  it("删除代码引用后发送不再携带已删除的引用", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: { id: "execution-1" },
    } as Awaited<ReturnType<SendMessage>>);
    render(
      <ChatQueueProvider>
        <CodeReferenceComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "message" });
    expect(
      input.querySelector('[data-chat-composer-reference-type="code"]'),
    ).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "Backspace" });
    await user.type(input, "继续");
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[3]?.codeReferences ?? []).toEqual([]);
  });

  it("shows the active execution conflict instead of the generic send failure", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness sendFailure={{ kind: "active-execution" }} />
      </ChatQueueProvider>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "当前对话仍有运行中的执行，请稍后重试。",
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "请检查项目目录和智能体状态",
    );
  });

  it("prefers backend error message over generic send failure copy", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          sendFailure={{
            kind: "rejected",
            message: "原生 Runtime 会话已失效，请新建会话后继续",
          }}
        />
      </ChatQueueProvider>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "原生 Runtime 会话已失效，请新建会话后继续",
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "请检查项目目录和智能体状态",
    );
  });

  it("keeps the composer outside the scrolling message viewport without a floating down arrow", () => {
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    const viewport = container.querySelector<HTMLElement>(
      "[data-chat-scroll-viewport]",
    );
    const composer = container.querySelector<HTMLElement>(
      "[data-chat-composer]",
    );
    expect(viewport).toBeInTheDocument();
    expect(composer).toBeInTheDocument();
    expect(viewport).not.toContainElement(composer);
    expect(viewport?.parentElement?.nextElementSibling).toBe(composer);
    expect(composer).not.toHaveClass("sticky");
    expect(composer?.querySelector("[data-chat-composer-surface]")).toHaveClass(
      "rounded-2xl",
    );
    expect(
      composer?.querySelector("[data-chat-composer-surface]"),
    ).not.toHaveClass("shadow-md");
    const surface = composer?.querySelector("[data-chat-composer-surface]");
    const contextStrip = composer?.querySelector(
      "[data-chat-composer-context-strip]",
    );
    const contextRow = composer?.querySelector(
      "[data-chat-composer-context-row]",
    );
    expect(contextStrip).toBeInTheDocument();
    expect(surface).not.toContainElement(contextStrip as HTMLElement);
    expect(contextStrip).toContainElement(
      screen.getByTestId("composer-workspace-control"),
    );
    expect(contextStrip).toContainElement(
      screen.getByTestId("composer-permission-control"),
    );
    expect(contextRow).toContainElement(
      screen.getByTestId("composer-runtime-control"),
    );
    expect(surface).not.toContainElement(
      screen.getByTestId("composer-runtime-control"),
    );
    expect(
      screen.queryByRole("button", { name: "滚动到底部" }),
    ).not.toBeInTheDocument();
  });

  it("旧待办链接直接恢复正常输入和发送，不显示失效提示", async () => {
    const user = userEvent.setup();
    const send = vi.fn<SendMessage>().mockResolvedValue({
      execution: { id: "execution-new" },
    });
    render(
      <MemoryRouter>
        <ChatQueueProvider>
          <ChatThreadLayoutHarness
            requestedInteractionId="expired"
            send={send}
          />
        </ChatQueueProvider>
      </MemoryRouter>,
    );
    expect(
      screen.queryByText("这项待办已处理或失效。"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "返回任务看板" }),
    ).not.toBeInTheDocument();
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "继续聊");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(send).toHaveBeenCalled());
  });

  it("旧待办失效但有新审批时仍保留审批操作和上下文", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          activeExecutionId="execution-1"
          requestedInteractionId="expired"
          pendingApprovals={[
            {
              approvalId: "approval-1",
              toolCallId: "tool-1",
              title: "允许执行命令吗？",
              kind: "command",
              options: [
                {
                  id: "reject-once",
                  name: "Reject once",
                  kind: "reject_once",
                },
                {
                  id: "allow-once",
                  name: "Allow once",
                  kind: "allow_once",
                },
              ],
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    expect(screen.getByText("允许执行命令吗？")).toBeInTheDocument();
    expect(
      screen.getByTestId("composer-workspace-control"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("composer-permission-control"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("composer-runtime-control")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "停止执行" }),
    ).not.toBeInTheDocument();
  });

  it("多个未决审批显示到达序号而不是写死 1/N", () => {
    const { rerender } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          activeExecutionId="execution-1"
          pendingApprovals={[
            {
              approvalId: "approval-1",
              toolCallId: "tool-1",
              title: "第一项审批",
              kind: "command",
              options: [
                {
                  id: "reject-once",
                  name: "Reject once",
                  kind: "reject_once",
                },
                {
                  id: "allow-once",
                  name: "Allow once",
                  kind: "allow_once",
                },
              ],
            },
            {
              approvalId: "approval-2",
              toolCallId: "tool-2",
              title: "第二项审批",
              kind: "edit",
              options: [
                {
                  id: "reject-once",
                  name: "Reject once",
                  kind: "reject_once",
                },
                {
                  id: "allow-once",
                  name: "Allow once",
                  kind: "allow_once",
                },
              ],
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    expect(screen.getByText("等待审批 · 1 / 2")).toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          activeExecutionId="execution-1"
          pendingApprovals={[
            {
              approvalId: "approval-2",
              toolCallId: "tool-2",
              title: "第二项审批",
              kind: "edit",
              options: [
                {
                  id: "reject-once",
                  name: "Reject once",
                  kind: "reject_once",
                },
                {
                  id: "allow-once",
                  name: "Allow once",
                  kind: "allow_once",
                },
              ],
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    expect(screen.getByText("等待审批 · 2 / 2")).toBeInTheDocument();
    expect(screen.getByText("第二项审批")).toBeInTheDocument();
  });

  it("长文本输入时输入框内部滚动，而不是溢出输入区", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    const input = document.querySelector<HTMLElement>(
      "[data-chat-composer-input]",
    );
    const surface = document.querySelector<HTMLElement>(
      "[data-chat-composer-surface]",
    );
    expect(input).toBeInTheDocument();
    expect(surface).toBeInTheDocument();
    expect(input).toHaveClass("max-h-40", "overflow-y-auto");
    expect(surface).toHaveClass("overflow-hidden");
  });

  it("空输入框通过占位样式展示功能提示", () => {
    render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    const input = document.querySelector<HTMLElement>(
      "[data-chat-composer-input]",
    );
    expect(input?.querySelector(".is-editor-empty")).toHaveAttribute(
      "data-placeholder",
      "输入消息…（@ 引用文件，/ 调用 Skill，# 使用 MCP；Enter 发送）",
    );
    expect(themeStyles).toMatch(
      /\[data-chat-composer-input\] p\.is-editor-empty:first-child::before\s*\{[^}]*content:\s*attr\(data-placeholder\)/s,
    );
  });

  it("只在已冻结工作范围的对话显示终端入口，并请求打开终端 Tab", async () => {
    const user = userEvent.setup();
    const onOpenTerminal = vi.fn();
    const { rerender } = render(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness />
      </ChatQueueProvider>,
    );

    expect(
      screen.queryByRole("button", { name: "打开终端" }),
    ).not.toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <ChatThreadLayoutHarness
          terminalEnabled
          onOpenTerminal={onOpenTerminal}
        />
      </ChatQueueProvider>,
    );

    const terminalButton = await screen.findByRole("button", {
      name: "打开终端",
    });
    await user.click(terminalButton);
    expect(onOpenTerminal).toHaveBeenCalledTimes(1);
    expect(
      screen
        .getByTestId("composer-runtime-control")
        .closest("[data-chat-composer]"),
    ).not.toHaveTextContent("终端面板");
  });

  it("允许从用户消息右下方复制完整命令", async () => {
    const user = userEvent.setup();
    const command = "bundle exec ruby script.rb --dry-run\necho finished";
    render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness
          messages={[{ id: "user-command", role: "user", content: command }]}
        />
      </ChatQueueProvider>,
    );

    const copyButton = await screen.findByRole("button", { name: "复制" });
    const messageLayout =
      copyButton.closest("[data-message-id]")?.firstElementChild;
    expect(messageLayout).toHaveClass("justify-items-end");
    expect(messageLayout?.lastElementChild).toContainElement(copyButton);

    await user.click(copyButton);

    await expect(navigator.clipboard.readText()).resolves.toBe(command);
  });

  it("用户消息不展示名称、头像和时间，并使用主题灰色气泡", async () => {
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness
          messages={[
            {
              id: "user-layout",
              role: "user",
              content: "问题",
              createdAt: Date.UTC(2026, 7, 9, 12, 0),
            },
            {
              id: "assistant-layout",
              role: "assistant",
              content: "回复",
              displayParts: [{ type: "text", text: "回复" }],
              createdAt: Date.UTC(2026, 7, 9, 12, 1),
            },
          ]}
        />
      </ChatQueueProvider>,
    );

    expect(
      await screen.findByText("回复", {}, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.queryByText("我")).not.toBeInTheDocument();
    expect(container.querySelector('img[src="/ally-logo.png"]')).toBeNull();
    expect(screen.getByText("测试伙伴")).toBeInTheDocument();
    const timestamps = container.querySelectorAll("time");
    expect(timestamps).toHaveLength(1);
    expect(timestamps[0]?.parentElement).toHaveClass(
      "grid",
      "justify-items-start",
    );
    const userMarkdown = container.querySelector('[data-chat-markdown="user"]');
    expect(userMarkdown?.closest(".bg-surface-tertiary")).not.toBeNull();
    expect(userMarkdown?.closest(".bg-primary")).toBeNull();
  });

  it("长对话可从轮次导航预览并跳转到指定用户消息", async () => {
    const messages: ChatMessage[] = [
      { id: "user-1", role: "user", content: "第一个问题" },
      {
        id: "assistant-1",
        role: "assistant",
        content: "第一个回答",
        displayParts: [{ type: "text", text: "第一个回答" }],
      },
      { id: "user-2", role: "user", content: "第二个问题的完整内容" },
      {
        id: "assistant-2",
        role: "assistant",
        content: "第二个回答的摘要内容",
        displayParts: [{ type: "text", text: "第二个回答的摘要内容" }],
      },
      { id: "user-3", role: "user", content: "第三个问题" },
      {
        id: "assistant-3",
        role: "assistant",
        content: "第三个回答",
        displayParts: [{ type: "text", text: "第三个回答" }],
      },
    ];
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness messages={messages} />
      </ChatQueueProvider>,
    );
    const viewport = container.querySelector<HTMLElement>(
      "[data-chat-scroll-viewport]",
    );
    expect(viewport).not.toBeNull();
    if (!viewport) throw new Error("缺少对话滚动容器");
    Object.defineProperties(viewport, {
      clientHeight: { configurable: true, value: 400 },
      scrollHeight: { configurable: true, value: 1_200 },
    });
    fireEvent.scroll(viewport);

    const turnNavigator = await screen.findByRole("slider", {
      name: "对话轮次导航",
    });
    expect(turnNavigator).toHaveAttribute("aria-valuemax", "3");
    const navigatorPositioner = turnNavigator.parentElement?.parentElement;
    expect(navigatorPositioner).toHaveClass("left-2");
    expect(navigatorPositioner).not.toHaveClass("max-w-3xl");
    expect(turnNavigator).toHaveClass("max-h-80", "overflow-y-auto");
    expect(
      turnNavigator.querySelectorAll("[data-chat-turn-marker]"),
    ).toHaveLength(3);
    expect(turnNavigator.querySelector("[data-chat-turn-marker]")).toHaveClass(
      "h-3",
    );
    vi.spyOn(turnNavigator, "getBoundingClientRect").mockReturnValue({
      bottom: 36,
      height: 36,
      left: 0,
      right: 32,
      top: 0,
      width: 32,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });

    fireEvent.mouseMove(turnNavigator, { clientY: 18 });
    const turnPreview = screen.getByRole("tooltip");
    expect(
      within(turnPreview).getByText("第二个问题的完整内容"),
    ).toBeInTheDocument();
    expect(
      within(turnPreview).getByText("第二个回答的摘要内容"),
    ).toBeInTheDocument();

    const targetMessage = container.querySelector<HTMLElement>(
      '[data-message-id="user-2"]',
    );
    expect(targetMessage).not.toBeNull();
    if (!targetMessage) throw new Error("缺少目标用户消息");
    targetMessage.scrollIntoView = vi.fn();
    fireEvent.click(turnNavigator, { clientY: 18 });
    expect(targetMessage.scrollIntoView).toHaveBeenCalledWith({
      behavior: "smooth",
      block: "start",
    });
  });

  it("超长对话的全部轮次标记保留在可滚动导航组件中", async () => {
    const messages: ChatMessage[] = Array.from({ length: 40 }, (_, index) => ({
      id: `user-${index + 1}`,
      role: "user",
      content: `第 ${index + 1} 个问题`,
    }));
    const { container } = render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness messages={messages} />
      </ChatQueueProvider>,
    );
    const viewport = container.querySelector<HTMLElement>(
      "[data-chat-scroll-viewport]",
    );
    if (!viewport) throw new Error("缺少对话滚动容器");
    Object.defineProperties(viewport, {
      clientHeight: { configurable: true, value: 400 },
      scrollHeight: { configurable: true, value: 4_000 },
    });
    fireEvent.scroll(viewport);

    const turnNavigator = await screen.findByRole("slider", {
      name: "对话轮次导航",
    });
    expect(turnNavigator).toHaveAttribute("aria-valuemax", "40");
    expect(turnNavigator).toHaveClass("max-h-80", "overflow-y-auto");
    expect(
      turnNavigator.querySelectorAll("[data-chat-turn-marker]"),
    ).toHaveLength(40);
  });

  it("keeps typed text and enables sending", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-1",
        userMessageId: "user-1",
        status: "pending",
      },
    });
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    const sendButton = screen.getByRole("button", { name: "send" });

    await user.type(input, "第二轮：只回复收到");

    expect(input).toHaveValue("第二轮：只回复收到");
    expect(sendButton).toBeEnabled();

    await user.click(sendButton);

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(screen.getByText("第二轮：只回复收到")).toBeInTheDocument();
    expect(sendMessage).toHaveBeenCalledWith(
      "第二轮：只回复收到",
      "chat-1",
      [],
      expect.objectContaining({
        runtimeId: "codex",
        model: "gpt-5",
        reasoning: "high",
        permissionLevel: "auto",
      }),
    );
  });

  it("does not send when Enter confirms an IME composition", () => {
    const sendMessage = vi.fn<SendMessage>();
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "你好" } });
    fireEvent.compositionEnd(input);
    const accepted = fireEvent.keyDown(input, {
      key: "Enter",
      code: "Enter",
      isComposing: false,
    });

    expect(sendMessage).not.toHaveBeenCalled();
    expect(input).toHaveValue("你好");
    expect(accepted).toBe(false);
  });

  it("keeps Shift+Enter as a newline and sends with a regular Enter", async () => {
    const user = userEvent.setup();
    const sendMessage = vi.fn<SendMessage>().mockResolvedValue({
      chat: { id: "chat-1" },
      execution: {
        id: "execution-1",
        userMessageId: "user-1",
        status: "pending",
      },
    });
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );

    const input = screen.getByRole("textbox", { name: "message" });
    await user.type(input, "第一行");
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    await user.type(input, "第二行");
    expect(sendMessage).not.toHaveBeenCalled();
    expect(input).toHaveValue("第一行\n第二行");

    await user.keyboard("{Enter}");
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage).toHaveBeenCalledWith(
      "第一行\n第二行",
      "chat-1",
      [],
      expect.objectContaining({
        runtimeId: "codex",
        model: "gpt-5",
        reasoning: "high",
        permissionLevel: "auto",
      }),
    );
  });

  it("replaces the assistant-ui message repository when switching chats", async () => {
    const { rerender } = render(
      <ChatQueueProvider>
        <KeyedHistoryHarness
          chatId="chat-1"
          messages={[{ id: "user-1", role: "user", content: "第一段历史" }]}
        />
      </ChatQueueProvider>,
    );
    expect(screen.getByText("第一段历史")).toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <HistoryHarness
          chatId="chat-2"
          messages={[{ id: "user-2", role: "user", content: "第二段历史" }]}
        />
      </ChatQueueProvider>,
    );

    await waitFor(() =>
      expect(screen.getByText("第二段历史")).toBeInTheDocument(),
    );
    expect(screen.queryByText("第一段历史")).not.toBeInTheDocument();
  });

  it("shows history when the selected chat finishes loading", async () => {
    const { rerender } = render(
      <ChatQueueProvider>
        <HistoryHarness chatId="chat-2" messages={[]} historyReady={false} />
      </ChatQueueProvider>,
    );
    expect(screen.queryByText("第二段历史")).not.toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <HistoryHarness
          chatId="chat-2"
          messages={[{ id: "user-2", role: "user", content: "第二段历史" }]}
        />
      </ChatQueueProvider>,
    );

    expect(await screen.findByText("第二段历史")).toBeInTheDocument();
  });

  it("replaces a partial assistant snapshot with the complete authoritative message", async () => {
    const partial: ChatMessage = {
      id: "assistant-1",
      role: "assistant",
      content: "你好！很高",
      status: "completed",
      revision: 6,
      displayParts: [
        { id: "assistant-1:part:5", type: "text", text: "你好！很高" },
      ],
    };
    const complete: ChatMessage = {
      ...partial,
      content: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
      status: "completed",
      revision: 7,
      displayParts: [
        {
          id: "assistant-1:part:5",
          type: "text",
          text: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
        },
      ],
    };
    const { rerender } = render(
      <ChatQueueProvider>
        <HistoryHarness chatId="chat-1" messages={[partial]} />
      </ChatQueueProvider>,
    );
    expect(screen.getByText("你好！很高")).toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <HistoryHarness chatId="chat-1" messages={[complete]} />
      </ChatQueueProvider>,
    );

    expect(
      await screen.findByText("你好！很高兴见到你。有什么我可以帮你处理的吗？"),
    ).toBeInTheDocument();
    expect(screen.queryByText("你好！很高")).not.toBeInTheDocument();
  });

  it("updates the visible chat message when a completed snapshot grows", async () => {
    const partial: ChatMessage = {
      id: "assistant-visible",
      role: "assistant",
      content: "你好！很高",
      status: "completed",
      revision: 6,
      displayParts: [
        { id: "assistant-visible:part:5", type: "text", text: "你好！很高" },
      ],
    };
    const complete: ChatMessage = {
      ...partial,
      content: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
      revision: 7,
      displayParts: [
        {
          id: "assistant-visible:part:5",
          type: "text",
          text: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
        },
      ],
    };
    const { rerender } = render(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness messages={[partial]} />
      </ChatQueueProvider>,
    );
    expect(await screen.findByText("你好！很高")).toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <ChatThreadHistoryHarness messages={[complete]} />
      </ChatQueueProvider>,
    );

    expect(
      await screen.findByText("你好！很高兴见到你。有什么我可以帮你处理的吗？"),
    ).toBeInTheDocument();
  });

  it("drains the complete answer after a running snapshot reaches terminal", async () => {
    const running: ChatMessage = {
      id: "assistant-streaming-terminal",
      role: "assistant",
      executionId: "execution-1",
      content: "你好！很高",
      status: "running",
      revision: 6,
      displayParts: [
        {
          id: "assistant-streaming-terminal:part:1",
          type: "text",
          text: "你好！很高",
        },
      ],
    };
    const completed: ChatMessage = {
      ...running,
      content: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
      status: "completed",
      revision: 7,
      runMetrics: { durationMs: 14_800 },
      displayParts: [
        {
          id: "assistant-streaming-terminal:part:1",
          type: "text",
          text: "你好！很高兴见到你。有什么我可以帮你处理的吗？",
        },
      ],
    };
    const { container, rerender } = render(
      <TooltipProvider delayDuration={0}>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness messages={[running]} />
        </ChatQueueProvider>
      </TooltipProvider>,
    );

    await screen.findByText("你好！很高");
    rerender(
      <TooltipProvider delayDuration={0}>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness messages={[completed]} />
        </ChatQueueProvider>
      </TooltipProvider>,
    );

    await screen.findByText("14.8s");
    expect(
      container.querySelector('[data-chat-markdown="assistant"]'),
    ).toHaveTextContent("你好！很高兴见到你。有什么我可以帮你处理的吗？");
  });

  it("renders consecutive SSE snapshots before the execution reaches terminal", async () => {
    const running: ChatMessage = {
      id: "execution-streaming",
      role: "assistant",
      executionId: "execution-1",
      content: "",
      status: "running",
      revision: 1,
      displayParts: [],
    };
    render(
      <TooltipProvider delayDuration={0}>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness messages={[running]} />
        </ChatQueueProvider>
      </TooltipProvider>,
    );
    await waitFor(() => expect(chatSubscription.onEvent).toBeDefined());

    act(() => {
      chatSubscription.onEvent?.({
        transient: true,
        type: "runtime.event",
        message: {
          ...running,
          revision: 2,
          displayParts: [{ type: "text", text: "第一段" }],
        },
      });
    });
    // 完整渲染包含流式平滑和延迟 Markdown；仍在执行终态到达前断言两次快照。
    expect(
      await screen.findByText("第一段", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    act(() => {
      chatSubscription.onEvent?.({
        transient: true,
        type: "runtime.event",
        message: {
          ...running,
          revision: 3,
          displayParts: [{ type: "text", text: "第一段，第二段" }],
        },
      });
    });
    expect(
      await screen.findByText("第一段，第二段", {}, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it("StrictMode 下历史晚于就绪标志到达时无需 SSE 状态更新即可显示", async () => {
    const view = (messages: ChatMessage[]) => (
      <StrictMode>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness
            chatId="late-team-history"
            messages={messages}
          />
        </ChatQueueProvider>
      </StrictMode>
    );
    const { rerender } = render(view([]));
    rerender(
      view([{ id: "late-history", role: "user", content: "不等待连接的历史" }]),
    );
    expect(await screen.findByText("不等待连接的历史")).toBeVisible();
  });

  it("shows history when messages arrive after a ready chat switch", async () => {
    const { rerender } = render(
      <ChatQueueProvider>
        <HistoryHarness
          chatId="chat-1"
          messages={[{ id: "user-1", role: "user", content: "第一段历史" }]}
        />
      </ChatQueueProvider>,
    );
    expect(screen.getByText("第一段历史")).toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <KeyedHistoryHarness chatId="chat-2" messages={[]} />
      </ChatQueueProvider>,
    );
    expect(screen.queryByText("第一段历史")).not.toBeInTheDocument();

    rerender(
      <ChatQueueProvider>
        <KeyedHistoryHarness
          chatId="chat-2"
          messages={[{ id: "user-2", role: "user", content: "第二段历史" }]}
        />
      </ChatQueueProvider>,
    );

    expect(await screen.findByText("第二段历史")).toBeInTheDocument();
  });

  it("切回仍在运行的已就绪对话时立即恢复执行过程", async () => {
    const runningMessage: ChatMessage = {
      id: "assistant-running",
      role: "assistant",
      executionId: "execution-1",
      content: "",
      status: "running",
      revision: 3,
      displayParts: [
        {
          id: "assistant-running:part:1",
          type: "process_commentary",
          text: "正在检查切换前的执行过程",
        },
      ],
    };
    const { rerender } = render(
      <StrictMode>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness
            chatId="chat-1"
            messages={[runningMessage]}
          />
        </ChatQueueProvider>
      </StrictMode>,
    );
    expect(
      await screen.findByText("正在检查切换前的执行过程"),
    ).toBeInTheDocument();

    rerender(
      <StrictMode>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness
            chatId="chat-2"
            messages={[{ id: "user-2", role: "user", content: "第二段历史" }]}
          />
        </ChatQueueProvider>
      </StrictMode>,
    );
    await act(async () => {
      await Promise.resolve();
    });

    rerender(
      <StrictMode>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness
            chatId="chat-2"
            messages={[
              { id: "user-2", role: "user", content: "第二段历史" },
              { id: "user-3", role: "user", content: "触发会话刷新" },
            ]}
          />
        </ChatQueueProvider>
      </StrictMode>,
    );
    expect(await screen.findByText("第二段历史")).toBeInTheDocument();
    expect(
      screen.queryByText("正在检查切换前的执行过程"),
    ).not.toBeInTheDocument();

    rerender(
      <StrictMode>
        <ChatQueueProvider>
          <ChatThreadHistoryHarness
            chatId="chat-1"
            messages={[runningMessage]}
          />
        </ChatQueueProvider>
      </StrictMode>,
    );

    expect(
      await screen.findByText("正在检查切换前的执行过程"),
    ).toBeInTheDocument();
  });

  it("restores composer text after an explicit backend rejection", async () => {
    const user = userEvent.setup();
    const sendMessage = vi
      .fn<SendMessage>()
      .mockRejectedValue(
        new ApiError("当前对话已有运行中的执行", "ChatHasActiveExecution", 409),
      );
    render(
      <ChatQueueProvider>
        <ComposerHarness send={sendMessage} />
      </ChatQueueProvider>,
    );
    const input = screen.getByRole("textbox", { name: "message" });
    await user.type(input, "保留这段输入");
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(input).toHaveValue("保留这段输入"));
  });

  it("restores the accepted skill selection after an explicit backend rejection", async () => {
    const user = userEvent.setup();
    const onMessageAccepted = vi.fn();
    const onMessageRejected = vi.fn();
    const sendMessage = vi
      .fn<SendMessage>()
      .mockRejectedValue(
        new ApiError("当前对话已有运行中的执行", "ChatHasActiveExecution", 409),
      );
    render(
      <ChatQueueProvider>
        <ComposerHarness
          send={sendMessage}
          onMessageAccepted={onMessageAccepted}
          onMessageRejected={onMessageRejected}
          selection={{
            runtimeId: "codex",
            model: "gpt-5",
            reasoning: "high",
            permissionLevel: "auto",
            skillIds: ["skill-review"],
            selectedSkills: [
              {
                id: "skill-review",
                name: "代码审查",
                version: "1.4.0",
              },
            ],
          }}
        />
      </ChatQueueProvider>,
    );
    await user.type(
      screen.getByRole("textbox", { name: "message" }),
      "审查改动",
    );
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(onMessageAccepted).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(onMessageRejected).toHaveBeenCalledWith(
        expect.objectContaining({ skillIds: ["skill-review"] }),
      ),
    );
  });
  it("restores the accepted plugin selection after an explicit backend rejection", async () => {
    const user = userEvent.setup();
    const onMessageAccepted = vi.fn();
    const onMessageRejected = vi.fn();
    const sendMessage = vi
      .fn<SendMessage>()
      .mockRejectedValue(
        new ApiError("当前对话已有运行中的执行", "ChatHasActiveExecution", 409),
      );
    render(
      <ChatQueueProvider>
        <ComposerHarness
          send={sendMessage}
          onMessageAccepted={onMessageAccepted}
          onMessageRejected={onMessageRejected}
          selection={{
            runtimeId: "codex",
            model: "gpt-5",
            reasoning: "high",
            permissionLevel: "auto",
            pluginIds: ["plugin-office"],
            selectedPlugins: [
              {
                id: "plugin-office",
                name: "飞书办公",
                version: "1.4.0",
              },
            ],
          }}
        />
      </ChatQueueProvider>,
    );
    await user.type(
      screen.getByRole("textbox", { name: "message" }),
      "审查改动",
    );
    await user.click(screen.getByRole("button", { name: "send" }));

    await waitFor(() => expect(onMessageAccepted).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(onMessageRejected).toHaveBeenCalledWith(
        expect.objectContaining({ pluginIds: ["plugin-office"] }),
      ),
    );
  });
});
