import type { AppendMessage } from "@assistant-ui/react";
import {
  createContext,
  useContext,
  useState,
  useSyncExternalStore,
  type PropsWithChildren,
} from "react";

import type { BotChatMessage, ChatReplyReference } from "./chat-projection";
import type { ChatWorkflowReference } from "../components/chat-workflow-reference";
import type { ChatScopeKey } from "../model/conversation-identity";
import type { PermissionLevel } from "@/shared/lib/runtime-permissions";

import type {
  GithubComJieWaZiNeuxBotServerServicesChannelQueuedLeadInput as QueuedLeadInput,
  GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as MessageResult,
} from "@/shared/api/generated/types.gen";

export type ChatSubmissionResult = MessageResult & {
  queuedInput?: QueuedLeadInput;
  /** 发言的公开时间线由业务入口刷新，接纳后即可继续输入。 */
  published?: boolean;
};

/** 请求适配层同步确定真实输入身份；接纳命令沿用同一份请求。 */
export type PreparedChatSubmission = {
  /** 普通 HTTP 输入复用已有幂等身份；无身份的领域入口保持缺省。 */
  submissionId?: string;
  /** 权威消息或成功回执确认接纳后，按本命令身份退役重试记录；重复确认无副作用。 */
  confirmAccepted?: () => void;
  /** 持久队列已接纳原请求，可向同内容的新输入分配另一个身份。 */
  confirmQueued?: () => void;
  /** 判断该回执是否明确拒绝原命令，并结算其本地提交身份。 */
  confirmRejected?: (error: unknown) => boolean;
  /** 明确拒绝已结算原身份时，重试用冻结输入重新准备。 */
  retryWithNewSubmission?: () => boolean;
  /** 仅支持幂等重放的首发入口提供；待确认期间禁止开始替代提交。 */
  hasUnconfirmedReceipt?: () => boolean;
  /** 本次准备失败且没有更早的不明回执时，允许取回原草稿。 */
  canReturnToDraft?: () => boolean;
  /** 首发未分配公开会话标识时，按冻结的原请求身份停止。 */
  cancel?: () => Promise<void>;
  /** 有独立消息流的入口可复用本次用户消息预览。 */
  accept: (userMessage?: BotChatMessage) => Promise<ChatSubmissionResult>;
};

/** 冻结所属场景的请求构造器；出队时不读取后来改选的领域上下文。 */
export type ChatSubmissionPreparer = (
  text: string,
  chatId?: string,
  attachmentPaths?: string[],
  selection?: ChatSendSelection,
) => PreparedChatSubmission;

export type ChatSendSelection = {
  /** 首发时冻结输入框中的档案优化意图，重试不读取后续标签改动。 */
  optimizeProfile?: boolean;
  /** 团队主聊天显式指定的执行伙伴，与插件引用按同一输入冻结。 */
  mentionWakerId?: string;
  steerExecutionId?: string;
  /** 原输入尚未取得执行身份时，出队前沿其提交身份解析。 */
  steerSubmissionId?: string;
  accountId?: string;
  runtimeId: string;
  model: string;
  reasoning: string;
  fast?: boolean;
  contextWindow?: string;
  permissionLevel: PermissionLevel;
  skillIds?: string[];
  selectedSkills?: Array<{
    id: string;
    name: string;
    version?: string;
  }>;
  pluginIds?: string[];
  selectedPlugins?: Array<{
    id: string;
    name: string;
    version?: string;
    versionId?: string;
  }>;
  connectorIds?: string[];
  selectedConnectors?: Array<{
    id: string;
    name: string;
  }>;
  codeReferences?: ChatCodeReference[];
  /** 正文由纯附件输入的本地提示补位。 */
  attachmentOnly?: boolean;
  /** 输入没有用户正文，由代码引用提示补位。 */
  codeReferenceOnly?: boolean;
  replyTo?: ChatReplyReference;
  workflow?: ChatWorkflowReference;
  workflowOnly?: boolean;
};

export type ChatCodeReference = {
  id?: string;
  path?: string;
  startLine?: number;
  endLine?: number;
  content?: string;
};

export type QueuedChatEntry = {
  id: string;
  prompt: string;
  message: AppendMessage;
  chatId?: string;
  selection: ChatSendSelection;
  prepareSubmission?: ChatSubmissionPreparer;
  /** 在原页签结算输入；延迟终态允许恢复到已重新清空且无占用的编辑器。 */
  settleDraft?: (rejected: boolean, restoreIfEmpty?: boolean) => boolean;
  replyDraft?: { isEmpty: () => boolean; restore: () => void };
};

export type ChatSendSnapshot = {
  conversationKey: ChatScopeKey;
  queueKey: string;
  chatId?: string;
  selection: ChatSendSelection;
  prepareSubmission: ChatSubmissionPreparer;
  settleDraft?: QueuedChatEntry["settleDraft"];
  replyDraft?: QueuedChatEntry["replyDraft"];
};

export type ScopedChatReplyRetry = {
  message: AppendMessage;
  snapshot: ChatSendSnapshot;
  prepared: PreparedChatSubmission;
  noticeId: string;
  reconcile: boolean;
  renewSubmission: boolean;
};

/** 本地冻结的原回复等待队列或主聊天结算，生命周期沿用所属工作区。 */
export type QueuedTeamReply = Pick<
  ScopedChatReplyRetry,
  "message" | "snapshot" | "prepared"
> & {
  inputId: string;
  submissionId: string;
};

type ChatReplyRetryHandler = (retry: ScopedChatReplyRetry) => Promise<void>;

type Listener = () => void;
type QueueState = Record<string, readonly QueuedChatEntry[]>;
type HandoffState = Record<string, readonly BotChatMessage[]>;

const emptyEntries: readonly QueuedChatEntry[] = [];
const emptyHandoffMessages: readonly BotChatMessage[] = [];
const emptyTeamReplies: readonly QueuedTeamReply[] = [];

export class ChatQueueStore {
  private state: QueueState = {};
  private handoffs: HandoffState = {};
  private readonly teamReplies = new Map<string, readonly QueuedTeamReply[]>();
  private readonly listeners = new Set<Listener>();
  private readonly replyRetryHandlers = new Map<
    ChatScopeKey,
    ChatReplyRetryHandler
  >();

  registerReplyRetryHandler(key: ChatScopeKey, replay: ChatReplyRetryHandler) {
    this.replyRetryHandlers.set(key, replay);
    return () => {
      if (this.replyRetryHandlers.get(key) === replay)
        this.replyRetryHandlers.delete(key);
    };
  }

  retryReply(retry: ScopedChatReplyRetry) {
    return this.replyRetryHandlers.get(retry.snapshot.conversationKey)?.(retry);
  }

  getTeamReplies(queueKey?: string): readonly QueuedTeamReply[] {
    return (queueKey && this.teamReplies.get(queueKey)) || emptyTeamReplies;
  }

  trackTeamReply(reply: QueuedTeamReply) {
    const { queueKey } = reply.snapshot;
    const current = this.getTeamReplies(queueKey);
    this.teamReplies.set(queueKey, [
      ...current.filter((item) => item.submissionId !== reply.submissionId),
      reply,
    ]);
    this.emit();
  }

  takeTeamReply(queueKey: string, submissionId: string) {
    const current = this.getTeamReplies(queueKey);
    const reply = current.find((item) => item.submissionId === submissionId);
    if (!reply) return;
    if (current.length === 1) this.teamReplies.delete(queueKey);
    else
      this.teamReplies.set(
        queueKey,
        current.filter((item) => item !== reply),
      );
    this.emit();
    return reply;
  }

  confirmTeamReply(queueKey: string, submissionId: string) {
    const reply = this.takeTeamReply(queueKey, submissionId);
    reply?.prepared.confirmAccepted?.();
    reply?.snapshot.settleDraft?.(false);
  }

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getEntries = (queueKey: string): readonly QueuedChatEntry[] =>
    this.state[queueKey] ?? emptyEntries;

  getHandoff = (chatId?: string): readonly BotChatMessage[] =>
    chatId
      ? (this.handoffs[chatId] ?? emptyHandoffMessages)
      : emptyHandoffMessages;

  enqueue(queueKey: string, entry: QueuedChatEntry, first = false) {
    const current = this.getEntries(queueKey);
    this.update(queueKey, first ? [entry, ...current] : [...current, entry]);
  }

  take(queueKey: string): QueuedChatEntry | undefined {
    const current = this.getEntries(queueKey);
    const head = current[0];
    if (!head) return undefined;
    this.update(queueKey, current.slice(1));
    return head;
  }

  promote(
    queueKey: string,
    entryId: string,
    steer?: Pick<ChatSendSelection, "steerExecutionId" | "steerSubmissionId">,
  ) {
    const current = this.getEntries(queueKey);
    const target = current.find((entry) => entry.id === entryId);
    if (!target) return;
    this.update(queueKey, [
      {
        ...target,
        selection: {
          ...target.selection,
          steerExecutionId: steer?.steerExecutionId,
          steerSubmissionId: steer?.steerSubmissionId,
        },
      },
      ...current.filter((entry) => entry.id !== entryId),
    ]);
  }

  remove(queueKey: string, entryId: string) {
    this.update(
      queueKey,
      this.getEntries(queueKey).filter((entry) => entry.id !== entryId),
    );
  }

  clear(queueKey: string) {
    this.update(queueKey, emptyEntries);
  }

  setHandoff(chatId: string, messages: readonly BotChatMessage[]) {
    this.handoffs = { ...this.handoffs, [chatId]: messages };
    this.emit();
  }

  clearHandoff(chatId: string) {
    if (!(chatId in this.handoffs)) return;
    this.handoffs = Object.fromEntries(
      Object.entries(this.handoffs).filter(([key]) => key !== chatId),
    );
    this.emit();
  }

  migrate(fromQueueKey: string, toChatId: string) {
    if (fromQueueKey === toChatId) return;
    const source = this.getEntries(fromQueueKey);
    if (source.length === 0) return;
    const migrated = source.map((entry) => ({
      ...entry,
      chatId: toChatId,
    }));
    const next = this.without(fromQueueKey);
    next[toChatId] = [...(next[toChatId] ?? emptyEntries), ...migrated];
    this.state = next;
    this.emit();
  }

  private update(queueKey: string, entries: readonly QueuedChatEntry[]) {
    if (entries.length === 0) {
      if (!(queueKey in this.state)) return;
      this.state = this.without(queueKey);
    } else {
      this.state = { ...this.state, [queueKey]: entries };
    }
    this.emit();
  }

  private emit() {
    for (const listener of this.listeners) listener();
  }

  private without(queueKey: string): QueueState {
    return Object.fromEntries(
      Object.entries(this.state).filter(([key]) => key !== queueKey),
    );
  }
}

const ChatQueueContext = createContext<ChatQueueStore | null>(null);

export function ChatQueueProvider({ children }: PropsWithChildren) {
  const [store] = useState(() => new ChatQueueStore());
  return (
    <ChatQueueContext.Provider value={store}>
      {children}
    </ChatQueueContext.Provider>
  );
}

export function useChatQueueStore(): ChatQueueStore {
  const store = useContext(ChatQueueContext);
  if (!store) {
    throw new Error("ChatQueueProvider is required");
  }
  return store;
}

export function useChatQueueEntries(queueKey: string) {
  const store = useChatQueueStore();
  return useSyncExternalStore(
    store.subscribe,
    () => store.getEntries(queueKey),
    () => store.getEntries(queueKey),
  );
}

export function useQueuedTeamReplies(queueKey?: string) {
  const store = useChatQueueStore();
  return useSyncExternalStore(
    store.subscribe,
    () => store.getTeamReplies(queueKey),
    () => store.getTeamReplies(queueKey),
  );
}

export function useChatHandoff(chatId?: string) {
  const store = useChatQueueStore();
  return useSyncExternalStore(
    store.subscribe,
    () => store.getHandoff(chatId),
    () => store.getHandoff(chatId),
  );
}
