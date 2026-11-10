import { messageCompanionActivity } from "../runtime/companion-activity";
import { chatContinuations, type ChatContinuation } from "./chat-continuation";
import { ChatMessagePluginReferences } from "./chat-plugin-reference";
import { useChatHistoryScroll } from "./use-chat-history-scroll";
import type { ChatResultSource } from "@/shared/api/notification-presentation";
import { ChatWorkResultReader } from "@/shared/api/work-result-reading";
import { useChatMessageContribution } from "./chat-contributions";
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
  type ComponentProps,
} from "react";
import { useTranslation } from "react-i18next";
import { motion, useReducedMotion } from "motion/react";

import type { ResolvedFileReference } from "@/shared/workspace/file-reference";
import { cn } from "@/shared/lib/cn";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { Avatar } from "@/shared/ui/avatar";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { selectFiles } from "@/shared/platform";
import {
  pathFile,
  type AllyPathFile,
} from "../runtime/chat-attachment-adapter";
import {
  type AllyChatMessage,
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
  type ChatComposerDraftReference,
  type ChatComposerContextReference,
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
  chatMessageReferenceChipClass,
  type ChatFileOpenRequest,
  type ChatSkillOption,
} from "./chat-reference-menus";
import { AssistantMarkdown, UserMarkdown } from "./chat-message-content";
import { ChatTurnNavigator } from "./chat-turn-navigator";

import { Fragment } from "react";
import { ExecutionFileChangesCard } from "./execution-file-changes-card";
export { ExecutionFileChangesCard } from "./execution-file-changes-card";
import {
  ChatMessageProjection,
  viewCompanionActivity,
  isTerminalMessage,
  type ChatMessageView,
  type ChatPartNode,
  type ChatToolGroup,
  type ChatProcessGroup,
} from "./chat-message-nodes";
import { MarkdownVisibility } from "@/shared/markdown/visibility";
import { useChatViewportScroll } from "./use-chat-viewport-scroll";
type DisplayPart = NonNullable<AllyChatMessage["displayParts"]>[number];

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
  messages: readonly AllyChatMessage[];
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
  showFileChanges?: boolean;
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
  composerReferences?: ChatComposerContextReference[];
  onComposerReferencesChange?: (ids: string[]) => void;
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
  composerDraftReference?: ChatComposerDraftReference;
  showComposer?: boolean;
  /** 完整输入区可独立用于业务指令，不呈现消息列表或产生阅读事实。 */
  presentation?: "thread" | "composer";
  composerAutoFocus?: boolean;
  composerDisabled?: boolean;
  composerClearRevision?: number;
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

export function ReasoningPart({ text }: { text: string }) {
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
  completed,
  terminal,
  metrics,
  showUsageMetrics,
}: {
  group: ChatProcessGroup;
  currentKey?: string;
  completed: boolean;
  terminal: boolean;
  metrics?: ChatRunMetrics;
  showUsageMetrics: boolean;
}) {
  const { t } = useTranslation("chat");
  const durationMs = metrics?.durationMs;
  const reading = useContext(ProcessExpansion);
  const [expanded, setExpanded] = useState(
    () => reading?.get(group.key) ?? false,
  );
  const triggerRef = useRef<HTMLButtonElement>(null);
  const focusedInside = useRef(false);
  // 完成前后保留同一容器，让 open → closed 触发现有高度退出动画。
  const open = !completed || expanded;
  const contentId = useId();
  useLayoutEffect(() => {
    if (!open && focusedInside.current && !document.hidden) {
      triggerRef.current?.focus({ preventScroll: true });
      focusedInside.current = false;
    }
  }, [open]);
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
        onFocusCapture={() => {
          focusedInside.current = true;
        }}
        onBlurCapture={(event) => {
          if (
            event.relatedTarget instanceof Node &&
            !event.currentTarget.contains(event.relatedTarget)
          )
            focusedInside.current = false;
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
              <ChatPart key={node.key} node={node} terminal={terminal} />
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
}: {
  node: ChatPartNode;
  terminal: boolean;
}) {
  const { part } = node;
  return (
    <div data-chat-node-id={node.key}>
      {part.type === "text" ? (
        <AssistantMarkdown text={part.text ?? ""} isTerminal={terminal} />
      ) : part.type === "reasoning" ? (
        <ReasoningPart text={part.text ?? ""} />
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
  showUsageMetrics,
}: {
  view: ChatMessageView;
  metrics?: ChatRunMetrics;
  showUsageMetrics: boolean;
}) {
  const terminal = isTerminalMessage(view.message);
  // 总用时属于整次执行；分散在正文之间的过程段不能各自重复认领。
  const metricsGroup = view.segments.find((segment) => "kind" in segment);
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
            completed={view.message.status === "completed"}
            terminal={terminal}
            metrics={segment === metricsGroup ? metrics : undefined}
            showUsageMetrics={showUsageMetrics}
          />
        ) : (
          <ChatPart key={segment.key} node={segment} terminal={terminal} />
        ),
      )}
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

function MessageTimestamp({ timestamp }: { timestamp?: number }) {
  const createdAt = new Date(timestamp ?? 0);
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
  message: AllyChatMessage;
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

function UserMessage({
  message,
  availableSkillIds,
  availableConnectorIds,
  availablePluginIds,
  wakerId = "",
  onOpenCodeReference,
  onOpenFile,
}: {
  message: AllyChatMessage;
  availableSkillIds?: ReadonlySet<string>;
  availableConnectorIds?: ReadonlySet<string>;
  availablePluginIds?: ReadonlySet<string>;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
}) {
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
      className="group/user-message mt-4 px-6"
    >
      <div className="ml-auto grid max-w-[78%] min-w-0 justify-items-end gap-1">
        <div className="bg-user-message-background text-user-message-content type-chat max-w-full min-w-0 overflow-hidden rounded-xl px-4 py-1.5">
          <div className="flex flex-wrap items-start gap-x-1.5 gap-y-1">
            <ChatMessagePluginReferences
              plugins={message.plugins}
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
        <div className="duration-quick pointer-events-none opacity-0 transition-opacity group-focus-within/user-message:pointer-events-auto group-focus-within/user-message:opacity-100 group-hover/user-message:pointer-events-auto group-hover/user-message:opacity-100 motion-reduce:transition-none [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
          <MessageCopyAction message={message} />
        </div>
      </div>
    </div>
  );
}

function ParticipantMessage({
  message,
  latest,
  avatar,
  messageAuthors,
  name,
}: {
  message: AllyChatMessage;
  latest: boolean;
  avatar?: string;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  name: string;
}) {
  const { t } = useTranslation("chat");
  const messageID = message.id ?? "";
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const memberReport = author?.kind === "member-report";

  return (
    <div
      data-message-id={message.id}
      data-role="user"
      className="group mt-4 grid grid-cols-[32px_minmax(0,1fr)] items-start gap-3 px-6"
    >
      {latest ? (
        <Avatar name={displayName} src={displayAvatar} size="sm" />
      ) : (
        <div aria-hidden="true" />
      )}
      <div className="min-w-0">
        <div className="grid justify-items-start">
          <span className="type-body font-medium">{displayName}</span>
          <MessageTimestamp timestamp={message.createdAt} />
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
            <UserMarkdown text={message.content ?? ""} />
          </div>
        </div>
        <div className="mt-2 flex items-center gap-1">
          <MessageCopyAction message={message} />
        </div>
      </div>
    </div>
  );
}

function MessageCopyAction({
  message,
  hideWhenRunning = false,
}: {
  message: AllyChatMessage;
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
    >
      {selecting ? <LoaderCircle className="animate-spin" /> : <Plus />}
    </Button>
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
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  showUsageMetrics?: boolean;
  showFileChanges?: boolean;
}) {
  const { t } = useTranslation("chat");
  const message = view.message;
  const messageID = message.id ?? "";
  const contribution = useChatMessageContribution(messageID);
  const hasFileChanges =
    showFileChanges &&
    isTerminalMessage(message) &&
    message.fileChangeRecord?.state !== "pending" &&
    (message.fileChangeRecord?.totalFiles ?? 0) > 0;
  const hasContent =
    view.segments.length > 0 ||
    Boolean(contribution) ||
    hasFileChanges ||
    (message.status === "canceled" && !continuation?.continued) ||
    (message.status === "failed" && !last);
  const isLatestAssistant = latest;
  const interaction = useContext(CompanionInteraction);
  const companionActivity = continuation?.activityMessage
    ? messageCompanionActivity(
        { ...continuation.activityMessage, isLast: isLatestAssistant },
        interaction,
      )
    : viewCompanionActivity(view, isLatestAssistant, interaction);
  const author = messageAuthors?.[messageID];
  const displayName = author?.name ?? name;
  const displayAvatar = author?.avatar ?? avatar;
  const openFileChange = author?.sourceChatId
    ? (file: ChatFileChangeOpenRequest) =>
        onOpenFileChange?.({ ...file, sourceChatId: author.sourceChatId })
    : onOpenFileChange;

  return (
    <div
      data-message-id={message.id}
      data-role="assistant"
      className="group mt-4 grid grid-cols-[32px_minmax(0,1fr)] items-start gap-3 px-6"
    >
      {isLatestAssistant ? (
        <Avatar
          name={displayName}
          src={displayAvatar}
          size="sm"
          activity={companionActivity}
        />
      ) : (
        <div aria-hidden="true" />
      )}
      <div className="group/reply min-w-0">
        {hasContent && (
          <div
            data-chat-message-card
            className="bg-surface-panel rounded-xl p-4 dark:bg-transparent dark:p-0"
          >
            <div className="min-w-0">
              <ChatMessageParts
                view={view}
                metrics={continuation?.footerMessage.runMetrics}
                showUsageMetrics={showUsageMetrics}
              />
              {showFileChanges && message.fileChangeRecord ? (
                <ExecutionFileChangesCard
                  message={message}
                  chatId={author?.sourceChatId}
                  visible={isTerminalMessage(message)}
                  onOpenFile={openFileChange}
                />
              ) : null}
              {contribution}
            </div>
            {message.status === "canceled" && !continuation?.continued ? (
              <p className="text-muted-foreground type-body mt-2">
                {t("canceled")}
              </p>
            ) : null}
            <MessageFailure message={message} last={last} />
          </div>
        )}

        {(hasContent || isTerminalMessage(message)) &&
        !continuation?.continued ? (
          <footer className="mt-1 flex items-center gap-1">
            <div className="duration-quick pointer-events-none flex items-center gap-1 opacity-0 transition-opacity group-focus-within/reply:pointer-events-auto group-focus-within/reply:opacity-100 group-hover/reply:pointer-events-auto group-hover/reply:opacity-100 motion-reduce:transition-none [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
              {isTerminalMessage(message) && (
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
              )}
              <MessageCopyAction
                message={continuation?.footerMessage ?? message}
                hideWhenRunning
              />
              <MessageForkAction message={message} />
            </div>
            <span className="duration-quick ml-auto shrink-0 opacity-0 transition-opacity group-focus-within/reply:opacity-100 group-hover/reply:opacity-100 motion-reduce:transition-none [@media(hover:none)]:opacity-100">
              <MessageTimestamp
                timestamp={(continuation?.footerMessage ?? message).createdAt}
              />
            </span>
          </footer>
        ) : null}
      </div>
    </div>
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
  availablePluginIds,
  wakerId,
  onOpenCodeReference,
  onOpenFile,
  onOpenFileChange,
  messageAuthors,
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
  availablePluginIds?: ReadonlySet<string>;
  wakerId?: string;
  onOpenCodeReference?: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  messageAuthors?: Readonly<Record<string, ChatMessageAuthor>>;
  showUsageMetrics?: boolean;
  showFileChanges?: boolean;
}) {
  const message = view.message;
  if (message.role === "user") {
    return messageAuthors?.[message.id ?? ""]?.displayAsParticipant ? (
      <ParticipantMessage
        message={message}
        latest={latest}
        name={name}
        avatar={avatar}
        messageAuthors={messageAuthors}
      />
    ) : (
      <UserMessage
        message={message}
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
      view={view}
      continuation={continuation}
      latest={latest}
      last={last}
      name={name}
      avatar={avatar}
      messageAuthors={messageAuthors}
      onOpenFileChange={onOpenFileChange}
      showUsageMetrics={showUsageMetrics}
      showFileChanges={showFileChanges}
    />
  ) : null;
});

const ChatMessageList = memo(function ChatMessageList({
  views,
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
  forkBoundaryMessageId?: string;
  forkSourceChatId?: string;
  onOpenForkSource?: (chatId: string) => Promise<boolean>;
}) {
  const { t } = useTranslation("chat");
  const [unavailableSourceId, setUnavailableSourceId] = useState<string>();
  const continuations = chatContinuations(views.map((view) => view.message));
  const latest = views.at(-1);
  return (
    <>
      {views.map((view, index) => (
        <Fragment key={view.key}>
          <ChatMessage
            {...props}
            wakerId={wakerId}
            view={view}
            continuation={continuations.get(view.message.id ?? "")}
            latest={view.key === latest?.key}
            last={index === views.length - 1}
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
      ))}
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
  contextReferences,
  onContextReferencesChange,
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
  errorScopes,
  failure,
  codeReferenceRequest,
  onCodeReferencesChange,
  draft,
  draftReference,
  clearRevision,
  restoreRevision,
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
  contextReferences?: ChatComposerContextReference[];
  onContextReferencesChange?: (ids: string[]) => void;
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
  errorScopes: readonly string[];
  failure?: AllyChatMessage;
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  draft?: string;
  draftReference?: ChatComposerDraftReference;
  clearRevision?: number;
  restoreRevision?: number;
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
  const errorNotices = useChatErrorNotices({
    scopes: errorScopes,
    fallback: sendFailure,
    failure,
    appearance: "composer",
  });
  const composerContext = errorNotices ?? renderContextStrip?.();

  return (
    <div className="w-full">
      <ComposerPrimitive.Root className="w-full">
        <ComposerSurface
          visualEffect={effects}
          bodyClassName="p-0"
          className="overflow-hidden"
          context={composerContext}
          contextTone={errorNotices ? "danger" : "neutral"}
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
          ) : null}
          {/* 待处理交互只隐藏输入区，保留当前文档、建议消费状态与发送恢复状态。 */}
          <div hidden={Boolean(currentElicitation || currentApproval)}>
            <ComposerInputBody data-chat-composer-input-body>
              <ChatComposerInput
                contextReferences={contextReferences}
                onContextReferencesChange={onContextReferencesChange}
                autoFocus={autoFocus}
                disabled={
                  isWorkStopping ||
                  disabled ||
                  Boolean(currentElicitation || currentApproval)
                }
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
                      disabled={isWorkStopping || disabled || !composerCanSend}
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

export function ChatThread({
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
  composerReferences,
  onComposerReferencesChange,
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
  composerDraftReference,
  showComposer = true,
  presentation = "thread",
  composerAutoFocus,
  composerDisabled,
  composerClearRevision,
  composerRestoreRevision,
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
  showFileChanges = true,
  hasEarlierMessages,
  isLoadingEarlier,
  onLoadEarlier,
  sendFailure,
  errorScopes = emptyList,
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
  const presentationScope =
    presentationScopeKey ??
    fileScopeChatId ??
    errorScopes[0] ??
    wakerId ??
    "global";
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
  const centerEmptyState =
    emptyStatePosition === "center" &&
    !isHistoryLoading &&
    messages.length === 0;
  const showDirectEmptyState =
    isHistoryLoading || pinEmptyState || centerEmptyState;
  useChatViewportScroll({
    viewport: viewportRef,
    scope: presentationScope,
    revision: messages,
    isHistoryAnchoring: historyScroll.isAnchoring,
    enabled: active && presentation === "thread" && !showDirectEmptyState,
  });
  const latestMessage = messages.at(-1);
  const companionInteraction = isWorkStopping
    ? "stopping"
    : pendingApprovals.length || pendingElicitations.length
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
            className={cn(
              "flex h-full min-h-0 flex-col",
              centerEmptyState && "justify-center",
            )}
          >
            {presentation === "thread" &&
            resultSource &&
            !isHistoryLoading &&
            messages.length > 0 &&
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
                  </div>
                ) : (
                  <>
                    <div
                      ref={viewportRef}
                      data-chat-scroll-viewport
                      onScroll={historyScroll.onScroll}
                      className="scrollbar-hidden relative flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain [overflow-anchor:none]"
                    >
                      {messages.length === 0 ? emptyState : null}
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
                            {loadEarlierError && errorScopes.length === 0 ? (
                              <ChatErrorCard
                                id={`earlier:${errorScopes.join(":")}`}
                                message={loadEarlierError}
                                onRetry={loadEarlier}
                                onDismiss={historyScroll.clearError}
                              />
                            ) : null}
                          </div>
                        )}
                        <MarkdownVisibility value={active}>
                          <ProcessExpansion value={readingState.groups}>
                            <CompanionInteraction value={companionInteraction}>
                              <ChatMessageList
                                views={views}
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
                                showFileChanges={showFileChanges}
                                onOpenCodeReference={onOpenCodeReference}
                                onOpenFile={onOpenFile}
                                onOpenFileChange={onOpenFileChange}
                              />
                            </CompanionInteraction>
                          </ProcessExpansion>
                        </MarkdownVisibility>
                      </ChatMessageColumn>
                    </div>
                    <ChatTurnNavigator
                      viewportRef={viewportRef}
                      messages={messages}
                      active={active}
                    />
                  </>
                )}
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
                    <span className="min-w-0">{warning}</span>
                  </div>
                ) : null}
                <AssistantRuntimeProvider runtime={runtime}>
                  <Composer
                    autoFocus={composerAutoFocus}
                    disabled={composerDisabled}
                    activityStatus={activityStatus}
                    settings={composerSettings}
                    actions={composerActions}
                    effects={composerEffects}
                    contextReferences={composerReferences}
                    onContextReferencesChange={onComposerReferencesChange}
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
                    clearRevision={composerClearRevision}
                    restoreRevision={composerRestoreRevision}
                    isRunning={isRunning}
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
                </AssistantRuntimeProvider>
              </div>
            </motion.div>
          </div>
        </ChatErrorActions>
      </ChatForkContext>
    </FileReferenceProvider>
  );
}
