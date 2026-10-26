import type {
  ThreadAssistantMessagePart,
  ThreadMessageLike,
  ToolCallMessagePart,
} from "@assistant-ui/react";

import type {
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessageDisplayPart as MessageDisplayPart,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessageRunMetrics as MessageRunMetrics,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewToolApprovalResult as ToolApprovalResult,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewToolFileChangeSummary as ToolFileChangeSummary,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewToolSafeSummary as ToolSafeSummary,
} from "@/shared/api/generated/types.gen";
import type { ChatEvent } from "@/shared/api/events";
import type { ChatCodeReference } from "./chat-queue-store";
import { runtimeFailurePresentation } from "../model/runtime-failure-presentation";

export const CHAT_PROCESS_COMMENTARY_PART = "chat.process_commentary";

export type ToolCategory =
  | "file_read"
  | "file_search"
  | "file_modify"
  | "command"
  | "network"
  | "mcp"
  | "skill"
  | "other";

export type ToolAction =
  | "read_file"
  | "list_files"
  | "search_files"
  | "write_file"
  | "modify_file"
  | "run_tests"
  | "check_project"
  | "build_project"
  | "format_code"
  | "inspect_git"
  | "execute_command"
  | "fetch_web"
  | "search_web"
  | "call_service"
  | "use_skill"
  | "view_image"
  | "other";

export type ChatProcessCommentaryData = {
  text: string;
};

export type ChatToolArgs = {
  category: ToolCategory;
  action: ToolAction;
  label?: string;
  loadingLabel?: string;
  safeSummary?: ToolSafeSummary;
  approvalResult?: ToolApprovalResult;
  status: "running" | "completed" | "failed";
};

export type ChatRunMetrics = MessageRunMetrics;
export type ChatFileChange = ToolFileChangeSummary & { path: string };

export type AllyChatMessage = ChatMessage & {
  optimistic?: boolean;
  /** 首发已接纳但响应未带执行身份时，仅展示到首次权威历史接管。 */
  awaitingFirstHistory?: boolean;
  codeReferences?: ChatCodeReference[];
};

export type ChatMessageVisibility = (message: AllyChatMessage) => boolean;

export function latestContextMetrics(
  messages: readonly AllyChatMessage[],
): ChatRunMetrics | undefined {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const metrics = messages[index]?.runMetrics;
    if (metrics?.contextWindowTokens && metrics.contextWindowTokens > 0) {
      return metrics;
    }
  }
  return undefined;
}

const toolCategoryKeys = new Set<ToolCategory>([
  "file_read",
  "file_search",
  "file_modify",
  "command",
  "network",
  "mcp",
  "skill",
  "other",
]);

const toolActionKeys = new Set<ToolAction>([
  "read_file",
  "list_files",
  "search_files",
  "write_file",
  "modify_file",
  "run_tests",
  "check_project",
  "build_project",
  "format_code",
  "inspect_git",
  "execute_command",
  "fetch_web",
  "search_web",
  "call_service",
  "use_skill",
  "view_image",
  "other",
]);

function toolCategoryOf(value?: string): ToolCategory {
  return toolCategoryKeys.has(value as ToolCategory)
    ? (value as ToolCategory)
    : "other";
}

function toolStatusOf(value?: string): ChatToolArgs["status"] {
  if (value === "completed" || value === "failed") return value;
  return "running";
}

function toolActionOf(value?: string): ToolAction {
  return toolActionKeys.has(value as ToolAction)
    ? (value as ToolAction)
    : "other";
}

function messageIDOf(message: AllyChatMessage | ChatMessage) {
  return message.id ?? "";
}

function normalizeAuthoritativeMessage(
  message: ChatMessage,
  recordedAt?: number,
): AllyChatMessage {
  return {
    ...message,
    createdAt: message.createdAt ?? recordedAt,
    updatedAt: message.updatedAt ?? recordedAt,
    optimistic: false,
  };
}

function contentRevision(message: AllyChatMessage): number {
  return message.revision ?? -1;
}

function displayPartsOf(message: AllyChatMessage): MessageDisplayPart[] {
  return Array.isArray(message.displayParts) ? message.displayParts : [];
}

function textPartOf(text?: string): ThreadAssistantMessagePart[] {
  return text ? [{ type: "text", text }] : [];
}

function processCommentaryPartOf(text?: string): ThreadAssistantMessagePart[] {
  return text
    ? [
        {
          type: "data",
          name: CHAT_PROCESS_COMMENTARY_PART,
          data: { text } satisfies ChatProcessCommentaryData,
        },
      ]
    : [];
}

function reasoningPartOf(text?: string): ThreadAssistantMessagePart[] {
  return text ? [{ type: "reasoning", text }] : [];
}

function toolPartOf(part: MessageDisplayPart): ToolCallMessagePart {
  const status = toolStatusOf(part.toolStatus);
  return {
    type: "tool-call",
    // 展示片段身份已包含调用范围，原生工具标识可能在重试时重复。
    // 审批与问答仍直接使用各自请求携带的原生 toolCallId。
    toolCallId: part.id || part.toolCallId || "unknown",
    toolName: toolCategoryOf(part.toolCategory),
    args: {
      category: toolCategoryOf(part.toolCategory),
      action: toolActionOf(part.toolAction),
      ...(part.toolLabel ? { label: part.toolLabel } : {}),
      ...(part.toolLoadingLabel ? { loadingLabel: part.toolLoadingLabel } : {}),
      ...(part.toolSafeSummary
        ? { safeSummary: part.toolSafeSummary as ToolSafeSummary }
        : {}),
      ...(part.toolApprovalResult
        ? { approvalResult: part.toolApprovalResult as ToolApprovalResult }
        : {}),
      status,
    } satisfies ChatToolArgs,
    argsText: "{}",
    ...(status === "running" ? {} : { result: {} }),
    isError: status === "failed",
  };
}

function assistantPartsOf(
  message: AllyChatMessage,
): ThreadAssistantMessagePart[] {
  const displayParts = displayPartsOf(message);
  return displayParts.flatMap((part): ThreadAssistantMessagePart[] => {
    // 按当前完整片段判断可展示内容，在工具分组前跳过纯空白。
    // trim 只用于判断，原始文本与流式增量保留，避免丢失段落换行和代码缩进。
    if (part.type !== "tool" && !part.text?.trim()) return [];
    if (part.type === "text") return textPartOf(part.text);
    if (part.type === "process_commentary") {
      return processCommentaryPartOf(part.text);
    }
    if (part.type === "reasoning") return reasoningPartOf(part.text);
    if (part.type === "tool") return [toolPartOf(part)];
    return [];
  });
}

export function mergeAuthoritativeMessages(
  current: AllyChatMessage[],
  authoritative: ChatMessage[],
): AllyChatMessage[] {
  const currentByID = new Map(
    current
      .filter((message) => message.id)
      .map((message) => [message.id, message]),
  );
  const merged = authoritative.map((message) => {
    const local = currentByID.get(message.id);
    currentByID.delete(message.id);
    if (!local) return message;
    if (local.optimistic) return message;
    const localRevision = contentRevision(local);
    const authoritativeRevision = message.revision ?? -1;
    if (localRevision > authoritativeRevision) return local;
    return message;
  });

  for (const message of currentByID.values()) {
    merged.push(message);
  }
  return merged;
}

function findMessageIndex(
  messages: AllyChatMessage[],
  messageID?: string,
): number {
  if (!messageID) return -1;
  return messages.findIndex((message) => message.id === messageID);
}

export function applyChatEvent(
  messages: AllyChatMessage[],
  event: ChatEvent,
  isMessageVisible: ChatMessageVisibility = () => true,
): AllyChatMessage[] {
  if (!event.message) return messages;
  const authoritative = normalizeAuthoritativeMessage(
    event.message,
    event.recordedAt,
  );
  if (!isMessageVisible(authoritative)) return messages;
  const index = findMessageIndex(messages, authoritative.id);
  const currentMessage = index >= 0 ? messages[index] : undefined;
  // 执行终态是已提交的权威边界，即使修订号落后也必须接管实时快照。
  if (
    currentMessage &&
    !isTerminalEvent(event) &&
    contentRevision(currentMessage) > contentRevision(authoritative)
  ) {
    return messages;
  }

  // 流式更新只替换命中的消息，保留历史消息引用，让消费方可以跳过
  // 已稳定消息的转换和渲染。
  const next = [...messages];
  if (index >= 0) next[index] = { ...currentMessage, ...authoritative };
  else next.push(authoritative);
  return next;
}

export function toThreadMessage(
  message: AllyChatMessage,
  incompleteErrorText = "",
): ThreadMessageLike {
  const role = message.role === "assistant" ? "assistant" : "user";
  const failure =
    message.status === "failed"
      ? runtimeFailurePresentation(message.errorCode)
      : undefined;
  const errorMessage =
    message.errorMessage?.trim() || failure?.message || incompleteErrorText;
  const fileChanges = message.fileChanges ?? [];
  const isTerminal = ["completed", "failed", "canceled"].includes(
    message.status ?? "",
  );
  const content =
    role === "assistant"
      ? assistantPartsOf(message)
      : [{ type: "text" as const, text: message.content ?? "" }];
  return {
    id: messageIDOf(message),
    role,
    content,
    createdAt: new Date(message.createdAt ?? Date.now()),
    metadata: {
      isOptimistic: message.optimistic,
      custom: {
        isTerminal,
        ...(failure
          ? {
              failureMessage: errorMessage,
              failureCode: message.errorCode,
              retryable: message.retryable === true,
              executionId: message.executionId,
            }
          : {}),
        ...(message.runMetrics ? { runMetrics: message.runMetrics } : {}),
        ...(fileChanges.length ? { fileChanges } : {}),
        ...(message.plugins?.length ? { plugins: message.plugins } : {}),
        ...(message.skills?.length ? { skills: message.skills } : {}),
        ...(message.connectors?.length
          ? { connectors: message.connectors }
          : {}),
        ...(message.codeReferences?.length
          ? { codeReferences: message.codeReferences }
          : {}),
      },
    },
    attachments: (message.attachments ?? []).map((attachment, index) => ({
      id: `${message.id}:attachment:${index}`,
      type: attachment.mediaType?.startsWith("image/") ? "image" : "file",
      name: attachment.name ?? attachment.path ?? "attachment",
      contentType: attachment.mediaType,
      status: { type: "complete" },
      content: [
        {
          type: "file",
          filename: attachment.name,
          data: attachment.path ?? "",
          mimeType: attachment.mediaType || "application/octet-stream",
        },
      ],
    })),
    status:
      role === "user"
        ? undefined
        : message.status === "completed"
          ? { type: "complete", reason: "stop" }
          : message.status === "failed"
            ? {
                type: "incomplete",
                reason: "error",
                error: { message: errorMessage },
              }
            : message.status === "canceled"
              ? { type: "incomplete", reason: "cancelled" }
              : { type: "running" },
  };
}

export function isTerminalEvent(event: ChatEvent): boolean {
  return (
    event.type === "execution.completed" ||
    event.type === "execution.failed" ||
    event.type === "execution.canceled"
  );
}
