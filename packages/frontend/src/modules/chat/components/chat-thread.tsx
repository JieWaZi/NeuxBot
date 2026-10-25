import { ChatMessagePluginReferences } from "./chat-plugin-reference";
import { useChatHistoryScroll } from "./use-chat-history-scroll";
import type { ChatResultSource } from "@/shared/api/notification-presentation";
import { ChatWorkResultReader } from "@/shared/api/work-result-reading";
import { ChatMessageContribution } from "./chat-contributions";
import { ChatForkContext, MessageForkAction } from "./chat-fork";
import { Link } from "react-router-dom";
import { chatHref } from "@/shared/lib/chat-navigation";
import {
  reportChatError,
  reportErrorNotice,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import {
  ChatErrorActions,
  ChatErrorCard,
  ChatErrorNotices,
  MessageFailure,
  useChatHistoryError,
} from "./chat-error-card";
import {
  FileReferenceProvider,
  FileReferenceText,
} from "@/shared/workspace/file-reference-link";
import {
  ActionBarPrimitive,
  AssistantRuntimeProvider,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  QueueItemPrimitive,
  ThreadPrimitive,
  TextMessagePartProvider,
  useAui,
  useAuiState,
  type AssistantRuntime,
  type ThreadMessage,
  type DataMessagePart,
  type PartState,
  type ReasoningMessagePartProps,
  type ToolCallMessagePart,
  type ToolCallMessagePartProps,
} from "@assistant-ui/react";
import {
  AlertTriangle,
  Brain,
  Cable,
  Check,
  ChevronRight,
  CircleEllipsis,
  Copy,
  CornerDownRight,
  File,
  FilePenLine,
  FileSearch,
  Globe,
  LoaderCircle,
  ListStart,
  Plus,
  Shuffle,
  Sparkles,
  Terminal,
  Trash2,
} from "lucide-react";
import {
  Children,
  createContext,
  useContext,
  useCallback,
  memo,
  useId,
  useMemo,
  useReducer,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type ComponentProps,
} from "react";
import { useTranslation } from "react-i18next";
import { motion, useReducedMotion } from "motion/react";

import type { ResolvedFileReference } from "@/shared/workspace/file-reference";
import { cn } from "@/shared/lib/cn";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { Avatar } from "@/shared/ui/avatar";
import { companionAvatar } from "@/shared/companion/appearance";
import {
  CompanionInteraction as ComposerCompanion,
  CompanionInteractionProvider,
} from "@/shared/ui/companion-interaction";
import { messageCompanionActivity } from "../runtime/companion-activity";
import { Collapsible, CollapsibleContent } from "@/shared/ui/collapsible";
import { Button } from "@/shared/ui/button";
import {
  ComposerContext,
  ComposerLayerStack,
  ComposerSendButton,
  ComposerStopButton,
  ComposerSurface,
  ComposerInputBody,
} from "@/shared/ui/composer";
import { Spinner } from "@/shared/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { selectFiles } from "@/shared/platform";
import {
  pathFile,
  type AllyPathFile,
} from "../runtime/chat-attachment-adapter";
import {
  CHAT_PROCESS_COMMENTARY_PART,
  type ChatProcessCommentaryData,
  type ChatFileChange,
  type ChatRunMetrics,
  type ChatToolArgs,
  type ToolCategory,
  type ToolAction,
} from "../runtime/chat-projection";
import { contextUsagePercent, formatTokenCount } from "./chat-context-usage";
import { chatPrimaryAction } from "./chat-primary-action";
import {
  sendFailureEquals,
  type ChatSendFailure,
} from "../runtime/chat-send-failure";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval } from "@/shared/api/generated/types.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingElicitation as PendingElicitation } from "@/shared/api/generated/types.gen";
import {
  approvalQueuePosition,
  approvalResultLabel,
  ChatApprovalCard,
  nextApprovalSeenIds,
  type ApprovalSubmissionState,
} from "./chat-approval-card";
import {
  ChatElicitationCard,
  type ElicitationSubmissionState,
} from "./chat-elicitation-card";
import {
  ChatComposerInput,
  type ChatMemberMentionOption,
} from "./chat-composer-input";
import { chatContentColumnClassName } from "./chat-layout";
import {
  ChatMessageConnectorReferences,
  type ChatConnectorOption,
} from "./chat-connector-reference";
import { ChatContextUsage } from "./chat-context-usage";
import {
  ChatMessageAttachments,
  ChatMessageSkillReferences,
  type ChatFileOpenRequest,
  type ChatSkillOption,
} from "./chat-reference-menus";
import { AssistantText, UserText } from "./chat-message-content";
import { ChatTurnNavigator } from "./chat-turn-navigator";

const CompanionInteraction = createContext<"waiting" | "stopping" | undefined>(
  undefined,
);

type ChatThreadProps = {
  fork?: React.ContextType<typeof ChatForkContext>;
  forkBoundaryMessageId?: string;
  forkSourceChatId?: string;
  onOpenForkSource?: (chatId: string) => Promise<boolean>;
  runtime: AssistantRuntime;
  wakerName: string;
  wakerId?: string;
  wakerAvatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  activityStatus?: ReactNode;
  isRunning?: boolean;
  isWorkRunning?: boolean;
  isWorkStopping?: boolean;
  onStopWork?: () => void;
  showUsageMetrics?: boolean;
  emptyState: ReactNode;
  emptyStatePosition?: "center" | "fill";
  /** 画布首发时同步输入框的位置与宽度，保留同一编辑器实例。 */
  animateComposerSize?: boolean;
  pinEmptyState?: boolean;
  isHistoryLoading?: boolean;
  historyError?: { message: string; onRetry: () => void; error?: unknown };
  composerSettings?: ReactNode;
  composerActions?: ReactNode;
  composerEffects?: ReactNode;
  renderComposerContextStrip?: () => ReactNode;
  composerSkills?: ChatSkillOption[];
  composerSkillsError?: boolean;
  composerPlugins?: ChatSkillOption[];
  composerPluginsError?: boolean;
  selectedPluginIds?: string[];
  onPluginSelectionChange?: (ids: string[]) => void;
  composerConnectors?: ChatConnectorOption[];
  composerConnectorsError?: boolean;
  composerFileRoots?: string[];
  composerFileScopePath?: string;
  composerMembers?: ChatMemberMentionOption[];
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  availablePluginIds?: ReadonlySet<string>;
  selectedSkillIds?: string[];
  selectedConnectorIds?: string[];
  onSkillSelectionChange?: (skillIds: string[]) => void;
  onConnectorSelectionChange?: (connectorIds: string[]) => void;
  onMemberMentionChange?: (wakerId?: string) => void;
  codeReferenceRequest?: ChatCodeReference;
  composerDraft?: string;
  showComposer?: boolean;
  /** 完整输入区可独立用于业务指令，不呈现消息列表或产生阅读事实。 */
  presentation?: "thread" | "composer";
  composerAutoFocus?: boolean;
  composerDisabled?: boolean;
  composerClearRevision?: number;
  companionScopeRevision?: number;
  composerRestoreRevision?: number;
  terminalEnabled?: boolean;
  onOpenTerminal?: () => void;
  onOpenLink?: (url: string) => boolean;
  linkWorkspaceRoot?: string;
  fileScopeChatId?: string;
  /** 真实聊天与成果归属独立于文件授权；所有共享聊天入口沿用同一阅读器。 */
  resultSource?: ChatResultSource;
  linkAdditionalWorkingDirectories?: readonly string[];
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  contextMetrics?: ChatRunMetrics;
  sendFailure: ChatSendFailure;
  errorScopes?: readonly string[];
  onRetryExecution?: (executionId: string) => Promise<void>;
  onRecoverSession?: () => void;
  hasEarlierMessages: boolean;
  isLoadingEarlier: boolean;
  onLoadEarlier: () => Promise<void>;
  warning?: string;
  requestedInteractionId?: string;
  pendingApprovals?: PendingApproval[];
  approvalSubmissionState?: ApprovalSubmissionState;
  onResolveApproval?: (approvalId: string, optionId: string) => void;
  pendingElicitations?: PendingElicitation[];
  elicitationSubmissionState?: ElicitationSubmissionState;
  onResolveElicitation?: (
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => void;
};

export type ChatMessageAuthor = {
  name: string;
  avatar?: string;
  displayAsParticipant?: boolean;
  sourceChatId?: string;
  kind?: "member-report";
  truncated?: boolean;
};

export type ChatFileChangeOpenRequest = ChatFileOpenRequest & {
  messageId: string;
  sourceChatId?: string;
};

const toolCategoryKeys: ToolCategory[] = [
  "file_read",
  "file_search",
  "file_modify",
  "command",
  "network",
  "mcp",
  "skill",
  "other",
];

const toolCategoryIcons = {
  file_read: File,
  file_search: FileSearch,
  file_modify: FilePenLine,
  command: Terminal,
  network: Globe,
  mcp: Cable,
  skill: Sparkles,
  other: CircleEllipsis,
} satisfies Record<ToolCategory, typeof File>;

const toolActionKeys: ToolAction[] = [
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
];

function toolArgsOf(args: ToolCallMessagePart["args"]): ChatToolArgs {
  const value = args as Record<string, unknown>;
  const category = toolCategoryKeys.includes(value.category as ToolCategory)
    ? (value.category as ToolCategory)
    : "other";
  const status =
    value.status === "completed" || value.status === "failed"
      ? value.status
      : "running";
  return {
    category,
    action: toolActionKeys.includes(value.action as ToolAction)
      ? (value.action as ToolAction)
      : "other",
    label: typeof value.label === "string" ? value.label : undefined,
    loadingLabel:
      typeof value.loadingLabel === "string" ? value.loadingLabel : undefined,
    safeSummary:
      value.safeSummary && typeof value.safeSummary === "object"
        ? (value.safeSummary as ChatToolArgs["safeSummary"])
        : undefined,
    approvalResult:
      value.approvalResult && typeof value.approvalResult === "object"
        ? (value.approvalResult as ChatToolArgs["approvalResult"])
        : undefined,
    status,
  };
}

function categoryFromTool(
  part: Pick<ToolCallMessagePart, "args">,
): ToolCategory {
  const args = part.args as Record<string, unknown>;
  const category = args.category;
  return toolCategoryKeys.includes(category as ToolCategory)
    ? (category as ToolCategory)
    : "other";
}

type ToolSummaryWords = {
  completedPrefix: string;
  runningPrefix: string;
  file: string;
  verbs: Record<ToolCategory, string>;
};

function isFileToolCategory(category: ToolCategory) {
  return (
    category === "file_read" ||
    category === "file_search" ||
    category === "file_modify"
  );
}

function formatToolActivitySummary(
  activities: readonly {
    category: ToolCategory;
    status: ChatToolArgs["status"];
    label?: string;
  }[],
  words: ToolSummaryWords,
) {
  let summary = "";
  for (const [index, activity] of activities.entries()) {
    const previous = activities[index - 1];
    if (!previous || previous.status !== activity.status) {
      if (previous) summary += "，";
      summary +=
        activity.status === "running"
          ? words.runningPrefix
          : words.completedPrefix;
    }
    summary += activity.label || words.verbs[activity.category];
    const next = activities[index + 1];
    if (
      isFileToolCategory(activity.category) &&
      (!next ||
        next.status !== activity.status ||
        !isFileToolCategory(next.category))
    ) {
      summary += words.file;
    }
  }
  return summary;
}

function statusFromTool(
  part: Pick<ToolCallMessagePart, "args" | "isError" | "result">,
  messageIsRunning: boolean,
): ChatToolArgs["status"] {
  const args = toolArgsOf(part.args);
  if (part.isError || args.status === "failed") return "failed";
  if (args.status === "completed" || part.result !== undefined) {
    return "completed";
  }
  return messageIsRunning ? "running" : args.status;
}

function ProcessCommentaryPart({
  part,
}: {
  part: DataMessagePart<ChatProcessCommentaryData>;
}) {
  if (part.name !== CHAT_PROCESS_COMMENTARY_PART) return null;
  return (
    <p className="type-chat text-foreground max-w-4xl whitespace-pre-wrap">
      {part.data.text}
    </p>
  );
}

export function ReasoningPart({ text }: ReasoningMessagePartProps) {
  const { t } = useTranslation("chat");
  const [expanded, setExpanded] = useState(false);
  const displayText = text.replace(/^[\r\n]+/, "");
  const canToggle = displayText.length > 120;
  if (!displayText) return null;
  return (
    <div className="space-y-1.5">
      <div className="text-muted-foreground type-label flex items-center gap-2">
        <Brain className="size-icon-sm" />
        <span>{t("reasoningActivity")}</span>
      </div>
      <div className="border-border-subtle bg-surface-tertiary rounded-lg border border-dashed px-4 py-3">
        <p
          className={[
            "type-chat text-foreground overflow-hidden whitespace-pre-wrap",
            expanded || !canToggle ? "" : "line-clamp-4",
          ].join(" ")}
        >
          {displayText}
        </p>
        {canToggle && (
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground type-ui mt-2 inline-flex items-center gap-1 transition-colors"
            onClick={() => setExpanded((current) => !current)}
          >
            {expanded ? t("collapseReasoning") : t("expandReasoning")}
          </button>
        )}
      </div>
    </div>
  );
}

export function ToolActivityRow({
  args,
  isError,
  result,
}: ToolCallMessagePartProps) {
  const { t } = useTranslation("chat");
  const messageStatus = useAuiState((state) => state.message.status);
  const toolArgs = toolArgsOf(args);
  const toolStatus = statusFromTool(
    { args, isError, result },
    messageStatus?.type === "running",
  );
  const Icon = toolCategoryIcons[toolArgs.category];
  const summary = toolArgs.safeSummary;
  const fallbackLabel = t(
    `toolPhrase.${toolStatus === "running" ? "running" : "completed"}.${toolArgs.category}`,
  );
  const label =
    toolStatus === "running"
      ? (toolArgs.loadingLabel ?? toolArgs.label ?? fallbackLabel)
      : (toolArgs.label ?? fallbackLabel);
  const showFallbackTarget = !toolArgs.label && !toolArgs.loadingLabel;
  const files = summary?.files ?? [];
  const messageIsTerminal =
    messageStatus?.type === "complete" || messageStatus?.type === "incomplete";
  const compactFileModification =
    messageIsTerminal &&
    toolStatus === "completed" &&
    toolArgs.category === "file_modify";
  const visibleFiles = compactFileModification ? [] : files;
  const hasFiles = visibleFiles.length > 0;
  const added = summary?.addedLines ?? 0;
  const deleted = summary?.deletedLines ?? 0;
  const hasDiff =
    !compactFileModification &&
    (typeof summary?.addedLines === "number" ||
      typeof summary?.deletedLines === "number");
  const approvalResult = toolArgs.approvalResult;
  const approvalLabel = approvalResult
    ? approvalResultLabel(
        {
          result: approvalResult.result,
          kind: approvalResult.optionKind,
          name: approvalResult.optionName,
        },
        t,
      )
    : "";
  const approvalCommand = approvalResult?.command?.trim() ?? "";
  const showApprovalCommand =
    approvalCommand !== "" && !label.includes(approvalCommand);

  return (
    <div
      className={[
        "type-label flex min-w-0 gap-2 py-0.5",
        toolStatus === "failed"
          ? "bg-destructive/10 text-destructive rounded-xs"
          : "text-muted-foreground",
        hasFiles ? "items-start" : "items-center",
      ].join(" ")}
    >
      <Icon
        className={["size-icon-sm shrink-0", hasFiles ? "mt-0.5" : ""].join(
          " ",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate">
            {toolStatus === "failed" ? (
              <span className="sr-only">
                {t("toolActivitySummary.failedPrefix")}
              </span>
            ) : null}
            {label}
          </span>
          {showApprovalCommand ? (
            <code
              className="bg-muted text-foreground/75 type-ui max-w-60 truncate rounded-xs px-1 py-0.5 font-mono"
              title={approvalCommand}
            >
              {approvalCommand}
            </code>
          ) : null}
          {!compactFileModification &&
            !hasFiles &&
            showFallbackTarget &&
            summary?.path && (
              <code className="bg-muted type-ui max-w-60 truncate rounded-xs px-1 py-0.5 font-mono">
                {summary.path}
              </code>
            )}
          {!hasFiles && showFallbackTarget && summary?.domain && (
            <span className="type-ui min-w-0 truncate">{summary.domain}</span>
          )}
          {!hasFiles && hasDiff && (
            <span className="type-label shrink-0 font-medium tabular-nums">
              {added > 0 && <span className="text-success">+{added}</span>}
              {added > 0 && deleted > 0 && " "}
              {deleted > 0 && (
                <span className="text-destructive">-{deleted}</span>
              )}
            </span>
          )}
          {approvalLabel ? (
            <span className="bg-muted text-muted-foreground type-caption shrink-0 rounded-xs px-2 py-0.5">
              {approvalLabel}
            </span>
          ) : null}
        </div>
        {hasFiles && (
          <div
            className="border-border/60 mt-2 space-y-1 border-l pl-3"
            role="list"
          >
            {visibleFiles.map((file) => {
              const path = file.path?.trim();
              if (!path) return null;
              return (
                <div
                  key={path}
                  className="type-ui flex min-w-0 items-center justify-between gap-4 py-0.5"
                  role="listitem"
                >
                  <code
                    className="text-foreground/75 min-w-0 truncate font-mono"
                    title={path}
                  >
                    {path}
                  </code>
                  <span className="type-label shrink-0 font-medium tabular-nums">
                    {(file.addedLines ?? 0) > 0 && (
                      <span className="text-success">+{file.addedLines}</span>
                    )}
                    {(file.addedLines ?? 0) > 0 &&
                      (file.deletedLines ?? 0) > 0 &&
                      " "}
                    {(file.deletedLines ?? 0) > 0 && (
                      <span className="text-destructive">
                        -{file.deletedLines}
                      </span>
                    )}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

export function WorkProcess({
  indices,
  children,
  onOpenFileChange,
}: {
  indices: readonly number[];
  children?: ReactNode;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
}) {
  const { t } = useTranslation("chat");
  const messageContent = useAuiState((state) => state.message.content);
  const messageStatus = useAuiState((state) => state.message.status);
  const messageIsRunning = messageStatus?.type === "running";
  const messageIsTerminal =
    messageStatus?.type === "complete" || messageStatus?.type === "incomplete";
  const [open, setOpen] = useState(false);
  const contentId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const indexedParts = useMemo(
    () =>
      indices
        .map((index) => messageContent[index])
        .filter((part) => part !== undefined),
    [indices, messageContent],
  );
  const tools = indexedParts.filter(
    (part): part is ToolCallMessagePart => part.type === "tool-call",
  );
  const categories = tools.reduce<ToolCategory[]>((ordered, tool) => {
    const category = categoryFromTool(tool);
    if (!ordered.includes(category)) ordered.push(category);
    return ordered;
  }, []);
  const activities: {
    category: ToolCategory;
    status: ChatToolArgs["status"];
    labels: string[];
  }[] = [];
  for (const tool of tools) {
    const category = categoryFromTool(tool);
    // 汇总只描述活动；失败状态留在对应工具行，不生成标题级失败总结。
    const actualStatus = statusFromTool(tool, messageIsRunning);
    const status = actualStatus === "failed" ? "completed" : actualStatus;
    let activity = activities.find(
      (item) => item.category === category && item.status === status,
    );
    if (!activity) {
      activity = { category, status, labels: [] };
      activities.push(activity);
    }
    const label = toolArgsOf(tool.args).label;
    if (
      (category === "mcp" || category === "skill") &&
      label &&
      !activity.labels.includes(label)
    ) {
      activity.labels.push(label);
    }
  }
  const summary = formatToolActivitySummary(
    activities.map((activity) => ({
      ...activity,
      label: activity.labels.join("、") || undefined,
    })),
    {
      completedPrefix: t("toolActivitySummary.completedPrefix"),
      runningPrefix: t("toolActivitySummary.runningPrefix"),
      file: t("toolActivitySummary.file"),
      verbs: {
        file_read: t("toolActivitySummary.verb.file_read"),
        file_search: t("toolActivitySummary.verb.file_search"),
        file_modify: t("toolActivitySummary.verb.file_modify"),
        command: t("toolActivitySummary.verb.command"),
        network: t("toolActivitySummary.verb.network"),
        mcp: t("toolActivitySummary.verb.mcp"),
        skill: t("toolActivitySummary.verb.skill"),
        other: t("toolActivitySummary.verb.other"),
      },
    },
  );
  const Icon = toolCategoryIcons[categories[0] ?? "other"];
  const isCurrentToolGroup =
    messageIsRunning && indices.at(-1) === messageContent.length - 1;
  const isLastToolGroup =
    indices.at(-1) ===
    messageContent.findLastIndex((part) => part.type === "tool-call");
  const processChildren = Children.toArray(children);

  const toggleOpen = () => {
    if (open) triggerRef.current?.focus({ preventScroll: true });
    setOpen((current) => !current);
  };

  return (
    <Collapsible open={open} asChild>
      <section className="space-y-1.5">
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground type-label group/tool-process relative inline-flex max-w-full min-w-0 items-center gap-2 overflow-hidden rounded-md py-0.5 text-left transition-colors"
          data-running-shimmer={
            isCurrentToolGroup && !open ? "true" : undefined
          }
          aria-expanded={open}
          aria-controls={contentId}
          aria-busy={isCurrentToolGroup}
          title={summary}
          ref={triggerRef}
          onClick={toggleOpen}
        >
          <Icon className="size-icon-sm shrink-0" aria-hidden="true" />
          <span className="min-w-0 truncate">{summary}</span>
          <ChevronRight
            className={cn(
              "size-icon-xs duration-layout shrink-0 transition-[opacity,transform] motion-reduce:transition-none",
              open
                ? "rotate-90 opacity-100"
                : "opacity-0 group-hover/tool-process:opacity-100 group-focus-visible/tool-process:opacity-100",
            )}
            aria-hidden="true"
          />
        </button>
        <CollapsibleContent id={contentId} inert={!open} aria-hidden={!open}>
          <div className="mt-2 space-y-2 pl-5">
            {processChildren.map((child, index) => (
              <div
                key={index}
                className={[
                  messageIsRunning && index < processChildren.length - 1
                    ? "opacity-70"
                    : "",
                ].join(" ")}
              >
                {child}
              </div>
            ))}
            <ExecutionFileChangesCard
              visible={messageIsTerminal && isLastToolGroup}
              onOpenFile={onOpenFileChange}
            />
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground type-ui inline-flex items-center gap-1 py-1 transition-colors"
              onClick={toggleOpen}
            >
              <ChevronRight className="size-icon-xs -rotate-90" />
              {t("collapseProcess")}
            </button>
          </div>
        </CollapsibleContent>
      </section>
    </Collapsible>
  );
}

function groupChatPart(part: PartState) {
  if (part.type === "tool-call") return ["group-work-process"] as const;
  return [];
}

function ChatMessageParts({
  onOpenFileChange,
}: {
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
}) {
  const messageId = useAuiState((state) => state.message.id);
  const isTerminal = useAuiState(
    (state) => state.message.metadata.custom?.isTerminal === true,
  );
  return (
    <div className="space-y-4">
      <MessagePrimitive.GroupedParts
        groupBy={groupChatPart}
        indicator="no-text"
      >
        {({ part, children }) => {
          switch (part.type) {
            case "group-work-process":
              return (
                <WorkProcess
                  key={`${messageId}:${part.indices[0]}`}
                  indices={part.indices}
                  onOpenFileChange={onOpenFileChange}
                >
                  {children}
                </WorkProcess>
              );
            case "text":
              return <AssistantText isTerminal={isTerminal} />;
            case "data":
              return (
                <ProcessCommentaryPart
                  part={part as DataMessagePart<ChatProcessCommentaryData>}
                />
              );
            case "reasoning":
              return <ReasoningPart {...part} />;
            case "tool-call":
              return part.toolUI ?? <ToolActivityRow {...part} />;
            case "indicator":
              return null;
            default:
              return null;
          }
        }}
      </MessagePrimitive.GroupedParts>
    </div>
  );
}

const durationNumberFormat = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});
const tokenNumberFormat = new Intl.NumberFormat("zh-CN");
const messageTimeFormat = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
});
const messageDateTimeFormat = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
});

function formatMessageTime(date: Date) {
  const now = new Date();
  const isToday =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();

  if (isToday) {
    return messageTimeFormat.format(date);
  }

  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day} ${messageDateTimeFormat.format(date)}`;
}

function MessageTimestamp() {
  const createdAt = useAuiState((state) => state.message.createdAt);
  return (
    <time
      className="text-muted-foreground type-caption tabular-nums"
      dateTime={createdAt.toISOString()}
    >
      {formatMessageTime(createdAt)}
    </time>
  );
}

function isUnknownRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object";
}

function runMetricsOf(value: unknown): ChatRunMetrics | undefined {
  if (!isUnknownRecord(value)) return undefined;
  const source = value;
  const numberValue = (key: string) =>
    typeof source[key] === "number" ? source[key] : undefined;
  return {
    startedAt: numberValue("startedAt"),
    durationMs: numberValue("durationMs"),
    inputTokens: numberValue("inputTokens"),
    outputTokens: numberValue("outputTokens"),
    reasoningOutputTokens: numberValue("reasoningOutputTokens"),
    cacheReadTokens: numberValue("cacheReadTokens"),
    cacheCreationTokens: numberValue("cacheCreationTokens"),
    contextUsedTokens: numberValue("contextUsedTokens"),
    contextWindowTokens: numberValue("contextWindowTokens"),
  };
}

function formatRunDuration(durationMs: number) {
  if (durationMs < 1000) return `${Math.max(0, Math.round(durationMs))}ms`;
  const seconds = durationMs / 1000;
  if (seconds < 60) return `${durationNumberFormat.format(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${durationNumberFormat.format(seconds % 60)}s`;
}

export function AgentRunningIndicator() {
  const { t } = useTranslation("chat");
  return (
    <div className="text-muted-foreground type-body min-h-control-sm flex items-center gap-2 font-normal">
      <Spinner className="text-primary/65" />
      <span>{t("agentRunning")}</span>
    </div>
  );
}

export function RunMetricsBadge({ metrics }: { metrics: ChatRunMetrics }) {
  const { t } = useTranslation("chat");
  const durationMs = metrics.durationMs ?? 0;
  const rows = [
    metrics.contextUsedTokens && metrics.contextWindowTokens
      ? [
          t("runMetrics.contextUsed"),
          `${formatTokenCount(metrics.contextUsedTokens)} / ${formatTokenCount(metrics.contextWindowTokens)} (${contextUsagePercent(metrics)}%)`,
        ]
      : undefined,
    [t("runMetrics.duration"), formatRunDuration(durationMs)],
    metrics.inputTokens
      ? [
          t("runMetrics.inputTokens"),
          tokenNumberFormat.format(metrics.inputTokens),
        ]
      : undefined,
    metrics.outputTokens
      ? [
          t("runMetrics.outputTokens"),
          tokenNumberFormat.format(metrics.outputTokens),
        ]
      : undefined,
    metrics.reasoningOutputTokens
      ? [
          t("runMetrics.reasoningOutputTokens"),
          tokenNumberFormat.format(metrics.reasoningOutputTokens),
        ]
      : undefined,
    metrics.cacheCreationTokens
      ? [
          t("runMetrics.cacheCreationTokens"),
          tokenNumberFormat.format(metrics.cacheCreationTokens),
        ]
      : undefined,
    metrics.cacheReadTokens
      ? [
          t("runMetrics.cacheReadTokens"),
          tokenNumberFormat.format(metrics.cacheReadTokens),
        ]
      : undefined,
  ].filter((row): row is string[] => Boolean(row));

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground type-body py-1 font-normal tabular-nums transition-colors"
          aria-label={t("runMetrics.details")}
        >
          {formatRunDuration(durationMs)}
        </button>
      </TooltipTrigger>
      <TooltipContent side="top">
        <dl className="space-y-1">
          {rows.map(([label, value]) => (
            <div key={label} className="grid grid-cols-[auto_1fr] gap-2">
              <dt>{label}:</dt>
              <dd className="text-right tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </TooltipContent>
    </Tooltip>
  );
}

export function MessageRunStatus({
  showUsageDetails = true,
}: {
  showUsageDetails?: boolean;
}) {
  const status = useAuiState((state) => state.message.status);
  const isCurrentToolProcess = useAuiState(
    (state) => state.message.content.at(-1)?.type === "tool-call",
  );
  const rawRunMetrics = useAuiState(
    (state) => state.message.metadata.custom.runMetrics,
  );
  const runMetrics = runMetricsOf(rawRunMetrics);

  if (status?.type === "running") {
    return isCurrentToolProcess ? null : <AgentRunningIndicator />;
  }
  if (!runMetrics?.durationMs) return null;
  return showUsageDetails ? (
    <RunMetricsBadge metrics={runMetrics} />
  ) : (
    <span className="text-muted-foreground type-body py-1 font-normal tabular-nums">
      {formatRunDuration(runMetrics.durationMs)}
    </span>
  );
}

function fileChangesOf(value: unknown): ChatFileChange[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (!isUnknownRecord(item) || typeof item.path !== "string") return [];
    const path = item.path.trim();
    if (!path) return [];
    return [
      {
        path,
        addedLines:
          typeof item.addedLines === "number" ? item.addedLines : undefined,
        deletedLines:
          typeof item.deletedLines === "number" ? item.deletedLines : undefined,
      },
    ];
  });
}

function fileChangeDisplayPath(path: string) {
  return path.startsWith("projects/") ? path.slice("projects/".length) : path;
}

export function ExecutionFileChangesCard({
  visible = false,
  onOpenFile,
}: {
  visible?: boolean;
  onOpenFile?: (file: ChatFileChangeOpenRequest) => void;
}) {
  const { t } = useTranslation("chat");
  const messageID = useAuiState((state) => state.message.id);
  const isTerminal = useAuiState(
    (state) => state.message.metadata?.custom?.isTerminal === true,
  );
  const rawChanges = useAuiState(
    (state) => state.message.metadata?.custom?.fileChanges,
  );
  const [expandedMessageID, setExpandedMessageID] = useState<string>();
  const expanded = expandedMessageID === messageID;
  const changes = fileChangesOf(rawChanges);
  const visibleChanges = expanded ? changes : changes.slice(0, 3);
  const hiddenCount = Math.max(0, changes.length - visibleChanges.length);

  if (!visible || !isTerminal || changes.length === 0) return null;

  return (
    <section
      data-work-result-body
      className="border-border bg-surface-elevated mt-3 overflow-hidden rounded-lg border"
      aria-label={t("fileChanges.title", { count: changes.length })}
    >
      <div className="min-h-row-default flex items-center gap-2 px-3">
        <span className="bg-muted size-control-sm grid shrink-0 place-items-center rounded-md">
          <FilePenLine className="text-muted-foreground size-icon-md" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="type-label font-medium">
            {t("fileChanges.title", { count: changes.length })}
          </p>
        </div>
      </div>
      <div className="border-border border-t px-3 py-2">
        <ul className="grid gap-0.5">
          {visibleChanges.map((file) => {
            const displayPath = fileChangeDisplayPath(file.path);
            const added = file.addedLines ?? 0;
            const deleted = file.deletedLines ?? 0;
            const content = (
              <>
                <code className="type-body min-w-0 truncate text-left font-mono">
                  {displayPath}
                </code>
                <span className="type-body flex shrink-0 items-center gap-2 font-medium tabular-nums">
                  {added > 0 ? (
                    <span className="text-success">+{added}</span>
                  ) : null}
                  {deleted > 0 ? (
                    <span className="text-destructive">-{deleted}</span>
                  ) : null}
                </span>
              </>
            );
            return onOpenFile ? (
              <li key={file.path}>
                <button
                  type="button"
                  className="hover:bg-muted focus-visible:ring-ring min-h-control-sm grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-4 rounded-sm px-2 text-left transition-colors outline-none focus-visible:ring-2"
                  aria-label={t("fileChanges.openFile", { path: displayPath })}
                  onClick={() =>
                    onOpenFile({
                      messageId: messageID,
                      name: file.path.split("/").at(-1) ?? file.path,
                      path: file.path,
                    })
                  }
                >
                  {content}
                </button>
              </li>
            ) : (
              <li
                key={file.path}
                className="min-h-control-sm grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 px-2"
              >
                {content}
              </li>
            );
          })}
        </ul>
        {changes.length > 3 ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="mt-1"
            onClick={() =>
              setExpandedMessageID((current) =>
                current === messageID ? undefined : messageID,
              )
            }
          >
            {expanded
              ? t("fileChanges.showLess")
              : t("fileChanges.showMore", { count: hiddenCount })}
            <ChevronRight
              className={expanded ? "rotate-90" : ""}
              aria-hidden="true"
            />
          </Button>
        ) : null}
      </div>
    </section>
  );
}

function UserMessage({
  text,
  availableSkillIds,
  availableConnectorIds,
  availablePluginIds,
  wakerId = "",
  onOpenCodeReference,
  onOpenFile,
}: {
  text: string;
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  availablePluginIds?: ReadonlySet<string>;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
}) {
  const rawSkills = useAuiState(
    (state) => state.message.metadata.custom.skills,
  );
  const skills = Array.isArray(rawSkills)
    ? rawSkills.flatMap((value) => {
        if (!value || typeof value !== "object") return [];
        const skill = value as {
          id?: unknown;
          name?: unknown;
          version?: unknown;
        };
        if (typeof skill.id !== "string" || typeof skill.name !== "string") {
          return [];
        }
        return [{ id: skill.id, name: skill.name, version: skill.version }];
      })
    : [];
  const rawConnectors = useAuiState(
    (state) => state.message.metadata.custom.connectors,
  );
  const connectors = Array.isArray(rawConnectors)
    ? rawConnectors.flatMap((value) => {
        if (!value || typeof value !== "object") return [];
        const connector = value as { id?: unknown; name?: unknown };
        if (
          typeof connector.id !== "string" ||
          typeof connector.name !== "string"
        ) {
          return [];
        }
        return [{ id: connector.id, name: connector.name }];
      })
    : [];
  const rawPlugins = useAuiState(
    (state) => state.message.metadata.custom.plugins,
  );
  const rawCodeReferences = useAuiState(
    (state) => state.message.metadata.custom.codeReferences,
  );
  const codeReferences = Array.isArray(rawCodeReferences)
    ? (rawCodeReferences as ChatCodeReference[])
    : [];
  return (
    <MessagePrimitive.Root className="group mt-6 px-6">
      <div className="ml-auto grid max-w-[78%] min-w-0 justify-items-end gap-2">
        <div className="bg-surface-tertiary text-foreground type-chat max-w-full min-w-0 overflow-hidden rounded-xl px-4 py-3">
          <div className="flex flex-wrap items-start gap-x-1.5 gap-y-1">
            <ChatMessagePluginReferences
              plugins={rawPlugins}
              wakerId={wakerId}
              availablePluginIds={availablePluginIds}
            />
            <ChatMessageSkillReferences
              skills={skills}
              wakerId={wakerId}
              availableSkillIds={availableSkillIds}
            />
            <ChatMessageConnectorReferences
              connectors={connectors}
              wakerId={wakerId}
              availableConnectorIds={availableConnectorIds}
            />
            <ChatMessageAttachments onOpenFile={onOpenFile} />
            {codeReferences.map((reference) => {
              const wholeFile =
                !reference.startLine &&
                !reference.endLine &&
                !reference.content;
              const path = reference.path ?? "";
              return (
                <button
                  key={`${path}:${reference.startLine}:${reference.endLine}:${reference.content}`}
                  type="button"
                  className="bg-border/60 hover:bg-border/80 type-body inline-flex max-w-full items-center rounded-md px-2 py-0.5 font-medium"
                  onClick={() => onOpenCodeReference?.(reference)}
                >
                  <span className="max-w-64 truncate">
                    <FileReferenceText path={path} />
                    {!wholeFile
                      ? ` (${reference.startLine}-${reference.endLine})`
                      : null}
                  </span>
                </button>
              );
            })}
            <div className="max-w-full min-w-0">
              <TextMessagePartProvider text={text} isRunning={false}>
                <UserText />
              </TextMessagePartProvider>
            </div>
          </div>
        </div>
        <MessageCopyAction />
      </div>
    </MessagePrimitive.Root>
  );
}

function ParticipantMessage({
  text,
  avatar,
  messageAuthors,
  name,
}: {
  text: string;
  avatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  name: string;
}) {
  const { t } = useTranslation("chat");
  const messageID = useAuiState((state) => state.message.id);
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const memberReport = author?.kind === "member-report";

  return (
    <MessagePrimitive.Root className="group mt-6 grid grid-cols-[32px_minmax(0,1fr)] items-start gap-3 px-6">
      <Avatar name={displayName} src={displayAvatar} size="sm" />
      <div className="min-w-0">
        <div className="grid justify-items-start">
          <span className="type-body font-medium">{displayName}</span>
          <MessageTimestamp />
        </div>
        <div
          className={cn(
            "mt-2 min-w-0",
            memberReport &&
              "border-border bg-muted/25 border-l-primary/55 overflow-hidden rounded-lg border border-l-2",
          )}
        >
          {memberReport ? (
            <div className="border-border/70 bg-muted/45 flex items-center justify-between gap-3 border-b px-4 py-2">
              <span className="text-muted-foreground type-label font-medium">
                {t("team.memberReport")}
              </span>
              {author?.truncated ? (
                <span className="text-warning type-caption inline-flex items-center gap-1 font-medium">
                  <AlertTriangle className="size-icon-xs" aria-hidden="true" />
                  {t("team.memberReportTruncated")}
                </span>
              ) : null}
            </div>
          ) : null}
          <div
            data-work-result-body
            className={memberReport ? "px-4 py-3" : undefined}
          >
            <TextMessagePartProvider text={text} isRunning={false}>
              <UserText />
            </TextMessagePartProvider>
          </div>
        </div>
        <div className="mt-2 flex items-center gap-1">
          <MessageCopyAction />
        </div>
      </div>
    </MessagePrimitive.Root>
  );
}

function MessageCopyAction({
  hideWhenRunning = false,
}: {
  hideWhenRunning?: boolean;
}) {
  const { t } = useTranslation("chat");

  return (
    <ActionBarPrimitive.Root hideWhenRunning={hideWhenRunning} autohide="never">
      <ActionBarPrimitive.Copy asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={t("copy")}
          aria-label={t("copy")}
        >
          <AuiIf condition={(state) => state.message.isCopied}>
            <Check />
          </AuiIf>
          <AuiIf condition={(state) => !state.message.isCopied}>
            <Copy />
          </AuiIf>
        </Button>
      </ActionBarPrimitive.Copy>
    </ActionBarPrimitive.Root>
  );
}

function AddAttachmentButton({
  disabled,
  onAttachmentsChange,
}: {
  disabled?: boolean;
  onAttachmentsChange: () => void;
}) {
  const { t } = useTranslation("chat");
  const aui = useAui();
  const [selecting, setSelecting] = useState(false);
  const errors = useContext(ChatErrorActions);
  const addAttachments = async () => {
    setSelecting(true);
    const recovered = errors.begin?.("attachment");
    try {
      const paths = await selectFiles();
      for (const path of paths) {
        await aui.composer.addAttachment(pathFile(path));
        onAttachmentsChange();
      }
      if (paths.length) recovered?.();
    } catch {
      errors.report?.(t("attachmentFailed"), "attachment");
    } finally {
      setSelecting(false);
    }
  };
  return (
    <Button
      type="button"
      onClick={() => void addAttachments()}
      disabled={disabled || selecting}
      title={t("addAttachment")}
      aria-label={t("addAttachment")}
      size="icon-sm"
      variant="ghost"
    >
      {selecting ? <LoaderCircle className="animate-spin" /> : <Plus />}
    </Button>
  );
}

export function AssistantMessage({
  name,
  avatar,
  messageAuthors,
  onOpenFileChange,
  showUsageMetrics = true,
}: {
  name: string;
  avatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  showUsageMetrics?: boolean;
}) {
  const { t } = useTranslation("chat");
  const message = useAuiState((state) => state.message);
  const messageID = message.id;
  const interaction = useContext(CompanionInteraction);
  const companionActivity = messageCompanionActivity(message, interaction);
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const openFileChange = author?.sourceChatId
    ? (file: ChatFileChangeOpenRequest) =>
        onOpenFileChange?.({ ...file, sourceChatId: author.sourceChatId })
    : onOpenFileChange;

  return (
    <MessagePrimitive.Root className="group mt-6 grid grid-cols-[32px_minmax(0,1fr)] items-start gap-3 px-6">
      <Avatar
        name={displayName}
        src={displayAvatar}
        size="sm"
        activity={companionActivity}
      />
      <div className="min-w-0">
        <div className="grid justify-items-start">
          <span className="type-body font-medium">{displayName}</span>
          <MessageTimestamp />
        </div>
        <div className="mt-2 min-w-0">
          <ChatMessageParts onOpenFileChange={openFileChange} />
          <ChatMessageContribution messageId={messageID} />
        </div>
        <AuiIf
          condition={(state) =>
            state.message.status?.type === "incomplete" &&
            state.message.status.reason === "cancelled"
          }
        >
          <p className="text-muted-foreground type-body mt-2">
            {t("canceled")}
          </p>
        </AuiIf>

        <MessageFailure />

        <div className="mt-2 flex items-center gap-1">
          <MessageRunStatus showUsageDetails={showUsageMetrics} />
          <MessageCopyAction hideWhenRunning />
          <MessageForkAction />
        </div>
      </div>
    </MessagePrimitive.Root>
  );
}

const ChatMessage = memo(function ChatMessage({
  message,
  name,
  avatar,
  availableSkillIds,
  availableConnectorIds,
  availablePluginIds,
  wakerId,
  onOpenCodeReference,
  onOpenFile,
  onOpenFileChange,
  messageAuthors,
  showUsageMetrics,
}: {
  message: ThreadMessage;
  name: string;
  avatar?: string;
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  availablePluginIds?: ReadonlySet<string>;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  showUsageMetrics?: boolean;
}) {
  // 消息分支直接消费当前快照。角色相同不代表正文或成员归属没有变化。
  if (message.role === "user") {
    const text = message.content
      .flatMap((part) => (part.type === "text" ? [part.text] : []))
      .join("\n");
    return messageAuthors?.[message.id]?.displayAsParticipant ? (
      <ParticipantMessage
        text={text}
        name={name}
        avatar={avatar}
        messageAuthors={messageAuthors}
      />
    ) : (
      <UserMessage
        text={text}
        availableSkillIds={availableSkillIds}
        availableConnectorIds={availableConnectorIds}
        availablePluginIds={availablePluginIds}
        wakerId={wakerId}
        onOpenCodeReference={onOpenCodeReference}
        onOpenFile={onOpenFile}
      />
    );
  }
  return message.role === "assistant" ? (
    <AssistantMessage
      name={name}
      avatar={avatar}
      messageAuthors={messageAuthors}
      onOpenFileChange={onOpenFileChange}
      showUsageMetrics={showUsageMetrics}
    />
  ) : null;
});

const ChatMessageList = memo(function ChatMessageList({
  messagesByID,
  forkBoundaryMessageId,
  forkSourceChatId,
  onOpenForkSource,
  wakerId,
  ...props
}: Omit<ComponentProps<typeof ChatMessage>, "message"> & {
  messagesByID: ReadonlyMap<string, ThreadMessage>;
  forkBoundaryMessageId?: string;
  forkSourceChatId?: string;
  onOpenForkSource?: (chatId: string) => Promise<boolean>;
}) {
  const { t } = useTranslation("chat");
  const [unavailableSourceId, setUnavailableSourceId] = useState<string>();
  return (
    <ThreadPrimitive.Messages>
      {({ message }) => (
        <>
          <ChatMessage
            {...props}
            wakerId={wakerId}
            message={messagesByID.get(message.id) ?? message}
          />
          {message.id === forkBoundaryMessageId && forkSourceChatId ? (
            <div
              data-fork-history-boundary
              className="text-muted-foreground type-caption mt-8 flex items-center gap-3 px-6"
            >
              <span aria-hidden="true" className="bg-border h-px flex-1" />
              {unavailableSourceId === forkSourceChatId ? (
                <span className="flex shrink-0 items-center gap-1.5">
                  <Shuffle aria-hidden="true" className="size-icon-sm" />
                  {t("forkSourceUnavailable")}
                </span>
              ) : (
                <Link
                  aria-label={t("forkBackToSource")}
                  title={t("forkBackToSource")}
                  to={chatHref(forkSourceChatId, wakerId ?? "")}
                  onClick={(event) => {
                    if (!onOpenForkSource) return;
                    event.preventDefault();
                    void onOpenForkSource(forkSourceChatId).then(
                      (available) => {
                        if (!available)
                          setUnavailableSourceId(forkSourceChatId);
                      },
                    );
                  }}
                  className="text-accent hover:text-accent-hover focus-visible:ring-ring flex shrink-0 items-center gap-1.5 rounded-sm underline-offset-2 hover:underline focus-visible:ring-2 focus-visible:outline-none"
                >
                  <Shuffle aria-hidden="true" className="size-icon-sm" />
                  {t("forkHistoryBoundary")}
                </Link>
              )}
              <span aria-hidden="true" className="bg-border h-px flex-1" />
            </div>
          ) : null}
        </>
      )}
    </ThreadPrimitive.Messages>
  );
});

export function ComposerContextUsage({
  metrics,
  visible,
}: {
  metrics?: ChatRunMetrics;
  visible: boolean;
}) {
  return visible ? <ChatContextUsage metrics={metrics} /> : null;
}

export function ChatMessageColumn({ children }: { children: ReactNode }) {
  return (
    <div
      data-chat-message-column
      className={cn(
        "mx-auto w-full flex-1 pt-4 pb-8",
        chatContentColumnClassName,
      )}
    >
      {children}
    </div>
  );
}

const Composer = memo(function Composer({
  autoFocus,
  disabled = false,
  activityStatus,
  settings,
  actions,
  effects,
  renderContextStrip,
  skills,
  skillsError,
  plugins,
  pluginsError,
  selectedPluginIds,
  onPluginSelectionChange,
  connectors,
  connectorsError,
  fileRoots,
  fileScopePath,
  fileScopeChatId,
  members,
  selectedSkillIds,
  selectedConnectorIds,
  onSkillSelectionChange,
  onConnectorSelectionChange,
  onMemberMentionChange,
  contextMetrics,
  showUsageMetrics,
  sendFailure,
  codeReferenceRequest,
  onCodeReferencesChange,
  draft,
  clearRevision,
  restoreRevision,
  terminalEnabled,
  onOpenTerminal,
  onOpenFile,
  requestedInteractionId,
  pendingApprovals,
  approvalSubmissionState,
  onResolveApproval,
  pendingElicitations = [],
  elicitationSubmissionState = "idle",
  onResolveElicitation = () => undefined,
  isWorkRunning,
  isRunning,
  isWorkStopping,
  onStopWork,
}: {
  autoFocus?: boolean;
  disabled?: boolean;
  activityStatus?: ReactNode;
  settings?: ReactNode;
  actions?: ReactNode;
  effects?: ReactNode;
  renderContextStrip?: () => ReactNode;
  skills: ChatSkillOption[];
  skillsError: boolean;
  plugins: ChatSkillOption[];
  pluginsError: boolean;
  selectedPluginIds: string[];
  onPluginSelectionChange: (ids: string[]) => void;
  connectors: ChatConnectorOption[];
  connectorsError: boolean;
  fileRoots: string[];
  fileScopePath?: string;
  fileScopeChatId?: string;
  members: ChatMemberMentionOption[];
  selectedSkillIds: string[];
  selectedConnectorIds: string[];
  onSkillSelectionChange: (skillIds: string[]) => void;
  onConnectorSelectionChange: (connectorIds: string[]) => void;
  onMemberMentionChange?: (wakerId?: string) => void;
  contextMetrics?: ChatRunMetrics;
  showUsageMetrics: boolean;
  sendFailure: ChatSendFailure;
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  draft?: string;
  clearRevision?: number;
  restoreRevision?: number;
  terminalEnabled: boolean;
  onOpenTerminal?: () => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  requestedInteractionId?: string;
  pendingApprovals: PendingApproval[];
  approvalSubmissionState: ApprovalSubmissionState;
  onResolveApproval: (approvalId: string, optionId: string) => void;
  pendingElicitations: PendingElicitation[];
  elicitationSubmissionState: ElicitationSubmissionState;
  onResolveElicitation: (
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => void;
  isWorkRunning: boolean;
  isRunning: boolean;
  isWorkStopping: boolean;
  onStopWork?: () => void;
}) {
  const { t } = useTranslation("chat");
  const aui = useAui();
  const composerIsEmpty = useAuiState((state) => state.composer.isEmpty);
  const composerCanSend = useAuiState((state) => state.composer.canSend);
  const queuedMessageCount = useAuiState(
    (state) => state.composer.queue.length,
  );
  const [, refreshComposer] = useReducer((revision: number) => revision + 1, 0);
  const [submissionState, setSubmissionState] = useState(() => ({
    isRunning,
    pending: false,
    sendFailure,
  }));
  if (
    submissionState.isRunning !== isRunning ||
    !sendFailureEquals(submissionState.sendFailure, sendFailure) ||
    (submissionState.pending && composerCanSend && !isRunning)
  ) {
    setSubmissionState({ isRunning, pending: false, sendFailure });
  }
  const effectiveIsRunning = isRunning || submissionState.pending;
  const primaryAction = chatPrimaryAction({
    isComposerEmpty: composerIsEmpty,
    isRunning: effectiveIsRunning,
    isWorkRunning,
  });
  const prepareComposerSend = () => {
    if (disabled || !aui.composer.getState().canSend) return false;
    setSubmissionState((current) => ({ ...current, pending: true }));
    return true;
  };
  const pendingApprovalKey = pendingApprovals
    .map((approval) => approval.approvalId)
    .join("\0");
  const [approvalSeenState, setApprovalSeenState] = useState(() => ({
    pendingKey: pendingApprovalKey,
    seenIds: nextApprovalSeenIds(pendingApprovals, []),
  }));
  if (pendingApprovalKey !== approvalSeenState.pendingKey) {
    setApprovalSeenState({
      pendingKey: pendingApprovalKey,
      seenIds: nextApprovalSeenIds(pendingApprovals, approvalSeenState.seenIds),
    });
  }
  const approvalPosition = approvalQueuePosition(
    (
      pendingApprovals.find(
        (item) => item.approvalId === requestedInteractionId,
      ) ?? pendingApprovals[0]
    )?.approvalId,
    approvalSeenState.seenIds,
  );
  const requestedApproval = pendingApprovals.find(
    (item) => item.approvalId === requestedInteractionId,
  );
  const requestedElicitation = pendingElicitations.find(
    (item) => item.elicitationId === requestedInteractionId,
  );
  const currentApproval = requestedApproval ?? pendingApprovals[0];
  const currentElicitation = requestedApproval
    ? undefined
    : (requestedElicitation ?? pendingElicitations[0]);
  const showComposerSettings = pendingElicitations.length === 0;
  const composerContext = renderContextStrip?.();

  return (
    <div className="w-full">
      <ComposerPrimitive.Root className="w-full">
        <ComposerCompanion />
        <ComposerSurface
          visualEffect={effects}
          bodyClassName="p-0"
          className="overflow-hidden"
          context={composerContext}
          expanded={!composerIsEmpty}
          contextLayers={
            queuedMessageCount > 0 || activityStatus ? (
              <ComposerLayerStack
                count={queuedMessageCount}
                label={t("queuedMessages", { count: queuedMessageCount })}
                expandLabel={t("expandQueuedMessages")}
                collapseLabel={t("collapseQueuedMessages")}
                footer={
                  activityStatus ? (
                    <ComposerContext data-chat-work-status>
                      {activityStatus}
                    </ComposerContext>
                  ) : undefined
                }
              >
                <ComposerPrimitive.Queue>
                  {() => (
                    <ComposerContext
                      data-chat-queued-message
                      title={t("temporaryQueueNotice")}
                    >
                      <ListStart
                        aria-hidden="true"
                        className="text-muted-foreground size-icon-sm shrink-0"
                      />
                      <QueueItemPrimitive.Text className="min-w-0 flex-1 truncate" />
                      <QueueItemPrimitive.Steer asChild>
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          title={t("sendNow")}
                          aria-label={t("sendNow")}
                          disabled={
                            isWorkStopping ||
                            pendingApprovals.length > 0 ||
                            pendingElicitations.length > 0
                          }
                        >
                          <CornerDownRight />
                          {t("steerQueued")}
                        </Button>
                      </QueueItemPrimitive.Steer>
                      <QueueItemPrimitive.Remove asChild>
                        <Button
                          type="button"
                          size="icon-sm"
                          variant="ghost"
                          title={t("removeQueued")}
                          aria-label={t("removeQueued")}
                        >
                          <Trash2 />
                        </Button>
                      </QueueItemPrimitive.Remove>
                    </ComposerContext>
                  )}
                </ComposerPrimitive.Queue>
              </ComposerLayerStack>
            ) : null
          }
        >
          {currentElicitation ? (
            <ChatElicitationCard
              key={currentElicitation.elicitationId}
              elicitation={currentElicitation}
              submissionState={elicitationSubmissionState}
              onResolve={onResolveElicitation}
            />
          ) : currentApproval ? (
            <ChatApprovalCard
              approval={currentApproval}
              current={approvalPosition.current}
              total={approvalPosition.total}
              submissionState={approvalSubmissionState}
              onResolve={onResolveApproval}
            />
          ) : (
            <>
              <ComposerInputBody>
                <ChatComposerInput
                  autoFocus={autoFocus}
                  disabled={isWorkStopping || disabled}
                  placeholder={t("placeholder")}
                  ariaLabel={t("messageInput")}
                  skills={skills}
                  skillsError={skillsError}
                  plugins={plugins}
                  pluginsError={pluginsError}
                  selectedPluginIds={selectedPluginIds}
                  onPluginSelectionChange={onPluginSelectionChange}
                  connectors={connectors}
                  connectorsError={connectorsError}
                  fileRoots={fileRoots}
                  fileScopePath={fileScopePath}
                  fileScopeChatId={fileScopeChatId}
                  members={members}
                  selectedSkillIds={selectedSkillIds}
                  selectedConnectorIds={selectedConnectorIds}
                  onSkillSelectionChange={onSkillSelectionChange}
                  onConnectorSelectionChange={onConnectorSelectionChange}
                  onMemberMentionChange={onMemberMentionChange}
                  onAttachmentAdd={async (path) => {
                    await aui.composer.addAttachment(pathFile(path));
                  }}
                  onAttachmentsChange={refreshComposer}
                  onValueChange={refreshComposer}
                  onOpenFile={onOpenFile}
                  onAttachmentsRemove={async (paths) => {
                    const removedPaths = new Set(paths);
                    const attachments = aui.composer.getState().attachments;
                    for (
                      let index = attachments.length - 1;
                      index >= 0;
                      index--
                    ) {
                      const attachment = attachments[index];
                      const sourcePath = (
                        attachment?.file as AllyPathFile | undefined
                      )?.allySourcePath;
                      if (!sourcePath || !removedPaths.has(sourcePath))
                        continue;
                      await aui.composer.attachment({ index }).remove();
                    }
                  }}
                  onSend={() => {
                    if (!prepareComposerSend()) return;
                    aui.composer.send();
                  }}
                  codeReferenceRequest={codeReferenceRequest}
                  onCodeReferencesChange={onCodeReferencesChange}
                  draft={draft}
                  clearRevision={clearRevision}
                  restoreRevision={restoreRevision}
                  className="placeholder:text-muted-foreground type-label resize-none bg-transparent outline-none disabled:opacity-55"
                />
              </ComposerInputBody>
              <div className="min-h-control-lg flex items-center justify-between gap-2 px-2 pb-2">
                <div className="flex min-w-0 items-center gap-1">
                  <AddAttachmentButton
                    disabled={isWorkStopping || disabled}
                    onAttachmentsChange={refreshComposer}
                  />
                  {actions}
                  <ComposerContextUsage
                    metrics={contextMetrics}
                    visible={showUsageMetrics}
                  />
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  {primaryAction === "stop" && effectiveIsRunning ? (
                    <ComposerPrimitive.Cancel asChild>
                      <ComposerStopButton
                        aria-label={
                          isWorkStopping ? t("team.stoppingWork") : t("stop")
                        }
                        title={
                          isWorkStopping ? t("team.stoppingWork") : t("stop")
                        }
                        data-chat-primary-action="stop"
                        loading={isWorkStopping}
                      />
                    </ComposerPrimitive.Cancel>
                  ) : null}
                  {primaryAction === "stop" && !effectiveIsRunning ? (
                    <ComposerStopButton
                      aria-label={
                        isWorkStopping ? t("team.stoppingWork") : t("stop")
                      }
                      title={
                        isWorkStopping ? t("team.stoppingWork") : t("stop")
                      }
                      data-chat-primary-action="stop"
                      loading={isWorkStopping}
                      onClick={onStopWork}
                    />
                  ) : null}
                  {primaryAction === "send" ? (
                    <ComposerPrimitive.Send asChild>
                      <ComposerSendButton
                        aria-label={t("send")}
                        title={t("send")}
                        disabled={disabled || !composerCanSend}
                        data-chat-primary-action="send"
                        onClickCapture={prepareComposerSend}
                      />
                    </ComposerPrimitive.Send>
                  ) : null}
                  {primaryAction === "queue" ? (
                    <ComposerPrimitive.Send asChild>
                      <ComposerSendButton
                        aria-label={t("addToQueue")}
                        title={t("addToQueue")}
                        data-chat-primary-action="send"
                        disabled={
                          isWorkStopping || disabled || !composerCanSend
                        }
                        onClickCapture={prepareComposerSend}
                      />
                    </ComposerPrimitive.Send>
                  ) : null}
                </div>
              </div>
            </>
          )}
        </ComposerSurface>
      </ComposerPrimitive.Root>
      <div
        data-chat-composer-context-row
        className="min-h-control-sm mt-1 flex items-center justify-between px-2"
      >
        <div className="flex min-w-0 items-center gap-1 overflow-hidden">
          {showComposerSettings ? settings : null}
        </div>
        {terminalEnabled ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={t("openTerminal")}
            title={t("openTerminal")}
            onClick={onOpenTerminal}
          >
            <Terminal />
          </Button>
        ) : null}
      </div>
    </div>
  );
});

const emptyList: never[] = [];
const ignoreChange = () => undefined;

export function ChatThread({
  fork,
  forkBoundaryMessageId,
  forkSourceChatId,
  onOpenForkSource,
  runtime,
  wakerName,
  wakerId,
  wakerAvatar,
  messageAuthors,
  activityStatus,
  isRunning = false,
  isWorkRunning = false,
  isWorkStopping = false,
  onStopWork,
  emptyState,
  emptyStatePosition = "fill",
  animateComposerSize = false,
  pinEmptyState = false,
  isHistoryLoading = false,
  historyError,
  composerSettings,
  composerActions,
  composerEffects,
  renderComposerContextStrip,
  composerSkills = emptyList,
  composerSkillsError = false,
  composerPlugins = emptyList,
  composerPluginsError = false,
  selectedPluginIds = emptyList,
  onPluginSelectionChange = ignoreChange,
  composerConnectors = emptyList,
  composerConnectorsError = false,
  composerFileRoots = emptyList,
  composerFileScopePath,
  composerMembers = emptyList,
  availableSkillIds,
  availableConnectorIds,
  availablePluginIds,
  selectedSkillIds = emptyList,
  selectedConnectorIds = emptyList,
  onSkillSelectionChange = ignoreChange,
  onConnectorSelectionChange = ignoreChange,
  onMemberMentionChange,
  codeReferenceRequest,
  composerDraft,
  showComposer = true,
  presentation = "thread",
  composerAutoFocus,
  composerDisabled,
  composerClearRevision,
  companionScopeRevision,
  composerRestoreRevision,
  terminalEnabled = false,
  onOpenTerminal,
  onOpenLink,
  linkWorkspaceRoot,
  fileScopeChatId,
  resultSource,
  linkAdditionalWorkingDirectories,
  onCodeReferencesChange,
  onOpenCodeReference,
  onOpenFile,
  onOpenFileChange,
  contextMetrics,
  showUsageMetrics = true,
  hasEarlierMessages,
  isLoadingEarlier,
  onLoadEarlier,
  sendFailure,
  errorScopes = emptyList,
  onRetryExecution,
  onRecoverSession,
  warning,
  requestedInteractionId,
  pendingApprovals = emptyList,
  approvalSubmissionState = "idle",
  onResolveApproval = ignoreChange,
  pendingElicitations = emptyList,
  elicitationSubmissionState = "idle",
  onResolveElicitation = ignoreChange,
}: ChatThreadProps) {
  const { t } = useTranslation("chat");
  const reducedMotion = useReducedMotion();
  const composerLayoutTransition = useMemo(
    () =>
      readLayoutMotionTokens(
        animateComposerSize ? "--duration-slow" : "--duration-layout",
      ),
    [animateComposerSize],
  );
  const viewportRef = useRef<HTMLDivElement>(null);
  useChatHistoryError(historyError, errorScopes.at(-1));
  const historyScroll = useChatHistoryScroll({
    scope: fileScopeChatId ?? errorScopes[0] ?? wakerId ?? "global",
    viewport: viewportRef,
    hasMore: hasEarlierMessages,
    loading: isLoadingEarlier,
    load: async () => {
      const operation = `history-earlier:${errorScopes[0] ?? "global"}`;
      const recovery = captureErrorNoticeRecovery(operation);
      await onLoadEarlier();
      resolveErrorNoticeRecovery(recovery);
    },
    onError: (error) => {
      reportChatError(
        error,
        errorScopes[0] ?? "global",
        t("loadEarlierFailed"),
        `history-earlier:${errorScopes[0] ?? "global"}`,
      );
    },
  });
  const loadEarlier = historyScroll.loadEarlier;
  const loadEarlierError = historyScroll.error ? t("loadEarlierFailed") : "";
  // 外部 Runtime 在被动 Effect 中提交消息。订阅它的已提交快照，保证
  // Provider 与消息原语在同一快照上渲染，不依赖 SSE 或其他父级更新补帧。
  const messageSnapshot = useSyncExternalStore(
    runtime.thread.subscribe,
    runtime.thread.getState,
    runtime.thread.getState,
  );
  const messagesByID = useMemo(
    () =>
      new Map(messageSnapshot.messages.map((message) => [message.id, message])),
    [messageSnapshot.messages],
  );
  const centerEmptyState =
    emptyStatePosition === "center" &&
    !isHistoryLoading &&
    messageSnapshot.messages.length === 0;
  const showDirectEmptyState =
    isHistoryLoading || pinEmptyState || centerEmptyState;
  const latestMessage = messageSnapshot.messages.at(-1);
  const latestAuthor = latestMessage
    ? messageAuthors?.[latestMessage.id]
    : undefined;
  const companionInteraction = isWorkStopping
    ? "stopping"
    : pendingApprovals.length || pendingElicitations.length
      ? "waiting"
      : undefined;
  const companionActivity =
    latestMessage?.role === "assistant"
      ? messageCompanionActivity(
          { ...latestMessage, isLast: true },
          companionInteraction,
        )
      : isWorkRunning
        ? "starting"
        : "idle";

  const referenceContext = useMemo(
    () => ({
      chatId: fileScopeChatId,
      baseDirectory: linkWorkspaceRoot ?? "",
      workspaceRoot: linkWorkspaceRoot ?? "",
      sourceRoots: linkAdditionalWorkingDirectories ?? emptyList,
    }),
    [fileScopeChatId, linkWorkspaceRoot, linkAdditionalWorkingDirectories],
  );
  const openReferencedFile = useCallback(
    (file: ResolvedFileReference) => onOpenFile?.({ ...file, reference: file }),
    [onOpenFile],
  );

  return (
    <FileReferenceProvider
      onOpenURL={onOpenLink}
      onOpenFile={openReferencedFile}
      context={referenceContext}
    >
      {/* draft → chat 会切换 runtime；会话清理由 clearRevision 显式完成，不重建编辑器。 */}
      <ChatForkContext value={fork}>
        <ChatErrorActions
          value={{
            retry: onRetryExecution,
            recover: onRecoverSession,
            disabled: isRunning,
            begin: (operation) => {
              const recovery = captureErrorNoticeRecovery(
                `${operation}:${errorScopes[0] ?? "global"}`,
              );
              return () => resolveErrorNoticeRecovery(recovery);
            },
            report: (message, operation) =>
              reportErrorNotice({
                id: `chat-local:${crypto.randomUUID()}`,
                scope: errorScopes[0] ?? "global",
                operation: `${operation}:${errorScopes[0] ?? "global"}`,
                message,
              }),
          }}
        >
          <AssistantRuntimeProvider runtime={runtime}>
            <CompanionInteractionProvider
              scope={`${wakerId ?? ""}:${companionScopeRevision ?? 0}`}
              activeAvatar={companionAvatar(
                latestAuthor?.avatar ?? wakerAvatar,
                latestAuthor?.name ?? wakerName,
              )}
              activity={companionActivity}
            >
              <ThreadPrimitive.Root
                className={cn(
                  "flex h-full min-h-0 flex-col",
                  centerEmptyState && "justify-center",
                )}
              >
                {presentation === "thread" &&
                resultSource &&
                !isHistoryLoading &&
                messageSnapshot.messages.length > 0 &&
                /^[a-f0-9]{32}$/.test(resultSource.chatId) ? (
                  <ChatWorkResultReader
                    chatId={resultSource.chatId}
                    owner={resultSource.owner}
                    viewport={viewportRef}
                    hasEarlierMessages={hasEarlierMessages}
                    onLoadEarlier={loadEarlier}
                  />
                ) : null}
                {presentation === "thread" ? (
                  <div
                    className={cn(
                      "relative flex min-h-0",
                      centerEmptyState ? "shrink-0" : "flex-1",
                    )}
                  >
                    {showDirectEmptyState ? (
                      <div
                        role={isHistoryLoading ? "status" : undefined}
                        aria-busy={isHistoryLoading ? true : undefined}
                        className="flex min-h-0 flex-1 flex-col overflow-y-auto"
                      >
                        <div className="min-h-0 flex-1">{emptyState}</div>
                        <div className="px-6">
                          <ChatErrorNotices
                            scopes={errorScopes}
                            fallback={sendFailure}
                          />
                        </div>
                      </div>
                    ) : (
                      <>
                        <ThreadPrimitive.Viewport
                          ref={viewportRef}
                          data-chat-scroll-viewport
                          onScroll={historyScroll.onScroll}
                          className="scrollbar-hidden relative flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain [overflow-anchor:none]"
                        >
                          <ThreadPrimitive.Empty>
                            {emptyState}
                          </ThreadPrimitive.Empty>
                          <ChatMessageColumn>
                            {historyError && errorScopes.length === 0 ? (
                              <ChatErrorCard
                                id={`history:${errorScopes.join(":")}`}
                                message={historyError.message}
                                onRetry={historyError.onRetry}
                              />
                            ) : null}
                            {hasEarlierMessages && (
                              <div className="flex flex-col items-center gap-1 px-6 pb-2">
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="sm"
                                  disabled={isLoadingEarlier}
                                  onClick={() => void loadEarlier()}
                                >
                                  {isLoadingEarlier && (
                                    <LoaderCircle className="animate-spin" />
                                  )}
                                  {t("loadEarlierMessages")}
                                </Button>
                                {loadEarlierError &&
                                errorScopes.length === 0 ? (
                                  <ChatErrorCard
                                    id={`earlier:${errorScopes.join(":")}`}
                                    message={loadEarlierError}
                                    onRetry={loadEarlier}
                                    onDismiss={historyScroll.clearError}
                                  />
                                ) : null}
                              </div>
                            )}
                            <CompanionInteraction value={companionInteraction}>
                              <ChatMessageList
                                messagesByID={messagesByID}
                                forkBoundaryMessageId={forkBoundaryMessageId}
                                forkSourceChatId={forkSourceChatId}
                                onOpenForkSource={onOpenForkSource}
                                name={wakerName}
                                avatar={wakerAvatar}
                                availableSkillIds={availableSkillIds}
                                availableConnectorIds={availableConnectorIds}
                                availablePluginIds={availablePluginIds}
                                wakerId={wakerId}
                                messageAuthors={messageAuthors}
                                showUsageMetrics={showUsageMetrics}
                                onOpenCodeReference={onOpenCodeReference}
                                onOpenFile={onOpenFile}
                                onOpenFileChange={onOpenFileChange}
                              />
                            </CompanionInteraction>
                            <div className="px-6">
                              <ChatErrorNotices
                                scopes={errorScopes}
                                fallback={sendFailure}
                              />
                            </div>
                          </ChatMessageColumn>
                        </ThreadPrimitive.Viewport>
                        <ChatTurnNavigator viewportRef={viewportRef} />
                      </>
                    )}
                  </div>
                ) : (
                  <ChatErrorNotices
                    scopes={errorScopes}
                    fallback={sendFailure}
                  />
                )}
                <motion.div
                  data-chat-composer
                  layout={animateComposerSize ? true : "position"}
                  layoutDependency={centerEmptyState}
                  transition={{
                    layout: reducedMotion
                      ? { duration: 0 }
                      : composerLayoutTransition,
                  }}
                  hidden={!showComposer}
                  className="shrink-0 bg-transparent pt-2"
                >
                  <div
                    className={cn(
                      "mx-auto w-full",
                      presentation === "thread" && "px-6 pb-4",
                      chatContentColumnClassName,
                    )}
                  >
                    {warning ? (
                      <div
                        role="status"
                        data-chat-permission-warning
                        className="border-border bg-muted text-muted-foreground type-ui mb-2 flex w-full items-start gap-2 rounded-md border px-3 py-2"
                      >
                        <AlertTriangle
                          className="text-destructive size-icon-md mt-0.5 shrink-0"
                          strokeWidth={1.5}
                        />
                        <span className="min-w-0">{warning}</span>
                      </div>
                    ) : null}
                    <Composer
                      autoFocus={composerAutoFocus}
                      disabled={composerDisabled}
                      activityStatus={activityStatus}
                      settings={composerSettings}
                      actions={composerActions}
                      effects={composerEffects}
                      renderContextStrip={renderComposerContextStrip}
                      skills={composerSkills}
                      skillsError={composerSkillsError}
                      plugins={composerPlugins}
                      pluginsError={composerPluginsError}
                      selectedPluginIds={selectedPluginIds}
                      onPluginSelectionChange={onPluginSelectionChange}
                      connectors={composerConnectors}
                      connectorsError={composerConnectorsError}
                      fileRoots={composerFileRoots}
                      fileScopePath={composerFileScopePath}
                      fileScopeChatId={fileScopeChatId}
                      members={composerMembers}
                      selectedSkillIds={selectedSkillIds}
                      selectedConnectorIds={selectedConnectorIds}
                      onSkillSelectionChange={onSkillSelectionChange}
                      onConnectorSelectionChange={onConnectorSelectionChange}
                      onMemberMentionChange={onMemberMentionChange}
                      contextMetrics={contextMetrics}
                      showUsageMetrics={showUsageMetrics}
                      sendFailure={sendFailure}
                      codeReferenceRequest={codeReferenceRequest}
                      onCodeReferencesChange={onCodeReferencesChange}
                      draft={composerDraft}
                      clearRevision={composerClearRevision}
                      restoreRevision={composerRestoreRevision}
                      isRunning={isRunning}
                      terminalEnabled={terminalEnabled}
                      onOpenTerminal={onOpenTerminal}
                      onOpenFile={onOpenFile}
                      requestedInteractionId={requestedInteractionId}
                      pendingApprovals={pendingApprovals}
                      approvalSubmissionState={approvalSubmissionState}
                      onResolveApproval={onResolveApproval}
                      pendingElicitations={pendingElicitations}
                      elicitationSubmissionState={elicitationSubmissionState}
                      onResolveElicitation={onResolveElicitation}
                      isWorkRunning={isWorkRunning}
                      isWorkStopping={isWorkStopping}
                      onStopWork={onStopWork}
                    />
                  </div>
                </motion.div>
              </ThreadPrimitive.Root>
            </CompanionInteractionProvider>
          </AssistantRuntimeProvider>
        </ChatErrorActions>
      </ChatForkContext>
    </FileReferenceProvider>
  );
}
