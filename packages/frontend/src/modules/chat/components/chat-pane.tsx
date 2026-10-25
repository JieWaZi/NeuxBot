import type { FloatingChatSnapshot } from "./floating-chat";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as AcceptedExecution } from "@/shared/api/generated/types.gen";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { openExternalURL } from "@/shared/platform";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { ExecutionPermissionControl } from "@/shared/ui/execution-selection-controls";
import {
  ChatComposerWorkspaceStrip,
  composerContextControlClassName,
} from "./chat-composer-workspace-strip";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  cancelChatExecutionMutation,
  chatMessagesInfiniteOptions,
  useChatHistoryVisibility,
  getChatOptions,
  resolveApprovalMutation,
  resolveElicitationMutation,
} from "../api/chat-queries";
import { conversationKeyOf } from "../model/conversation-identity";
import { useChatRuntime } from "../runtime/use-chat-runtime";
import type { ChatSendSelection } from "../runtime/chat-queue-store";
import { ChatThread } from "./chat-thread";
import { ChatHistoryLoadingState } from "./chat-history-loading-state";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";

/** 上层工作入口只接管发送接纳；消息、过程、队列、原生审批和输入仍走普通 Chat。 */
export function ChatPane({
  scopeKey,
  chatId,
  wakerId: initialWakerId,
  title,
  defaults,
  draft,
  emptyState,
  centered = false,
  showComposer = true,
  allowExecutionRetry = true,
  isHistoryLoading = false,
  onSend,
  onAccepted,
  onRefresh,
  onConfigurationChange,
  onRunningChange,
  onSnapshotChange,
}: {
  scopeKey: string;
  chatId?: string;
  /** 新会话在接纳前绑定伙伴，接纳后以 Chat 的真实身份为准。 */
  wakerId?: string;
  title: string;
  defaults?: ExecutionSelection;
  draft?: string;
  emptyState?: ReactNode;
  centered?: boolean;
  showComposer?: boolean;
  /** 受限工作入口可关闭原生重试，改由其专用发送入口重新接纳。 */
  allowExecutionRetry?: boolean;
  /** 上层入口尚在解析 Chat 身份时，只延长消息区的加载状态。 */
  isHistoryLoading?: boolean;
  onSend: (
    text: string,
    attachments: string[],
    selection: ChatSendSelection,
  ) => Promise<AcceptedExecution>;
  onAccepted?: (result: AcceptedExecution) => void;
  onRefresh?: () => void;
  onConfigurationChange?: (selection: ChatSendSelection | undefined) => void;
  onRunningChange?: (running: boolean) => void;
  onSnapshotChange?: (snapshot: FloatingChatSnapshot) => void;
}) {
  const { t } = useTranslation("chat");
  const active = useRouteSceneActive();
  const [selection, setSelection] = useState<ExecutionSelection>({});
  const [clearRevision, setClearRevision] = useState(0);
  const [restoreRevision, setRestoreRevision] = useState(0);
  const detail = useQuery({
    ...getChatOptions({ path: { chatId: chatId || "pending" } }),
    enabled: active && Boolean(chatId),
  });
  useQueryEvents({
    topic: "chat",
    resource: { kind: "chat", id: chatId || "" },
    queryKeys: [
      getChatOptions({ path: { chatId: chatId || "pending" } }).queryKey,
    ],
    active,
    enabled: Boolean(chatId),
  });
  useChatHistoryVisibility(chatId, active);
  const history = useInfiniteQuery({
    ...chatMessagesInfiniteOptions(chatId || "pending"),
    enabled: active && Boolean(chatId),
  });
  const wakerId = detail.data?.chat?.wakerId || initialWakerId;
  const partner = useQuery({
    ...getWakerOptions({ path: { wakerId: wakerId || "pending" } }),
    enabled: active && Boolean(wakerId),
  });
  const messages = useMemo(() => {
    const seen = new Set<string>();
    return [...(history.data?.pages ?? [])].reverse().flatMap((page) =>
      (page.resources ?? []).filter((message) => {
        if (!message.id || seen.has(message.id)) return false;
        seen.add(message.id);
        return true;
      }),
    );
  }, [history.data]);
  const chat = detail.data?.chat;
  // 已有会话沿用真实执行选择，后台任务的配置不能被全局默认值覆盖。
  const savedSelection = chat?.lastRuntimeId
    ? {
        runtimeId: chat.lastRuntimeId,
        accountId: chat.lastAccountId || "system",
        model: chat.lastModel,
        reasoning: chat.lastReasoning,
        permissionLevel: chat.lastPermissionLevel,
      }
    : defaults;
  const execution = useExecutionSelection({
    value: { ...savedSelection, ...selection },
    defaults: !chat?.lastRuntimeId,
    enabled: active,
  });
  const config = useMemo<ChatSendSelection>(
    () => ({
      runtimeId: execution.selection.runtimeId || "",
      accountId: execution.selection.accountId,
      model: execution.selection.model || "",
      reasoning: execution.selection.reasoning || "",
      permissionLevel:
        permissionLevelOf(execution.selection.permissionLevel) ?? "default",
    }),
    [
      execution.selection.runtimeId,
      execution.selection.accountId,
      execution.selection.model,
      execution.selection.reasoning,
      execution.selection.permissionLevel,
    ],
  );
  useEffect(() => {
    onConfigurationChange?.(
      active && !execution.loading && config.runtimeId ? config : undefined,
    );
  }, [active, config, execution.loading, onConfigurationChange]);
  const refresh = () => {
    if (chatId) {
      void detail.refetch();
      void history.refetch();
    }
    onRefresh?.();
  };
  const cancel = useMutation(
    withLocalErrorMutation(cancelChatExecutionMutation()),
  );
  const approval = useMutation(
    withLocalErrorMutation(resolveApprovalMutation()),
  );
  const elicitation = useMutation(
    withLocalErrorMutation(resolveElicitationMutation()),
  );
  const state = useChatRuntime({
    conversationKey: conversationKeyOf(scopeKey, chatId),
    queueKey: chatId || scopeKey,
    queueActive: active,
    chatId,
    messages,
    latestSequence: history.data?.pages[0]?.latestSequence ?? 0,
    activeExecutionId: detail.data?.chat?.activeExecutionId,
    initialPendingApprovals: active ? detail.data?.chat?.pendingApprovals : [],
    initialPendingElicitations: active
      ? detail.data?.chat?.pendingElicitations
      : [],
    isHistoryReady: !chatId || history.isSuccess,
    isLoading: Boolean(chatId && (detail.isLoading || history.isLoading)),
    isSendDisabled:
      !showComposer ||
      !active ||
      !config.runtimeId ||
      execution.loading ||
      !execution.valid ||
      Boolean(chatId && !detail.isSuccess) ||
      isHistoryLoading,
    attachmentOnlyText: t("attachmentOnlyMessage"),
    incompleteErrorText: t("executionIncomplete"),
    selection: config,
    send: (text, _id, paths = [], selected = config) =>
      onSend(text, paths, selected),
    cancel: async () => {
      if (chatId) await cancel.mutateAsync({ path: { chatId } });
    },
    onMessageAccepted: () => setClearRevision((value) => value + 1),
    onMessageRejected: () => setRestoreRevision((value) => value + 1),
    onStarted: (result) => {
      onAccepted?.(result);
      refresh();
    },
    onSettled: refresh,
    onTerminal: refresh,
  });
  useEffect(() => {
    onRunningChange?.(state.isRunning);
  }, [onRunningChange, state.isRunning]);
  useEffect(() => {
    onSnapshotChange?.({
      isRunning: state.isRunning,
      pendingApprovalCount: state.pendingApprovals.length,
      pendingElicitationCount: state.pendingElicitations.length,
      streamState: state.streamState,
    });
  }, [
    onSnapshotChange,
    state.isRunning,
    state.pendingApprovals.length,
    state.pendingElicitations.length,
    state.streamState,
  ]);
  const historyLoading = Boolean(
    isHistoryLoading ||
    (chatId && chatId !== state.boundChatId) ||
    (chatId && !state.isHistorySynchronized && !state.hasVisibleMessages),
  );
  return (
    <ChatThread
      runtime={state.runtime}
      wakerId={wakerId}
      wakerName={partner.data?.name || title}
      wakerAvatar={partner.data?.avatar}
      isRunning={state.isRunning}
      emptyState={historyLoading ? <ChatHistoryLoadingState /> : emptyState}
      emptyStatePosition={centered ? "center" : "fill"}
      isHistoryLoading={historyLoading}
      composerSettings={
        <>
          <ExecutionConfigurationControls
            state={execution}
            onChange={setSelection}
            showPermissions={false}
          />
          {!execution.loading && !execution.error && !execution.valid && (
            <span role="alert" className="text-destructive type-caption w-full">
              {t("common:executionSelection.invalid")}
            </span>
          )}
        </>
      }
      renderComposerContextStrip={() => (
        <ChatComposerWorkspaceStrip
          permissionControl={
            <ExecutionPermissionControl
              selectedPermissionLevel={execution.selection.permissionLevel}
              triggerClassName={composerContextControlClassName}
              supportedPermissions={execution.supportedPermissions}
              disabled={execution.loading}
              onChange={(permissionLevel) =>
                setSelection({ ...execution.selection, permissionLevel })
              }
            />
          }
        />
      )}
      composerDraft={draft}
      composerDisabled={
        execution.loading ||
        !execution.valid ||
        Boolean(chatId && !detail.isSuccess)
      }
      showComposer={
        showComposer ||
        state.pendingApprovals.length > 0 ||
        state.pendingElicitations.length > 0
      }
      composerClearRevision={clearRevision}
      composerRestoreRevision={restoreRevision}
      fileScopeChatId={chatId}
      resultSource={
        chatId ? { chatId, owner: { kind: "chat", id: chatId } } : undefined
      }
      linkWorkspaceRoot={detail.data?.chat?.workspaceDirectory}
      onOpenLink={(url) => {
        void openExternalURL(url);
        return true;
      }}
      sendFailure={state.sendFailure}
      errorScopes={[
        scopeKey,
        chatId || scopeKey,
        `chat:${chatId ?? "pending"}`,
      ]}
      onRetryExecution={allowExecutionRetry ? state.retryExecution : undefined}
      contextMetrics={state.contextMetrics}
      hasEarlierMessages={active && Boolean(history.hasNextPage)}
      isLoadingEarlier={history.isFetchingNextPage}
      onLoadEarlier={() =>
        history
          .fetchNextPage({ cancelRefetch: false, throwOnError: true })
          .then(() => {})
      }
      pendingApprovals={active ? state.pendingApprovals : []}
      approvalSubmissionState={approval.isPending ? "submitting" : "idle"}
      onResolveApproval={(approvalId, optionId) => {
        if (active && chatId)
          approval.mutate(
            { path: { chatId, approvalId }, body: { optionId } },
            { onSuccess: refresh },
          );
      }}
      pendingElicitations={active ? state.pendingElicitations : []}
      elicitationSubmissionState={elicitation.isPending ? "submitting" : "idle"}
      onResolveElicitation={(elicitationId, content, declined) => {
        if (active && chatId)
          elicitation.mutate(
            { path: { chatId, elicitationId }, body: { content, declined } },
            { onSuccess: refresh },
          );
      }}
    />
  );
}
