import {
  localizedDateTimeFormat,
  localizedNumberFormat,
} from "@/shared/i18n/format";
import { messageCompanionActivity } from "../runtime/companion-activity";
import { chatContinuations, type ChatContinuation } from "./chat-continuation";
import {
  groupTeamReports,
  TeamReportGroup,
  type TeamReportGroupRow,
} from "./team-report-group";
import {
  ChatMessagePluginReferences,
  chatPluginReferences,
  type ChatPluginIcons,
  type ChatPluginOption,
} from "./chat-plugin-reference";
import { useChatHistoryScroll } from "./use-chat-history-scroll";
import { ChatHistoryLoadingState } from "./chat-history-loading-state";
import { useChatMessageLocator } from "./chat-message-locator";
import {
  ChatMessageReplyActions,
  ComposerReplyContext,
  MessageReplyAction,
  MessageReplyQuote,
  useSelectMessageReply,
} from "./chat-message-reply";
import {
  useChatReading,
  usePresentedChatBoundary,
  useVisibleScene,
} from "@/shared/api/chat-reading";
import { ChatForkContext, MessageForkAction } from "./chat-fork";
import {
  Link,
  useHref,
  useInRouterContext,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { pluginDetailHref, usePluginReferenceIcons } from "@/modules/plugin";
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
  useChatErrorNotices,
  MessageFailure,
  useChatHistoryError,
} from "./chat-error-card";
import {
  FileReferenceProvider,
  FileReferenceText,
} from "@/shared/workspace/file-reference-link";
import {
  AssistantRuntimeProvider,
  ComposerPrimitive,
  QueueItemPrimitive,
  useAui,
  useAuiState,
  type AssistantRuntime,
} from "@assistant-ui/react";
import {
  AlertTriangle,
  Brain,
  Cable,
  Check,
  ChevronRight,
  CircleEllipsis,
  Ellipsis,
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
  createContext,
  useContext,
  useEffect,
  useLayoutEffect,
  useCallback,
  memo,
  useId,
  useMemo,
  useReducer,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
  type ComponentProps,
} from "react";
import { useTranslation } from "react-i18next";
import { motion, useReducedMotion } from "motion/react";

import type { ResolvedFileReference } from "@/shared/workspace/file-reference";
import { cn } from "@/shared/lib/cn";
import { useContentVisibility } from "@/shared/lib/content-presentation";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { Avatar } from "@/shared/ui/avatar";
import { Collapsible, CollapsibleContent } from "@/shared/ui/collapsible";
import { Button } from "@/shared/ui/button";
import { RetainedPresence } from "@/shared/ui/retained-presence";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import {
  ComposerContext,
  ComposerLayerStack,
  ComposerSendButton,
  ComposerStopButton,
  ComposerSurface,
  ComposerInputBody,
} from "@/shared/ui/composer";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { selectFiles } from "@/shared/platform";
import { pathFile, type BotPathFile } from "../runtime/chat-attachment-adapter";
import {
  type BotChatMessage,
  type ChatPendingElicitation,
  type ChatReplyDraftEditor,
  type ChatReplyReference,
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
  type ChatComposerDraftReference,
  type ChatComposerContextReference,
} from "./chat-composer-input";
import type { ChatWorkflowPicker } from "./chat-workflow-reference";
import { ChatMessageReferenceChip } from "./chat-reference-chip";
import {
  CancelWorkflowInvocation,
  type WorkflowInvocationStop,
} from "./chat-workflow-cancel";
import { chatContentColumnClassName } from "./chat-layout";
import {
  ChatMessageConnectorReferences,
  type ChatConnectorOption,
} from "./chat-connector-reference";
import { ChatContextUsage } from "./chat-context-usage";
import {
  ChatMessageAttachments,
  ChatMessageSkillReferences,
  chatMessageReferenceChipClass,
  type ChatFileOpenRequest,
  type ChatSkillOption,
} from "./chat-reference-menus";
import { AssistantMarkdown, UserMarkdown } from "./chat-message-content";
import { ChatTurnNavigator } from "./chat-turn-navigator";
import { ChatExcalidraw } from "./chat-excalidraw";

import { Fragment } from "react";
import { ExecutionFileChangesAction } from "./execution-file-changes-card";
import { AutomaticTaskCreationCard } from "./automatic-task-creation-card";
import {
  ChatMessageProjection,
  timelineEventsInMessageWindow,
  viewCompanionActivity,
  isTerminalMessage,
  type ChatMessageView,
  type ChatPartNode,
  type ChatToolGroup,
  type ChatProcessGroup,
} from "./chat-message-nodes";
import { useChatViewportScroll } from "./use-chat-viewport-scroll";
type DisplayPart = NonNullable<BotChatMessage["displayParts"]>[number];

const CompanionInteraction = createContext<"waiting" | "stopping" | undefined>(
  undefined,
);

const ProcessExpansion = createContext<Map<string, boolean> | undefined>(
  undefined,
);

type ChatThreadProps = {
  fork?: React.ContextType<typeof ChatForkContext>;
  forkBoundaryMessageId?: string;
  forkSourceChatId?: string;
  onOpenForkSource?: (chatId: string) => Promise<boolean>;
  active?: boolean;
  presentationScopeKey?: string;
  runtime: AssistantRuntime;
  messages: readonly BotChatMessage[];
  wakerName: string;
  wakerId?: string;
  wakerAvatar?: string;
  /** 团队作者索引；空索引沿用当前成员身份，并保留历史回复的身份展示。 */
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  /** 团队成员名称索引，用于原消息尚未加载时展示引用作者。 */
  teamReplyAuthorNames?: Readonly<Record<string, string>>;
  timelineEvents?: readonly ChatTimelineEvent[];
  workflowInvocation?: WorkflowInvocationStop;
  activityStatus?: ReactNode;
  afterMessages?: ReactNode;
  isRunning?: boolean;
  isWorkRunning?: boolean;
  isWorkStopping?: boolean;
  onStopWork?: () => void;
  showUsageMetrics?: boolean;
  showFileChanges?: boolean;
  emptyState: ReactNode;
  emptyStatePosition?: "center" | "fill";
  /** 画布首发时同步输入框的位置与宽度，保留同一编辑器实例。 */
  animateComposerSize?: boolean;
  pinEmptyState?: boolean;
  /** 已有对话的路由身份可先于所属 Chat ID 就绪。 */
  hasExistingConversation?: boolean;
  isHistoryLoading?: boolean;
  historyError?: { message: string; onRetry: () => void; error?: unknown };
  renderComposerSettings?: (simplified: boolean) => ReactNode;
  composerInputSize?: "compact" | "tall";
  composerActions?: ReactNode;
  composerEffects?: ReactNode;
  composerReferences?: ChatComposerContextReference[];
  onComposerReferencesChange?: (ids: string[]) => void;
  renderComposerContextStrip?: () => ReactNode;
  composerSkills?: ChatSkillOption[];
  composerSkillsError?: boolean;
  composerPlugins?: ChatPluginOption[];
  composerPluginsError?: boolean;
  composerWorkflowPicker?: ChatWorkflowPicker;
  composerSendBlocked?: boolean;
  selectedPluginIds?: string[];
  onPluginSelectionChange?: (ids: string[]) => void;
  composerConnectors?: ChatConnectorOption[];
  composerConnectorsError?: boolean;
  composerFileRoots?: string[];
  composerFileScopePath?: string;
  composerMembers?: ChatMemberMentionOption[];
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  selectedSkillIds?: string[];
  selectedConnectorIds?: string[];
  onSkillSelectionChange?: (skillIds: string[]) => void;
  onConnectorSelectionChange?: (connectorIds: string[]) => void;
  onMemberMentionChange?: (wakerId?: string) => void;
  codeReferenceRequest?: ChatCodeReference;
  composerDraft?: string;
  composerDraftReference?: ChatComposerDraftReference;
  replyTo?: ChatReplyReference;
  onReplyToChange?: (replyTo: ChatReplyReference | undefined) => void;
  registerReplyDraftEditor?: (editor: ChatReplyDraftEditor) => () => void;
  showComposer?: boolean;
  /** 完整输入区可独立用于业务指令，不呈现消息列表或产生阅读事实。 */
  presentation?: "thread" | "composer";
  composerAutoFocus?: boolean;
  composerDisabled?: boolean;
  /** 首发获得对话标识前只显示发送操作。 */
  composerSingleSubmission?: boolean;
  composerClearRevision?: number;
  composerRestoreRevision?: number;
  onOpenLink?: (url: string) => boolean;
  linkWorkspaceRoot?: string;
  fileScopeChatId?: string;
  /** 真实聊天与成果归属独立于文件授权；所有共享聊天入口沿用同一阅读器。 */
  readingChatId?: string;
  linkAdditionalWorkingDirectories?: readonly string[];
  onCodeReferencesChange?: (
    references: ChatCodeReference[],
    codeReferenceOnly: boolean,
  ) => void;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  contextMetrics?: ChatRunMetrics;
  sendFailure: ChatSendFailure;
  errorScopes?: readonly string[];
  onRecoverSession?: () => void;
  hasEarlierMessages: boolean;
  isLoadingEarlier: boolean;
  onLoadEarlier: () => Promise<void>;
  warning?: string;
  warningAction?: ReactNode;
  requestedInteractionId?: string;
  pendingApprovals?: PendingApproval[];
  approvalSubmissionState?: ApprovalSubmissionState;
  onResolveApproval?: (approvalId: string, optionId: string) => void;
  pendingElicitations?: ChatPendingElicitation[];
  elicitationSubmissionState?: ElicitationSubmissionState;
  onResolveElicitation?: (
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => void;
};

export type ChatTimelineEvent = {
  id: string;
  createdAt: number;
  sequence?: number;
  beforeMessageId?: string;
  afterMessageId?: string;
  content: ReactNode;
};

export type ChatMessageAuthor = {
  wakerId?: string;
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

function toolArgsOf(part: DisplayPart): ChatToolArgs {
  return {
    category: toolCategoryKeys.includes(part.toolCategory as ToolCategory)
      ? (part.toolCategory as ToolCategory)
      : "other",
    action: toolActionKeys.includes(part.toolAction as ToolAction)
      ? (part.toolAction as ToolAction)
      : "other",
    label: part.toolLabel,
    loadingLabel: part.toolLoadingLabel,
    safeSummary: part.toolSafeSummary,
    approvalResult: part.toolApprovalResult,
    status:
      part.toolStatus === "completed" || part.toolStatus === "failed"
        ? part.toolStatus
        : "running",
  };
}
function categoryFromTool(part: DisplayPart): ToolCategory {
  return toolArgsOf(part).category;
}

type ToolActivityStatus = ChatToolArgs["status"] | "interrupted";

type ToolSummaryWords = {
  completedPrefix: string;
  runningPrefix: string;
  interruptedPrefix: string;
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
    status: ToolActivityStatus;
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
        activity.status === "interrupted"
          ? words.interruptedPrefix
          : activity.status === "running"
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
  part: DisplayPart,
  terminal: boolean,
): ToolActivityStatus {
  const status = toolArgsOf(part).status;
  return terminal && status === "running" ? "interrupted" : status;
}

export function ReasoningPart({
  text,
  running = false,
}: {
  text: string;
  running?: boolean;
}) {
  const { t } = useTranslation("chat");
  const [expanded, setExpanded] = useState(false);
  const titleRef = useRef<HTMLDivElement>(null);
  const sceneVisible = useContext(ContentVisibility);
  const displayText = text.replace(/^[\r\n]+/, "");
  const canToggle = displayText.length > 120;
  const shimmer = useContentVisibility(
    titleRef,
    sceneVisible && running && !expanded && Boolean(displayText),
  );
  if (!displayText) return null;
  return (
    <div className="space-y-1.5">
      <div
        ref={titleRef}
        className="text-muted-foreground type-label relative flex w-fit max-w-full items-center gap-2 overflow-hidden rounded-md"
        data-running-shimmer={shimmer ? "true" : undefined}
        aria-busy={running}
      >
        <Brain className="size-icon-sm" aria-hidden="true" />
        <span>{t(running ? "thinking" : "reasoningActivity")}</span>
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
            aria-expanded={expanded}
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

export const ToolActivityRow = memo(function ToolActivityRow({
  part,
  terminal = false,
}: {
  part: DisplayPart;
  terminal?: boolean;
}) {
  const { t } = useTranslation("chat");
  const toolArgs = toolArgsOf(part);
  const toolStatus = statusFromTool(part, terminal);
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
  const compactFileModification =
    terminal &&
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
            {toolStatus === "interrupted" ? (
              <span>{t("toolActivitySummary.interruptedPrefix")}</span>
            ) : null}
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
});

export const WorkProcess = memo(function WorkProcess({
  group,
  current = false,
  terminal = false,
  children,
}: {
  group: ChatToolGroup;
  current?: boolean;
  terminal?: boolean;
  children?: ReactNode;
}) {
  const { t } = useTranslation("chat");
  const reading = useContext(ProcessExpansion);
  const [open, setOpen] = useState(() => reading?.get(group.key) ?? false);
  const contentId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const tools = group.nodes.map((node) => node.part);
  const categories = tools.reduce<ToolCategory[]>((ordered, tool) => {
    const category = categoryFromTool(tool);
    if (!ordered.includes(category)) ordered.push(category);
    return ordered;
  }, []);
  const activities: {
    category: ToolCategory;
    status: ToolActivityStatus;
    labels: string[];
  }[] = [];
  for (const tool of tools) {
    const category = categoryFromTool(tool);
    // 汇总只描述活动；失败状态留在对应工具行，不生成标题级失败总结。
    const actualStatus = statusFromTool(tool, terminal);
    const status = actualStatus === "failed" ? "completed" : actualStatus;
    let activity = activities.find(
      (item) => item.category === category && item.status === status,
    );
    if (!activity) {
      activity = { category, status, labels: [] };
      activities.push(activity);
    }
    const label = toolArgsOf(tool).label;
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
      interruptedPrefix: t("toolActivitySummary.interruptedPrefix"),
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
  const isCurrentToolGroup = current;
  const toggleOpen = () => {
    if (open) triggerRef.current?.focus({ preventScroll: true });
    reading?.set(group.key, !open);
    setOpen(!open);
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
            {children ??
              group.nodes.map((node) => (
                <div key={node.key} data-chat-node-id={node.key}>
                  <ToolActivityRow part={node.part} terminal={terminal} />
                </div>
              ))}
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
});

const MessageWorkProcess = memo(function MessageWorkProcess({
  group,
  currentKey,
  reasoningRunning,
  completed,
  terminal,
  metrics,
  showUsageMetrics,
  focusedProcessElementRef,
}: {
  group: ChatProcessGroup;
  currentKey?: string;
  reasoningRunning: boolean;
  completed: boolean;
  terminal: boolean;
  metrics?: ChatRunMetrics;
  showUsageMetrics: boolean;
  focusedProcessElementRef: RefObject<Element | null>;
}) {
  const { t } = useTranslation("chat");
  const durationMs = metrics?.durationMs;
  const reading = useContext(ProcessExpansion);
  const [expanded, setExpanded] = useState(
    () => reading?.get(group.key) ?? false,
  );
  const triggerRef = useRef<HTMLButtonElement>(null);
  // 完成前后保留同一容器，让 open → closed 触发现有高度退出动画。
  const open = !completed || expanded;
  const contentId = useId();
  useLayoutEffect(() => {
    const focused = focusedProcessElementRef.current;
    // 完成时多个过程段会合并，焦点也可能来自已卸载的后续过程段。
    if (
      !open &&
      focused &&
      !document.hidden &&
      (document.activeElement === focused ||
        document.activeElement === document.body)
    ) {
      focusedProcessElementRef.current = null;
      triggerRef.current?.focus({ preventScroll: true });
    }
  }, [open, focusedProcessElementRef]);
  const label =
    durationMs !== undefined
      ? t("completedProcessDuration", {
          duration: formatRunDuration(durationMs),
        })
      : t("completedProcess");
  const trigger = (
    <button
      type="button"
      className="text-muted-foreground hover:text-foreground type-label min-h-control-sm inline-flex items-center gap-1 py-1 text-left transition-colors"
      aria-expanded={open}
      aria-controls={contentId}
      ref={triggerRef}
      onClick={() => {
        reading?.set(group.key, !expanded);
        setExpanded(!expanded);
      }}
    >
      <span>{label}</span>
      <ChevronRight
        className={cn(
          "size-icon-xs transition-transform motion-reduce:transition-none",
          open && "rotate-90",
        )}
        aria-hidden="true"
      />
    </button>
  );
  return (
    <Collapsible open={open} onOpenChange={setExpanded}>
      {!completed ? null : metrics && showUsageMetrics ? (
        <RunMetricsTooltip metrics={metrics}>{trigger}</RunMetricsTooltip>
      ) : (
        trigger
      )}
      <CollapsibleContent
        onFocusCapture={(event) => {
          focusedProcessElementRef.current = event.target;
        }}
        onBlurCapture={(event) => {
          if (
            focusedProcessElementRef.current === event.target &&
            event.relatedTarget instanceof Node &&
            !event.currentTarget.contains(event.relatedTarget)
          )
            focusedProcessElementRef.current = null;
        }}
        id={contentId}
        data-motion={completed ? "presence" : "none"}
        inert={!open}
        aria-hidden={!open}
      >
        <div className={cn("space-y-4", completed && "pt-2")}>
          {group.nodes.map((node) =>
            "kind" in node ? (
              <WorkProcess
                key={node.key}
                group={node}
                current={!terminal && node.nodes.at(-1)?.key === currentKey}
                terminal={terminal}
              />
            ) : (
              <ChatPart
                key={node.key}
                node={node}
                terminal={terminal}
                reasoningRunning={reasoningRunning && node.key === currentKey}
              />
            ),
          )}
        </div>
      </CollapsibleContent>
      {completed && <hr className="border-border/60 mt-2 border-0 border-t" />}
    </Collapsible>
  );
});

const ChatPart = memo(function ChatPart({
  node,
  terminal,
  reasoningRunning,
}: {
  node: ChatPartNode;
  terminal: boolean;
  reasoningRunning?: boolean;
}) {
  const { part } = node;
  return (
    <div data-chat-node-id={node.key}>
      {part.type === "text" ? (
        <AssistantMarkdown text={part.text ?? ""} isTerminal={terminal} />
      ) : part.type === "reasoning" ? (
        <ReasoningPart text={part.text ?? ""} running={reasoningRunning} />
      ) : part.type === "process_commentary" ? (
        <p className="type-chat text-foreground max-w-4xl whitespace-pre-wrap">
          {part.text}
        </p>
      ) : part.type === "tool" ? (
        <ToolActivityRow part={part} terminal={terminal} />
      ) : null}
    </div>
  );
});
function ChatMessageParts({
  view,
  metrics = view.message.runMetrics,
  reasoningRunning,
  showUsageMetrics,
}: {
  view: ChatMessageView;
  metrics?: ChatRunMetrics;
  reasoningRunning: boolean;
  showUsageMetrics: boolean;
}) {
  const terminal = isTerminalMessage(view.message);
  const focusedProcessElementRef = useRef<Element | null>(null);
  const finalSegment = view.segments.at(-1);
  const finalNode =
    finalSegment && "kind" in finalSegment
      ? finalSegment.nodes.at(-1)
      : finalSegment;
  const currentKey =
    finalNode && "kind" in finalNode
      ? finalNode.nodes.at(-1)?.key
      : finalNode?.key;
  return (
    <div className="space-y-4">
      {view.segments.map((segment) =>
        "kind" in segment ? (
          <MessageWorkProcess
            key={segment.key}
            group={segment}
            currentKey={currentKey}
            reasoningRunning={reasoningRunning}
            completed={view.message.status === "completed"}
            terminal={terminal}
            metrics={metrics}
            showUsageMetrics={showUsageMetrics}
            focusedProcessElementRef={focusedProcessElementRef}
          />
        ) : (
          <ChatPart key={segment.key} node={segment} terminal={terminal} />
        ),
      )}
    </div>
  );
}

const durationNumberFormat = localizedNumberFormat({
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});
const tokenNumberFormat = localizedNumberFormat();
const messageTimeFormat = localizedDateTimeFormat({
  hour: "2-digit",
  minute: "2-digit",
});
const messageDateTimeFormat = localizedDateTimeFormat({
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

function MessageTimestamp({ timestamp }: { timestamp?: number }) {
  if (timestamp === undefined || timestamp <= 0) return null;
  const createdAt = new Date(timestamp);
  if (!Number.isFinite(createdAt.getTime())) return null;
  return (
    <time
      className="text-muted-foreground type-caption tabular-nums"
      dateTime={createdAt.toISOString()}
    >
      {formatMessageTime(createdAt)}
    </time>
  );
}

function formatRunDuration(durationMs: number) {
  if (durationMs < 1000) return `${Math.max(0, Math.round(durationMs))}ms`;
  const seconds = durationMs / 1000;
  if (seconds < 60) return `${durationNumberFormat.format(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${durationNumberFormat.format(seconds % 60)}s`;
}

function RunMetricsTooltip({
  metrics,
  children,
}: {
  metrics: ChatRunMetrics;
  children: ReactNode;
}) {
  const { t } = useTranslation("chat");
  const durationMs = metrics.durationMs ?? 0;
  const usage = metrics.tokenUsage;
  const tokens = usage
    ? usage.knownTokens
    : {
        reportedInput: undefined,
        input: metrics.inputTokens || undefined,
        output: metrics.outputTokens || undefined,
        reasoning: metrics.reasoningOutputTokens || undefined,
        cacheWrite: metrics.cacheCreationTokens || undefined,
        cacheRead: metrics.cacheReadTokens || undefined,
      };
  const rows = [
    metrics.contextUsedTokens && metrics.contextWindowTokens
      ? [
          t("runMetrics.contextUsed"),
          `${formatTokenCount(metrics.contextUsedTokens)} / ${formatTokenCount(metrics.contextWindowTokens)} (${contextUsagePercent(metrics)}%)`,
        ]
      : undefined,
    [t("runMetrics.duration"), formatRunDuration(durationMs)],
    tokens?.input != null
      ? [t("runMetrics.inputTokens"), tokenNumberFormat.format(tokens.input)]
      : tokens?.reportedInput != null
        ? [
            t("runMetrics.reportedInputTokens"),
            tokenNumberFormat.format(tokens.reportedInput),
          ]
        : undefined,
    tokens?.output != null
      ? [t("runMetrics.outputTokens"), tokenNumberFormat.format(tokens.output)]
      : metrics.latestRequestOutputTokens != null
        ? [
            t("runMetrics.latestRequestOutputTokens"),
            tokenNumberFormat.format(metrics.latestRequestOutputTokens),
          ]
        : undefined,
    tokens?.reasoning != null
      ? [
          t("runMetrics.reasoningOutputTokens"),
          tokenNumberFormat.format(tokens.reasoning),
        ]
      : undefined,
    tokens?.cacheWrite != null
      ? [
          t("runMetrics.cacheCreationTokens"),
          tokenNumberFormat.format(tokens.cacheWrite),
        ]
      : undefined,
    tokens?.cacheRead != null
      ? [
          t("runMetrics.cacheReadTokens"),
          tokenNumberFormat.format(tokens.cacheRead),
        ]
      : undefined,
  ].filter((row): row is string[] => Boolean(row));

  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
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

export function RunMetricsBadge({ metrics }: { metrics: ChatRunMetrics }) {
  const { t } = useTranslation("chat");
  return (
    <RunMetricsTooltip metrics={metrics}>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground type-body py-1 font-normal tabular-nums transition-colors"
        aria-label={t("runMetrics.details")}
      >
        {formatRunDuration(metrics.durationMs ?? 0)}
      </button>
    </RunMetricsTooltip>
  );
}

export function MessageRunStatus({
  message,
  showUsageDetails = true,
}: {
  message: BotChatMessage;
  showUsageDetails?: boolean;
}) {
  const runMetrics = message.runMetrics;
  if (!isTerminalMessage(message)) return null;
  const hasCompletedProcess =
    message.status === "completed" &&
    message.displayParts?.some(
      (part) =>
        part.type === "tool" ||
        part.type === "reasoning" ||
        part.type === "process_commentary",
    );
  if (hasCompletedProcess || !runMetrics?.durationMs) return null;
  return showUsageDetails ? (
    <RunMetricsBadge metrics={runMetrics} />
  ) : (
    <span className="text-muted-foreground type-body py-1 font-normal tabular-nums">
      {formatRunDuration(runMetrics.durationMs)}
    </span>
  );
}

const messageFooterLayoutClassName =
  "mt-1 flex min-h-control-sm items-center gap-1";

const messageLocationClassName =
  "duration-standard outline-none transition-[background-color] focus-visible:inset-ring-2 focus-visible:inset-ring-ring motion-reduce:transition-none";

function UserMessage({
  message,
  replyQuote,
  availableSkillIds,
  availableConnectorIds,
  pluginIcons,
  wakerId = "",
  onOpenCodeReference,
  onOpenFile,
}: {
  message: BotChatMessage;
  replyQuote?: ReactNode;
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  pluginIcons?: ChatPluginIcons;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
}) {
  const selectReply = useSelectMessageReply(message);
  const skills = (message.skills ?? []).flatMap((skill) =>
    skill.id && skill.name
      ? [{ id: skill.id, name: skill.name, version: skill.version }]
      : [],
  );
  const connectors = (message.connectors ?? []).flatMap((connector) =>
    connector.id && connector.name
      ? [{ id: connector.id, name: connector.name }]
      : [],
  );
  const codeReferences = message.codeReferences ?? [];
  return (
    <div
      data-message-id={message.id}
      data-role="user"
      className="group/user-message mt-1 px-6"
    >
      <div className="ml-auto grid w-fit max-w-[78%] min-w-0 justify-items-end">
        <div
          data-chat-message-bubble
          tabIndex={-1}
          className={cn(
            "bg-user-message-background text-user-message-content type-chat data-[chat-message-located]:bg-user-message-background/85 max-w-full min-w-0 overflow-hidden rounded-xl px-4 py-1.5",
            messageLocationClassName,
          )}
        >
          {replyQuote}
          <div className="flex flex-wrap items-start gap-x-1.5 gap-y-1">
            <ChatMessagePluginReferences
              plugins={message.plugins}
              pluginIcons={pluginIcons}
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
            {message.workflow?.name ? (
              <ChatMessageReferenceChip
                tone="workflow"
                label={message.workflow.name}
              />
            ) : null}
            <ChatMessageAttachments
              attachments={message.attachments}
              onOpenFile={onOpenFile}
            />
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
                  className={`${chatMessageReferenceChipClass} focus-visible:ring-ring outline-none hover:bg-current/20 focus-visible:ring-2`}
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
              <UserMarkdown text={message.content ?? ""} />
            </div>
          </div>
        </div>
        <footer
          className={cn(
            messageFooterLayoutClassName,
            "duration-quick pointer-events-none opacity-0 transition-opacity group-focus-within/user-message:pointer-events-auto group-focus-within/user-message:opacity-100 group-hover/user-message:pointer-events-auto group-hover/user-message:opacity-100 motion-reduce:transition-none [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100",
          )}
        >
          <MessageCopyAction message={message} />
          {selectReply ? <MessageReplyAction onSelect={selectReply} /> : null}
          <MessageReadActions message={message} />
        </footer>
      </div>
    </div>
  );
}

function ParticipantMessage({
  message,
  avatar,
  messageAuthors,
  name,
}: {
  message: BotChatMessage;
  avatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  name: string;
}) {
  const { t } = useTranslation("chat");
  const messageID = message.id ?? "";
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const selectReply = useSelectMessageReply(message, undefined, true);

  return (
    <ChatReplyMessage
      message={message}
      name={displayName}
      identity={<Avatar name={displayName} src={displayAvatar} size="sm" />}
      footer={
        <>
          <MessageCopyAction message={message} />
          {selectReply ? <MessageReplyAction onSelect={selectReply} /> : null}
          <MessageReadActions message={message} />
          <span className="ml-auto shrink-0">
            <MessageTimestamp timestamp={message.createdAt} />
          </span>
        </>
      }
    >
      <div data-chat-readable-body>
        <UserMarkdown text={message.content ?? ""} />
      </div>
      {author?.truncated ? (
        <p className="text-warning type-caption mt-2 inline-flex items-center gap-1">
          <AlertTriangle className="size-icon-xs" aria-hidden="true" />
          {t("team.memberReportTruncated")}
        </p>
      ) : null}
    </ChatReplyMessage>
  );
}

function MessageCopyAction({
  message,
  hideWhenRunning = false,
}: {
  message: BotChatMessage;
  hideWhenRunning?: boolean;
}) {
  const { t } = useTranslation("chat");
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  if (hideWhenRunning && !isTerminalMessage(message)) return null;
  return (
    <Button
      type="button"
      size="icon-sm"
      variant="ghost"
      title={t("copy")}
      aria-label={t("copy")}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(
            message.role === "assistant"
              ? (message.displayParts ?? [])
                  .filter((part) => part.type === "text")
                  .map((part) => part.text ?? "")
                  .join("\n")
              : (message.content ?? ""),
          );
          setCopied(true);
        } catch {
          setCopied(false);
        }
      }}
    >
      {copied ? <Check /> : <Copy />}
    </Button>
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
      className="shrink-0"
    >
      {selecting ? <LoaderCircle className="animate-spin" /> : <Plus />}
    </Button>
  );
}

const assistantMessageRowClassName =
  "grid grid-cols-[32px_minmax(0,1fr)] items-start gap-3 px-6";

/** 普通回复与团队成员交付共用卡片、悬停底栏和消息间距。 */
function ChatReplyMessage({
  message,
  identity,
  name,
  status,
  children,
  footer,
  fillWidth = false,
}: {
  message: BotChatMessage;
  identity: ReactNode;
  name?: string;
  status?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  fillWidth?: boolean;
}) {
  return (
    <div
      data-message-id={message.id}
      data-chat-readable-sequence={
        message.readable && !message.optimistic
          ? message.createdSequence
          : undefined
      }
      data-role={message.role}
      className={cn(
        "group mt-1",
        children && !footer && "pb-4",
        assistantMessageRowClassName,
      )}
    >
      {identity}
      <div
        className={cn(
          "group/reply max-w-full min-w-0",
          fillWidth ? "w-full" : "w-fit",
        )}
      >
        {name ? <div className="type-body mb-2 font-medium">{name}</div> : null}
        {status}
        {children ? (
          <div
            data-chat-message-card
            data-chat-message-bubble
            tabIndex={-1}
            className={cn(
              "bg-message-card-background max-w-full min-w-0 rounded-xl px-4 py-1.5 data-[chat-message-located]:bg-[color-mix(in_srgb,var(--message-card-background),var(--accent-default)_12%)]",
              fillWidth ? "w-full" : "w-fit",
              messageLocationClassName,
            )}
          >
            {children}
          </div>
        ) : null}
        {footer ? (
          <footer
            className={cn(
              messageFooterLayoutClassName,
              "duration-quick pointer-events-none opacity-0 transition-opacity group-focus-within/reply:pointer-events-auto group-focus-within/reply:opacity-100 group-hover/reply:pointer-events-auto group-hover/reply:opacity-100 motion-reduce:transition-none [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100",
            )}
          >
            {footer}
          </footer>
        ) : null}
      </div>
    </div>
  );
}

function AssistantReplyWaitStatus({
  establishingSession,
}: {
  establishingSession?: boolean;
}) {
  const { t } = useTranslation("chat");
  return (
    <div role="status">
      {!establishingSession && (
        <span className="sr-only">{t("awaitingReply")}</span>
      )}
      <RetainedPresence
        present={Boolean(establishingSession)}
        data-motion={establishingSession ? "none" : "presence"}
        className="text-content-secondary type-ui min-h-avatar-sm flex items-center"
      >
        <span className="relative overflow-hidden" data-running-shimmer="true">
          {t("preparingSession")}
        </span>
      </RetainedPresence>
    </div>
  );
}

export function ChatQuestionRow({
  children,
  compact = false,
  interactionId,
}: {
  children: ReactNode;
  compact?: boolean;
  interactionId?: string;
}) {
  return (
    <div
      data-chat-interaction-id={interactionId}
      className={cn("py-2", compact ? "px-6" : assistantMessageRowClassName)}
    >
      <div className={compact ? undefined : "col-start-2 min-w-0"}>
        {children}
      </div>
    </div>
  );
}

export function AssistantMessage({
  view,
  continuation,
  latest = false,
  last = false,
  name,
  avatar,
  messageAuthors,
  onOpenFile,
  onOpenFileChange,
  showUsageMetrics = true,
  showFileChanges = true,
}: {
  view: ChatMessageView;
  continuation?: ChatContinuation;
  latest?: boolean;
  last?: boolean;
  name: string;
  avatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  showUsageMetrics?: boolean;
  showFileChanges?: boolean;
}) {
  const { t } = useTranslation("chat");
  const message = view.message;
  const selectReply = useSelectMessageReply(message, view.segments);
  const messageID = message.id ?? "";
  const hasFileChanges =
    showFileChanges &&
    isTerminalMessage(message) &&
    message.fileChangeRecord?.state !== "pending" &&
    (message.fileChangeRecord?.totalFiles ?? 0) > 0;
  const drawings = isTerminalMessage(message)
    ? message.displayParts?.filter((part) => part.excalidraw)
    : undefined;
  const hasContent =
    view.segments.length > 0 ||
    Boolean(drawings?.length) ||
    Boolean(message.attachments?.length) ||
    (message.status === "canceled" && !continuation?.continued) ||
    (message.status === "failed" && !last);
  const hasVisibleReply =
    hasContent || Boolean(message.automaticTaskCreations?.length);
  const showFooterActions =
    (hasVisibleReply || isTerminalMessage(message)) && !continuation?.continued;
  const isLatestAssistant = latest;
  const interaction = useContext(CompanionInteraction);
  const activityMessage = continuation?.activityMessage ?? message;
  const companionActivity = continuation?.activityMessage
    ? messageCompanionActivity(
        { ...activityMessage, isLast: isLatestAssistant },
        interaction,
      )
    : viewCompanionActivity(view, isLatestAssistant, interaction);
  const waitingForReply =
    isLatestAssistant &&
    !interaction &&
    !hasVisibleReply &&
    (activityMessage.status === "pending" ||
      activityMessage.status === "running");
  const avatarActivity =
    hasVisibleReply &&
    (companionActivity === "thinking" || companionActivity === "working")
      ? "idle"
      : companionActivity;
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const showIdentity = Boolean(messageAuthors);
  const openFileChange = author?.sourceChatId
    ? (file: ChatFileChangeOpenRequest) =>
        onOpenFileChange?.({ ...file, sourceChatId: author.sourceChatId })
    : onOpenFileChange;

  return (
    <ChatReplyMessage
      message={message}
      fillWidth={drawings?.some((part) => Boolean(part.excalidraw?.elements))}
      name={showIdentity ? displayName : undefined}
      status={
        waitingForReply && (
          <AssistantReplyWaitStatus
            establishingSession={activityMessage.preparingSession}
          />
        )
      }
      identity={
        isLatestAssistant || showIdentity ? (
          <Avatar
            name={displayName}
            src={displayAvatar}
            size="sm"
            activity={avatarActivity}
            attention={waitingForReply ? { kind: "working" } : undefined}
            animated={isLatestAssistant || showIdentity}
            interactive={isLatestAssistant || showIdentity}
            terminalHover={showIdentity}
          />
        ) : (
          <div aria-hidden="true" />
        )
      }
      footer={
        showFooterActions || hasFileChanges || selectReply ? (
          <>
            {hasFileChanges ? (
              <ExecutionFileChangesAction
                message={message}
                chatId={author?.sourceChatId}
                onOpenFile={openFileChange}
              />
            ) : null}
            {showFooterActions && isTerminalMessage(message) ? (
              <MessageRunStatus
                message={
                  continuation
                    ? {
                        ...message,
                        runMetrics: continuation.footerMessage.runMetrics,
                      }
                    : message
                }
                showUsageDetails={showUsageMetrics}
              />
            ) : null}
            {showFooterActions ? (
              <MessageCopyAction
                message={continuation?.footerMessage ?? message}
                hideWhenRunning
              />
            ) : null}
            {selectReply ? <MessageReplyAction onSelect={selectReply} /> : null}
            {showFooterActions ? (
              <>
                <MessageForkAction message={message} />
                <MessageReadActions message={message} />
                <span className="ml-auto shrink-0">
                  <MessageTimestamp
                    timestamp={
                      (continuation?.footerMessage ?? message).createdAt
                    }
                  />
                </span>
              </>
            ) : null}
          </>
        ) : null
      }
    >
      {hasContent ? (
        <>
          <div className="min-w-0">
            <ChatMessageParts
              view={view}
              metrics={continuation?.footerMessage.runMetrics}
              reasoningRunning={message.status === "running" && !interaction}
              showUsageMetrics={showUsageMetrics}
            />
            {drawings?.map((part) => (
              <ChatExcalidraw
                key={part.id}
                elements={part.excalidraw?.elements}
              />
            ))}
            {message.attachments?.length ? (
              <div className="flex flex-wrap items-start gap-1 py-1">
                <ChatMessageAttachments
                  attachments={message.attachments}
                  onOpenFile={
                    author?.sourceChatId && onOpenFile
                      ? (file) =>
                          onOpenFile({
                            ...file,
                            sourceChatId: author.sourceChatId,
                          })
                      : onOpenFile
                  }
                />
              </div>
            ) : null}
          </div>
          {message.status === "canceled" && !continuation?.continued ? (
            <p className="text-muted-foreground type-body mt-2">
              {t("canceled")}
            </p>
          ) : null}
          <MessageFailure message={message} last={last} />
        </>
      ) : null}
    </ChatReplyMessage>
  );
}

const ChatMessage = memo(function ChatMessage({
  view,
  continuation,
  latest,
  last,
  name,
  avatar,
  availableSkillIds,
  availableConnectorIds,
  pluginIcons,
  wakerId,
  onOpenCodeReference,
  onOpenFile,
  onOpenFileChange,
  messageAuthors,
  teamReplyAuthorNames,
  showUsageMetrics,
  showFileChanges,
}: {
  view: ChatMessageView;
  continuation?: ChatContinuation;
  latest: boolean;
  last: boolean;
  name: string;
  avatar?: string;
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  pluginIcons?: ChatPluginIcons;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  teamReplyAuthorNames?: Readonly<Record<string, string>>;
  showUsageMetrics?: boolean;
  showFileChanges?: boolean;
}) {
  const message = view.message;
  if (message.question) {
    return message.question.result ? (
      <ChatQuestionRow>
        <ChatElicitationCard
          elicitation={message.question}
          resolution={message.question}
        />
      </ChatQuestionRow>
    ) : null;
  }
  if (message.role === "user") {
    const replyTo = message.replyTo;
    const quotedWakerId = replyTo?.sourceWakerId;
    const quotedWakerName =
      replyTo && teamReplyAuthorNames
        ? (messageAuthors?.[replyTo.messageId]?.name ??
          (quotedWakerId
            ? teamReplyAuthorNames[quotedWakerId]
            : replyTo.role === "assistant"
              ? name
              : undefined))
        : undefined;
    return messageAuthors?.[message.id ?? ""]?.displayAsParticipant ? (
      <ParticipantMessage
        message={message}
        name={name}
        avatar={avatar}
        messageAuthors={messageAuthors}
      />
    ) : (
      <UserMessage
        message={message}
        replyQuote={
          replyTo ? (
            <MessageReplyQuote replyTo={replyTo} authorName={quotedWakerName} />
          ) : undefined
        }
        availableSkillIds={availableSkillIds}
        availableConnectorIds={availableConnectorIds}
        pluginIcons={pluginIcons}
        wakerId={wakerId}
        onOpenCodeReference={onOpenCodeReference}
        onOpenFile={onOpenFile}
      />
    );
  }
  return message.role === "assistant" ? (
    <>
      {message.automaticTaskCreations?.length ? (
        <div className={cn("py-2", assistantMessageRowClassName)}>
          {message.automaticTaskCreations.map((task) => (
            <AutomaticTaskCreationCard
              key={task.taskId}
              task={task}
              readingMessageId={message.id}
              readingSequence={
                message.readable && !message.optimistic
                  ? message.createdSequence
                  : undefined
              }
            />
          ))}
        </div>
      ) : null}
      <AssistantMessage
        view={view}
        continuation={continuation}
        latest={latest}
        last={last}
        name={name}
        avatar={avatar}
        messageAuthors={messageAuthors}
        onOpenFile={onOpenFile}
        onOpenFileChange={onOpenFileChange}
        showUsageMetrics={showUsageMetrics}
        showFileChanges={showFileChanges}
      />
    </>
  ) : null;
});

function ChatReadingSurface({
  chatId,
  active,
  positioning,
  viewport,
  children,
}: {
  chatId: string;
  active: boolean;
  positioning: boolean;
  viewport: RefObject<HTMLDivElement | null>;
  children: ReactNode;
}) {
  const readingActive = active && Boolean(chatId);
  const sceneVisible = useVisibleScene();
  const reading = useChatReading({ chatId, active: readingActive });
  usePresentedChatBoundary({
    root: viewport,
    active: readingActive && sceneVisible && !positioning,
    present: reading.present,
    revision: reading.revision,
  });
  const ready = Boolean(chatId) && reading.state?.complete === true;
  const { markRead, markUnread } = reading;
  const actions = useMemo(
    () => (ready ? { markRead, markUnread } : undefined),
    [ready, markRead, markUnread],
  );
  return <ChatReadingActions value={actions}>{children}</ChatReadingActions>;
}

function ChatMessageReading({
  chatId,
  ...props
}: Omit<ComponentProps<typeof ChatReadingSurface>, "chatId"> & {
  chatId?: string;
}) {
  const inRouter = useInRouterContext();
  return inRouter ? (
    <ChatReadingSurface
      {...props}
      chatId={chatId && /^[a-f0-9]{32}$/.test(chatId) ? chatId : ""}
    />
  ) : (
    props.children
  );
}

const ChatReadingActions = createContext<
  | {
      markUnread: (id: string) => Promise<void>;
      markRead: () => Promise<void>;
    }
  | undefined
>(undefined);

function MessageReadActions({ message }: { message: BotChatMessage }) {
  const reading = useContext(ChatReadingActions);
  const errors = useContext(ChatErrorActions);
  const { t } = useTranslation("chat");
  if (!reading || !message.id || message.readable !== true) return null;
  const id = message.id;
  const report = () => errors.report?.(t("reading.failed"), "chat-reading");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("reading.actions")}
          title={t("reading.actions")}
          variant="ghost"
          size="icon-sm"
        >
          <Ellipsis />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuItem
          onSelect={() => void reading.markUnread(id).catch(report)}
        >
          {t("reading.markUnread")}
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => void reading.markRead().catch(report)}
        >
          {t("reading.markRead")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** 执行回复先占位，提问结束后在回复之前呈现。权威游标和普通消息顺序不变。 */
function orderChatQuestionViews(views: readonly ChatMessageView[]) {
  const questionsByExecution = new Map<string, ChatMessageView[]>();
  for (const view of views) {
    const { executionId, question } = view.message;
    if (!executionId || !question?.result) continue;
    const questions = questionsByExecution.get(executionId);
    if (questions) questions.push(view);
    else questionsByExecution.set(executionId, [view]);
  }
  if (questionsByExecution.size === 0) return views;

  const replies = new Map<string, ChatMessageView>();
  for (const view of views) {
    const { executionId, question, role } = view.message;
    if (
      !executionId ||
      question ||
      role !== "assistant" ||
      !questionsByExecution.has(executionId)
    )
      continue;
    if (!replies.has(executionId) || isTerminalMessage(view.message))
      replies.set(executionId, view);
  }
  if (replies.size === 0) return views;

  const ordered: ChatMessageView[] = [];
  for (const view of views) {
    const { executionId, question } = view.message;
    const reply = executionId ? replies.get(executionId) : undefined;
    if (question?.result && reply) continue;
    if (executionId && reply === view) {
      const questions = questionsByExecution.get(executionId) ?? [];
      ordered.push(
        ...questions.sort(
          (left, right) =>
            (left.message.createdSequence ?? 0) -
            (right.message.createdSequence ?? 0),
        ),
      );
    }
    ordered.push(view);
  }
  return ordered;
}

export const ChatMessageList = memo(function ChatMessageList({
  views,
  timelineEvents = [],
  requestedMessageId,
  requestedMessageKey,
  forkBoundaryMessageId,
  forkSourceChatId,
  onOpenForkSource,
  wakerId,
  ...props
}: Omit<
  ComponentProps<typeof ChatMessage>,
  "view" | "latest" | "last" | "continuation"
> & {
  views: readonly ChatMessageView[];
  requestedMessageId?: string;
  requestedMessageKey?: string;
  timelineEvents?: readonly ChatTimelineEvent[];
  forkBoundaryMessageId?: string;
  forkSourceChatId?: string;
  onOpenForkSource?: (chatId: string) => Promise<boolean>;
}) {
  const { t } = useTranslation("chat");
  const [unavailableSourceId, setUnavailableSourceId] = useState<string>();
  const [expandedReports, setExpandedReports] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const sceneVisible = useVisibleScene();
  const revealKey =
    sceneVisible && requestedMessageId
      ? JSON.stringify([requestedMessageKey, requestedMessageId])
      : undefined;
  const [revealedTarget, setRevealedTarget] = useState<string>();
  const requestedReport = views.find(
    (view) =>
      view.message.id === requestedMessageId &&
      props.messageAuthors?.[view.message.id ?? ""]?.kind === "member-report",
  );
  // 定位身份随导航和 Scene 恢复更新；只展开已加载的真实目标报告。
  if (revealedTarget !== revealKey && (!revealKey || requestedReport)) {
    setRevealedTarget(revealKey);
    if (revealKey && requestedReport)
      setExpandedReports((current) =>
        new Set(current).add(requestedReport.key),
      );
  }

  const messages = views.map((view) => view.message);
  const continuations = chatContinuations(messages);
  const latest = views.findLast((view) => !view.message.question);
  const before = new Map<string, ChatTimelineEvent[]>();
  const after = new Map<string, ChatTimelineEvent[]>();
  const sequenced: ChatTimelineEvent[] = [];
  const timed: ChatTimelineEvent[] = [];
  const hasSequence = views.some(
    ({ message }) => (message.createdSequence ?? 0) > 0,
  );
  for (const event of timelineEvents) {
    const target = event.beforeMessageId ?? event.afterMessageId;
    if (target) {
      const index = event.beforeMessageId ? before : after;
      const existing = index.get(target);
      if (existing) existing.push(event);
      else index.set(target, [event]);
    } else if (hasSequence && (event.sequence ?? 0) > 0) {
      sequenced.push(event);
    } else {
      timed.push(event);
    }
  }
  sequenced.sort(
    (a, b) => (a.sequence ?? 0) - (b.sequence ?? 0) || a.id.localeCompare(b.id),
  );
  timed.sort((a, b) => a.createdAt - b.createdAt || a.id.localeCompare(b.id));
  const rows: Array<ChatMessageView | ChatTimelineEvent | TeamReportGroupRow> =
    [];
  let sequenceCursor = 0;
  let timeCursor = 0;
  for (const view of groupTeamReports(
    orderChatQuestionViews(views),
    props.messageAuthors,
  )) {
    const createdAt =
      "kind" in view
        ? Math.min(
            ...view.members.flatMap((member) =>
              member.views.map((report) => report.message.createdAt ?? 0),
            ),
          )
        : (view.message.createdAt ?? 0);
    const sequence = "kind" in view ? undefined : view.message.createdSequence;
    // 同一聊天的持久事件按游标归位。跨来源且只有发生时间的标记沿用时间定位。
    while (sequence !== undefined && sequence > 0) {
      const event = sequenced[sequenceCursor];
      if (!event || (event.sequence ?? 0) > sequence) break;
      rows.push(event);
      sequenceCursor++;
    }
    while (timeCursor < timed.length) {
      const event = timed[timeCursor];
      if (!event || event.createdAt > createdAt) break;
      rows.push(event);
      timeCursor++;
    }
    if ("kind" in view) {
      const reportIds = new Set(
        view.members.flatMap((member) =>
          member.views.map((report) => report.message.id),
        ),
      );
      rows.push(
        ...timelineEvents.filter(
          (event) =>
            event.beforeMessageId && reportIds.has(event.beforeMessageId),
        ),
        view,
      );
      continue;
    }
    const id = view.message.id;
    if (id) rows.push(...(before.get(id) ?? []));
    rows.push(view);
    if (id) rows.push(...(after.get(id) ?? []));
  }
  rows.push(...sequenced.slice(sequenceCursor), ...timed.slice(timeCursor));
  return (
    <>
      {rows.map((row) => {
        if ("kind" in row)
          return (
            <TeamReportGroup
              key={row.key}
              group={row}
              expanded={expandedReports}
              setExpanded={setExpandedReports}
              renderMessage={(view) => (
                <ChatMessage
                  key={view.key}
                  {...props}
                  wakerId={wakerId}
                  view={view}
                  latest={false}
                  last={false}
                />
              )}
            />
          );
        if ("content" in row)
          return <Fragment key={`event:${row.id}`}>{row.content}</Fragment>;
        const view = row;
        return (
          <Fragment key={view.key}>
            <ChatMessage
              {...props}
              wakerId={wakerId}
              view={view}
              continuation={continuations.get(view.message.id ?? "")}
              latest={view.key === latest?.key}
              last={view.key === latest?.key}
            />
            {view.message.id === forkBoundaryMessageId && forkSourceChatId ? (
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
          </Fragment>
        );
      })}
    </>
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

function ChatMessageColumn({ children }: { children: ReactNode }) {
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
  singleSubmission = false,
  inputSize,
  activityStatus,
  settings,
  actions,
  effects,
  contextReferences,
  onContextReferencesChange,
  renderContextStrip,
  skills,
  skillsError,
  plugins,
  pluginsError,
  workflowPicker,
  sendBlocked = false,
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
  errorScopes,
  failure,
  codeReferenceRequest,
  onCodeReferencesChange,
  draft,
  draftReference,
  replyTo,
  onDismissReply,
  registerReplyDraftEditor,
  focusInputRef,
  clearRevision,
  restoreRevision,
  onOpenFile,
  onOpenPlugin,
  pluginHrefPrefix,
  requestedInteractionId,
  pendingApprovals,
  approvalSubmissionState,
  onResolveApproval,
  waitingForAnswer,
  isWorkRunning,
  isRunning,
  isWorkStopping,
  onStopWork,
}: {
  autoFocus?: boolean;
  disabled?: boolean;
  singleSubmission?: boolean;
  inputSize?: ChatThreadProps["composerInputSize"];
  activityStatus?: ReactNode;
  settings?: ReactNode;
  actions?: ReactNode;
  effects?: ReactNode;
  contextReferences?: ChatComposerContextReference[];
  onContextReferencesChange?: (ids: string[]) => void;
  renderContextStrip?: () => ReactNode;
  skills: ChatSkillOption[];
  skillsError: boolean;
  plugins: ChatPluginOption[];
  pluginsError: boolean;
  workflowPicker?: ChatWorkflowPicker;
  sendBlocked?: boolean;
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
  errorScopes: readonly string[];
  failure?: BotChatMessage;
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (
    references: ChatCodeReference[],
    codeReferenceOnly: boolean,
  ) => void;
  draft?: string;
  draftReference?: ChatComposerDraftReference;
  replyTo?: ChatReplyReference;
  onDismissReply?: () => void;
  registerReplyDraftEditor?: (editor: ChatReplyDraftEditor) => () => void;
  focusInputRef: RefObject<(() => void) | null>;
  clearRevision?: number;
  restoreRevision?: number;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenPlugin?: (pluginId: string) => void;
  pluginHrefPrefix?: string;
  requestedInteractionId?: string;
  pendingApprovals: PendingApproval[];
  approvalSubmissionState: ApprovalSubmissionState;
  onResolveApproval: (approvalId: string, optionId: string) => void;
  waitingForAnswer: boolean;
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
  const primaryAction = singleSubmission
    ? "send"
    : chatPrimaryAction({
        isComposerEmpty: composerIsEmpty,
        isRunning: effectiveIsRunning,
        isWorkRunning,
      });
  const sendDisabled =
    disabled || sendBlocked || (singleSubmission && effectiveIsRunning);
  const prepareComposerSend = () => {
    if (waitingForAnswer || sendDisabled || !aui.composer.getState().canSend)
      return false;
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
  const currentApproval = pendingApprovals[0];
  const queuedApproval = pendingApprovals.find(
    (item) =>
      item.approvalId === requestedInteractionId && item !== currentApproval,
  );
  const approvalPosition = approvalQueuePosition(
    currentApproval?.approvalId,
    approvalSeenState.seenIds,
  );
  const errorNotices = useChatErrorNotices({
    scopes: errorScopes,
    fallback: sendFailure,
    failure,
    appearance: "composer",
  });
  const composerContext =
    errorNotices ??
    (activityStatus ? (
      <div data-chat-work-status className="flex min-w-0 flex-1 items-center">
        {activityStatus}
      </div>
    ) : (
      renderContextStrip?.()
    ));

  return (
    <div className="w-full">
      <ComposerPrimitive.Root className="w-full">
        <ComposerSurface
          visualEffect={effects}
          bodyClassName="p-0"
          className="overflow-hidden"
          context={composerContext}
          contextTone={errorNotices ? "danger" : "neutral"}
          contextFooter={
            replyTo && onDismissReply ? (
              <ComposerReplyContext
                replyTo={replyTo}
                onDismiss={onDismissReply}
              />
            ) : null
          }
          expanded={!composerIsEmpty}
          contextLayers={
            queuedMessageCount > 0 ? (
              <ComposerLayerStack
                count={queuedMessageCount}
                label={t("queuedMessages", { count: queuedMessageCount })}
                expandLabel={t("expandQueuedMessages")}
                collapseLabel={t("collapseQueuedMessages")}
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
                            waitingForAnswer
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
          {currentApproval ? (
            <ChatApprovalCard
              approval={currentApproval}
              current={approvalPosition.current}
              total={approvalPosition.total}
              queuedApproval={queuedApproval}
              submissionState={approvalSubmissionState}
              onResolve={onResolveApproval}
            />
          ) : null}
          {/* 审批期间保留输入草稿和发送恢复状态。 */}
          <div hidden={Boolean(currentApproval)}>
            <ComposerInputBody data-chat-composer-input-body size={inputSize}>
              <ChatComposerInput
                contextReferences={contextReferences}
                onContextReferencesChange={onContextReferencesChange}
                autoFocus={autoFocus}
                disabled={
                  isWorkStopping || disabled || Boolean(currentApproval)
                }
                placeholder={t(
                  workflowPicker ? "placeholderWithWorkflows" : "placeholder",
                )}
                ariaLabel={t("messageInput")}
                skills={skills}
                skillsError={skillsError}
                plugins={plugins}
                pluginsError={pluginsError}
                workflowPicker={workflowPicker}
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
                onOpenPlugin={onOpenPlugin}
                pluginHrefPrefix={pluginHrefPrefix}
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
                      attachment?.file as BotPathFile | undefined
                    )?.botSourcePath;
                    if (!sourcePath || !removedPaths.has(sourcePath)) continue;
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
                draftReference={draftReference}
                registerReplyDraftEditor={registerReplyDraftEditor}
                focusRef={focusInputRef}
                clearRevision={clearRevision}
                restoreRevision={restoreRevision}
                className="placeholder:text-muted-foreground type-label resize-none bg-transparent outline-none disabled:opacity-55"
              />
            </ComposerInputBody>
            {/* 角落入口的命中区延伸至 36px，发送操作在其左侧留出空间。 */}
            <div className="min-h-control-lg flex items-center justify-between gap-2 px-2 pb-2 in-data-[chat-corner-launcher]:pr-3">
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
              <div className="flex min-w-0 items-center justify-end gap-1">
                {settings}
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
                    title={isWorkStopping ? t("team.stoppingWork") : t("stop")}
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
                      disabled={
                        waitingForAnswer || sendDisabled || !composerCanSend
                      }
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
                        waitingForAnswer ||
                        isWorkStopping ||
                        sendDisabled ||
                        !composerCanSend
                      }
                      onClickCapture={prepareComposerSend}
                    />
                  </ComposerPrimitive.Send>
                ) : null}
              </div>
            </div>
          </div>
        </ComposerSurface>
      </ComposerPrimitive.Root>
    </div>
  );
});

const emptyList: never[] = [];
const ignoreChange = () => undefined;

export function ChatThread(props: ChatThreadProps) {
  const inRouter = useInRouterContext();
  return inRouter ? (
    <RoutedChatThread {...props} />
  ) : (
    <ChatThreadContent {...props} />
  );
}

function RoutedChatThread(props: ChatThreadProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const pluginHrefPrefix = useHref(pluginDetailHref(""));
  const messageId = props.readingChatId
    ? (new URLSearchParams(location.search).get("message") ?? undefined)
    : undefined;
  return (
    <ChatThreadContent
      {...props}
      messageId={messageId}
      routeKey={location.key}
      onOpenPlugin={(pluginId) => void navigate(pluginDetailHref(pluginId))}
      pluginHrefPrefix={pluginHrefPrefix}
    />
  );
}

function ChatThreadContent({
  messageId,
  routeKey,
  onOpenPlugin,
  pluginHrefPrefix,
  fork,
  forkBoundaryMessageId,
  forkSourceChatId,
  onOpenForkSource,
  runtime,
  messages,
  active = true,
  presentationScopeKey,
  wakerName,
  wakerId,
  wakerAvatar,
  messageAuthors,
  teamReplyAuthorNames,
  timelineEvents = [],
  workflowInvocation,
  activityStatus,
  afterMessages,
  isRunning = false,
  isWorkRunning = false,
  isWorkStopping = false,
  onStopWork,
  emptyState,
  emptyStatePosition = "fill",
  animateComposerSize = false,
  pinEmptyState = false,
  hasExistingConversation = false,
  isHistoryLoading = false,
  historyError,
  renderComposerSettings,
  composerInputSize,
  composerActions,
  composerEffects,
  composerReferences,
  onComposerReferencesChange,
  renderComposerContextStrip,
  composerSkills = emptyList,
  composerSkillsError = false,
  composerPlugins = emptyList,
  composerPluginsError = false,
  composerWorkflowPicker,
  composerSendBlocked,
  selectedPluginIds = emptyList,
  onPluginSelectionChange = ignoreChange,
  composerConnectors = emptyList,
  composerConnectorsError = false,
  composerFileRoots = emptyList,
  composerFileScopePath,
  composerMembers = emptyList,
  availableSkillIds,
  availableConnectorIds,
  selectedSkillIds = emptyList,
  selectedConnectorIds = emptyList,
  onSkillSelectionChange = ignoreChange,
  onConnectorSelectionChange = ignoreChange,
  onMemberMentionChange,
  codeReferenceRequest,
  composerDraft,
  composerDraftReference,
  replyTo,
  onReplyToChange,
  registerReplyDraftEditor,
  showComposer = true,
  presentation = "thread",
  composerAutoFocus,
  composerDisabled,
  composerSingleSubmission,
  composerClearRevision,
  composerRestoreRevision,
  onOpenLink,
  linkWorkspaceRoot,
  fileScopeChatId,
  readingChatId,
  linkAdditionalWorkingDirectories,
  onCodeReferencesChange,
  onOpenCodeReference,
  onOpenFile,
  onOpenFileChange,
  contextMetrics,
  showUsageMetrics = true,
  showFileChanges = true,
  hasEarlierMessages,
  isLoadingEarlier,
  onLoadEarlier,
  sendFailure,
  errorScopes = emptyList,
  onRecoverSession,
  warning,
  warningAction,
  requestedInteractionId,
  pendingApprovals = emptyList,
  approvalSubmissionState = "idle",
  onResolveApproval = ignoreChange,
  pendingElicitations = emptyList,
  elicitationSubmissionState = "idle",
  onResolveElicitation = ignoreChange,
}: ChatThreadProps & {
  messageId?: string;
  routeKey?: string;
  onOpenPlugin?: (pluginId: string) => void;
  pluginHrefPrefix?: string;
}) {
  const { t } = useTranslation("chat");
  const candidateIcons = useMemo<ChatPluginIcons>(
    () =>
      Object.fromEntries(
        composerPlugins
          .filter(({ icon, icons }) => icon || icons?.default || icons?.dark)
          .map(({ id, icon, icons }) => [id, { icon, icons }]),
      ),
    [composerPlugins],
  );
  const missingPluginIcons = useMemo(
    () =>
      messages
        .filter((message) => !message.optimistic)
        .flatMap((message) =>
          chatPluginReferences(message.plugins).flatMap(({ id }) =>
            id && id.length <= 32 && !Object.hasOwn(candidateIcons, id)
              ? [id]
              : [],
          ),
        ),
    [candidateIcons, messages],
  );
  const historyIcons = usePluginReferenceIcons(missingPluginIcons, active);
  const pluginIcons = useMemo(
    () => ({ ...historyIcons, ...candidateIcons }),
    [candidateIcons, historyIcons],
  );
  const reducedMotion = useReducedMotion();
  const composerLayoutTransition = useMemo(
    () =>
      readLayoutMotionTokens(
        animateComposerSize ? "--duration-slow" : "--duration-layout",
      ),
    [animateComposerSize],
  );
  const viewportRef = useRef<HTMLDivElement>(null);
  const readingRootRef = useRef<HTMLDivElement>(null);
  const focusInputRef = useRef<(() => void) | null>(null);
  const inlineHistoryError =
    historyError && (messages.length === 0 || errorScopes.length === 0)
      ? historyError
      : undefined;
  useChatHistoryError(
    active && !inlineHistoryError ? historyError : undefined,
    errorScopes.at(-1),
  );
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
      if (positioning) return;
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
  const presentationScope =
    presentationScopeKey ??
    fileScopeChatId ??
    errorScopes[0] ??
    wakerId ??
    "global";
  const [replyLocation, setReplyLocation] = useState<{
    scope: string;
    routeKey?: string;
    messageId?: string;
    keyboardTrigger?: HTMLButtonElement;
    request: number;
  }>();
  const currentReplyLocation =
    active &&
    replyLocation?.scope === presentationScope &&
    replyLocation.routeKey === routeKey
      ? replyLocation
      : undefined;
  if (replyLocation && !currentReplyLocation) setReplyLocation(undefined);
  const locateReply = useCallback(
    (targetId: string, keyboardTrigger?: HTMLButtonElement) =>
      setReplyLocation((current) => ({
        scope: presentationScope,
        routeKey,
        messageId: targetId,
        keyboardTrigger,
        request: (current?.request ?? 0) + 1,
      })),
    [presentationScope, routeKey],
  );
  const cancelReplyLocation = () => {
    setReplyLocation((current) =>
      current ? { ...current, messageId: undefined } : current,
    );
  };
  const replyActions = useMemo(
    () => ({
      select:
        onReplyToChange && showComposer
          ? (reply: ChatReplyReference) => {
              onReplyToChange(reply);
              focusInputRef.current?.();
            }
          : undefined,
      locate: locateReply,
    }),
    [locateReply, onReplyToChange, showComposer],
  );
  const projection = useMemo(
    () => new ChatMessageProjection(undefined, presentationScope),
    [presentationScope],
  );
  const readingState = useMemo(
    () => ({ scope: presentationScope, groups: new Map<string, boolean>() }),
    [presentationScope],
  );
  const views = useMemo(
    () => projection.update(messages),
    [projection, messages],
  );
  const settledQuestionIds = useMemo(
    () =>
      new Set(
        messages.flatMap((message) =>
          message.question?.result ? [message.question.elicitationId] : [],
        ),
      ),
    [messages],
  );
  const openQuestions = pendingElicitations.filter(
    (question) =>
      question.elicitationId && !settledQuestionIds.has(question.elicitationId),
  );
  const liveQuestions = openQuestions.map((question, index) => (
    <ChatQuestionRow
      key={question.elicitationId}
      compact={presentation === "composer"}
    >
      <ChatElicitationCard
        elicitation={question}
        submissionState={index === 0 ? elicitationSubmissionState : "idle"}
        waitingForTurn={index > 0}
        onResolve={onResolveElicitation}
      />
      {index === 0 && fileScopeChatId && workflowInvocation ? (
        <CancelWorkflowInvocation
          key={workflowInvocation.executionId}
          chatId={fileScopeChatId}
          {...workflowInvocation}
        />
      ) : null}
    </ChatQuestionRow>
  ));
  const visibleTimelineEvents = timelineEventsInMessageWindow(
    timelineEvents,
    messages,
    hasEarlierMessages,
  );
  const emptyThread =
    messages.length === 0 &&
    liveQuestions.length === 0 &&
    visibleTimelineEvents.length === 0;
  const centerEmptyState =
    emptyStatePosition === "center" &&
    !isHistoryLoading &&
    !inlineHistoryError &&
    emptyThread;
  const showDirectEmptyState =
    !isHistoryLoading &&
    !inlineHistoryError &&
    liveQuestions.length === 0 &&
    (pinEmptyState || centerEmptyState);
  const threadVisible =
    active &&
    presentation === "thread" &&
    !showDirectEmptyState &&
    (!isHistoryLoading || liveQuestions.length > 0);
  const targetMessageId = currentReplyLocation
    ? currentReplyLocation.messageId
    : messageId;
  const locatingReply = Boolean(currentReplyLocation?.messageId);
  const targetKey = locatingReply
    ? JSON.stringify([
        presentationScope,
        routeKey,
        currentReplyLocation?.request,
      ])
    : routeKey;
  const { positioning, feedback: locationFeedback } = useChatMessageLocator({
    viewport: viewportRef,
    active: threadVisible && (!targetMessageId || !isHistoryLoading),
    routeKey: targetKey,
    messageId: targetMessageId,
    cancelReplyLocation: locatingReply ? cancelReplyLocation : undefined,
    keyboardReplyTrigger: currentReplyLocation?.keyboardTrigger,
    requestedInteractionId,
    hasEarlierMessages,
    onLoadEarlier: loadEarlier,
    onProgrammaticScroll: historyScroll.ignoreProgrammaticScroll,
  });
  useChatViewportScroll({
    viewport: viewportRef,
    scope: fileScopeChatId ?? presentationScope,
    revision: messages,
    isHistoryAnchoring: historyScroll.isAnchoring,
    onProgrammaticScroll: historyScroll.ignoreProgrammaticScroll,
    enabled: threadVisible && !positioning,
    followInitially: !messageId && !currentReplyLocation,
  });
  const latestMessage = messages.findLast((message) => !message.question);
  const compactComposer =
    presentation === "thread" &&
    (((hasExistingConversation || Boolean(fileScopeChatId)) &&
      (isHistoryLoading || Boolean(historyError))) ||
      hasEarlierMessages ||
      messages.some((message) => !message.optimistic));
  const companionInteraction = isWorkStopping
    ? "stopping"
    : pendingApprovals.length || openQuestions.length
      ? "waiting"
      : undefined;

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
      allowLocalReveal
      context={referenceContext}
    >
      {/* draft → chat 会切换 runtime；会话清理由 clearRevision 显式完成，不重建编辑器。 */}
      <ChatForkContext value={fork}>
        <ChatErrorActions
          value={{
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
                messageSource: "public",
              }),
          }}
        >
          <div
            ref={readingRootRef}
            className={cn(
              "flex h-full min-h-0 flex-col",
              centerEmptyState && "justify-center",
            )}
          >
            {locationFeedback}
            {presentation === "thread" ? (
              <div
                className={cn(
                  "relative flex min-h-0",
                  centerEmptyState ? "shrink-0" : "flex-1",
                )}
              >
                <div
                  ref={viewportRef}
                  data-chat-scroll-viewport
                  onScroll={threadVisible ? historyScroll.onScroll : undefined}
                  className="scrollbar-hidden relative flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain [overflow-anchor:none]"
                >
                  {showDirectEmptyState ? (
                    <div className="min-h-0 flex-1">{emptyState}</div>
                  ) : (
                    <>
                      {emptyThread && !isHistoryLoading ? emptyState : null}
                      <ChatMessageColumn>
                        {isHistoryLoading ? (
                          active && (
                            <ChatHistoryLoadingState
                              key={JSON.stringify([
                                presentationScope,
                                fileScopeChatId,
                                routeKey,
                              ])}
                            />
                          )
                        ) : (
                          <>
                            {active && inlineHistoryError ? (
                              <ChatErrorCard
                                id={`history:${errorScopes.join(":")}`}
                                message={inlineHistoryError.message}
                                onRetry={inlineHistoryError.onRetry}
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
                                !positioning &&
                                errorScopes.length === 0 ? (
                                  <ChatErrorCard
                                    id={`earlier:${errorScopes.join(":")}`}
                                    message={loadEarlierError}
                                    onRetry={async () => {
                                      await loadEarlier();
                                    }}
                                    onDismiss={historyScroll.clearError}
                                  />
                                ) : null}
                              </div>
                            )}
                            <ChatMessageReading
                              chatId={readingChatId}
                              active={active}
                              positioning={positioning}
                              viewport={readingRootRef}
                            >
                              <ContentVisibility value={active}>
                                <ProcessExpansion value={readingState.groups}>
                                  <CompanionInteraction
                                    value={companionInteraction}
                                  >
                                    <ChatMessageReplyActions
                                      value={replyActions}
                                    >
                                      <ChatMessageList
                                        views={views}
                                        requestedMessageId={
                                          active ? targetMessageId : undefined
                                        }
                                        requestedMessageKey={targetKey}
                                        timelineEvents={visibleTimelineEvents}
                                        forkBoundaryMessageId={
                                          forkBoundaryMessageId
                                        }
                                        forkSourceChatId={forkSourceChatId}
                                        onOpenForkSource={onOpenForkSource}
                                        name={wakerName}
                                        avatar={wakerAvatar}
                                        availableSkillIds={availableSkillIds}
                                        availableConnectorIds={
                                          availableConnectorIds
                                        }
                                        pluginIcons={pluginIcons}
                                        wakerId={wakerId}
                                        messageAuthors={messageAuthors}
                                        teamReplyAuthorNames={
                                          teamReplyAuthorNames
                                        }
                                        showUsageMetrics={showUsageMetrics}
                                        showFileChanges={showFileChanges}
                                        onOpenCodeReference={
                                          onOpenCodeReference
                                        }
                                        onOpenFile={onOpenFile}
                                        onOpenFileChange={onOpenFileChange}
                                      />
                                    </ChatMessageReplyActions>
                                  </CompanionInteraction>
                                </ProcessExpansion>
                              </ContentVisibility>
                            </ChatMessageReading>
                          </>
                        )}
                        {liveQuestions}
                        {!isHistoryLoading && afterMessages}
                      </ChatMessageColumn>
                    </>
                  )}
                </div>
                {!showDirectEmptyState && !isHistoryLoading && (
                  <ChatTurnNavigator
                    viewportRef={viewportRef}
                    messages={messages}
                    active={active}
                  />
                )}
              </div>
            ) : liveQuestions.length > 0 ? (
              <div
                className={cn(
                  "mx-auto w-full overflow-y-auto",
                  chatContentColumnClassName,
                )}
              >
                {liveQuestions}
              </div>
            ) : null}
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
              className="shrink-0 bg-transparent"
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
                    <span className="min-w-0 flex-1">{warning}</span>
                    {warningAction}
                  </div>
                ) : null}
                <AssistantRuntimeProvider runtime={runtime}>
                  <Composer
                    autoFocus={composerAutoFocus}
                    disabled={composerDisabled}
                    singleSubmission={composerSingleSubmission}
                    inputSize={
                      composerInputSize ??
                      (compactComposer ? "compact" : undefined)
                    }
                    activityStatus={activityStatus}
                    settings={renderComposerSettings?.(compactComposer)}
                    actions={composerActions}
                    effects={composerEffects}
                    contextReferences={composerReferences}
                    onContextReferencesChange={onComposerReferencesChange}
                    renderContextStrip={
                      compactComposer ? undefined : renderComposerContextStrip
                    }
                    skills={composerSkills}
                    skillsError={composerSkillsError}
                    plugins={composerPlugins}
                    pluginsError={composerPluginsError}
                    workflowPicker={composerWorkflowPicker}
                    sendBlocked={composerSendBlocked}
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
                    errorScopes={errorScopes}
                    failure={
                      latestMessage?.status === "failed"
                        ? latestMessage
                        : undefined
                    }
                    codeReferenceRequest={codeReferenceRequest}
                    onCodeReferencesChange={onCodeReferencesChange}
                    draft={composerDraft}
                    draftReference={composerDraftReference}
                    replyTo={replyTo}
                    onDismissReply={
                      onReplyToChange
                        ? () => {
                            onReplyToChange(undefined);
                            focusInputRef.current?.();
                          }
                        : undefined
                    }
                    registerReplyDraftEditor={registerReplyDraftEditor}
                    focusInputRef={focusInputRef}
                    clearRevision={composerClearRevision}
                    restoreRevision={composerRestoreRevision}
                    isRunning={isRunning}
                    onOpenFile={onOpenFile}
                    onOpenPlugin={onOpenPlugin}
                    pluginHrefPrefix={pluginHrefPrefix}
                    requestedInteractionId={requestedInteractionId}
                    pendingApprovals={pendingApprovals}
                    approvalSubmissionState={approvalSubmissionState}
                    onResolveApproval={onResolveApproval}
                    waitingForAnswer={openQuestions.length > 0}
                    isWorkRunning={isWorkRunning}
                    isWorkStopping={isWorkStopping}
                    onStopWork={onStopWork}
                  />
                </AssistantRuntimeProvider>
              </div>
            </motion.div>
          </div>
        </ChatErrorActions>
      </ChatForkContext>
    </FileReferenceProvider>
  );
}
