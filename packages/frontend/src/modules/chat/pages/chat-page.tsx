import { getWakerRecentProjectOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { useInstalledPluginReferences } from "@/modules/plugin";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { useChatFork } from "../components/chat-fork";
import { useChatAccountSelection } from "../model/use-chat-account-selection";
import { useDraftWorkspaceSelections } from "./use-draft-workspace-selections";
import { SuggestionPrimitive, ThreadPrimitive } from "@assistant-ui/react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type {
  GithubComJieWaZiNeuxBotServerApisChatsMessageRequest as ChatMessageRequest,
  GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as AcceptedExecution,
} from "@/shared/api/generated/types.gen";
import type { ChatWorkspaceMainContent } from "../components/chat-workspace-surface";
import { useWorkbench } from "../workbench/workbench-store";
import type { ChatSendSelection } from "../runtime/chat-queue-store";
import { useTranslation } from "react-i18next";
import {
  useNavigate,
  useOutletContext,
  useParams,
  useSearchParams,
} from "react-router-dom";

import {
  cancelChatExecutionMutation,
  createChatMutation,
  chatListKey,
  chatMessagesInfiniteOptions,
  chatMessageSource,
  useChatHistoryVisibility,
  commitAcceptedChat,
  getChatOptions,
  getChatQueryKey,
  listRuntimesOptions,
  listWakerSkillsOptions,
  resolveApprovalMutation,
  resolveElicitationMutation,
  sendMessageMutation,
} from "../api/chat-queries";
import { listAiAccountsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { ChatHistoryLoadingState } from "../components/chat-history-loading-state";
import {
  ChatWorkspaceSurface,
  type ChatWorkspaceAdapter,
} from "../components/chat-workspace-surface";
import { ChatThread } from "../components/chat-thread";
import { useChatWorkspaceHost } from "../components/chat-contributions";
import { useChatComposerControls } from "../components/chat-composer-controls";
import {
  ChatSessionTransitionDialog,
  type SessionTransitionChoice,
} from "../components/chat-session-transition-dialog";
import { workspaceFilesOf } from "../model/chat-workspace-files";
import { resolveGitWorkspaceDirectories } from "../model/git-workspace-directories";
import { useChatWorkspaceBindings } from "../components/use-chat-workspace-bindings";
import { useComposerScopeTransition } from "../model/use-composer-scope-transition";
import { useChatViewState } from "../model/use-chat-view-state";
import { projectFilesScopePath } from "../model/project-files-scope";
import {
  conversationKeyOf,
  useChatInvocationGuard,
} from "../model/conversation-identity";
import { useChatInteractions } from "../model/use-conversation-interactions";
import { selectableRuntimes } from "../model/selectable-runtimes";
import {
  useChatRuntime,
  type PreparedChatSubmission,
} from "../runtime/use-chat-runtime";
import {
  useRouteSceneActive,
  useScenePresentationKey,
} from "@/shared/workspace/app-scene-workspace";
import { chatHref } from "@/shared/lib/chat-navigation";
import { listProjectsOptions } from "@/modules/project";
import {
  connectorSelectableInComposer,
  listWakerConnectorsOptions,
} from "@/modules/connector";
import { listWakersOptions } from "@/modules/waker";
import { selectDirectory } from "@/shared/platform";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval } from "@/shared/api/generated/types.gen";
import {
  clearStableCreateRequest,
  withStableCreateRequest,
} from "@/shared/lib/create-request-idempotency";
import { type PermissionLevel } from "@/shared/lib/runtime-permissions";
import { Avatar } from "@/shared/ui/avatar";
import { TypedPhrases } from "@/shared/ui/typed-phrases";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";
import type { WorkspaceMemberTab } from "@/shared/workspace/workspace-tab";
import { type ChatOutletContext, chatSuggestionKeys } from "./chat-page-shared";

export function ChatPage({
  presentationScopeKey,
}: { presentationScopeKey?: string } = {}) {
  const { chatId } = useParams();
  const scopeKey = useScenePresentationKey(presentationScopeKey);
  return <ChatPageContent chatId={chatId} presentationScopeKey={scopeKey} />;
}

/** 业务入口贡献身份、发送接纳和内容；普通聊天的其余能力共用同一实现。 */
export type ChatWorkspaceEntry = {
  /** 首条输入在完整画布内居中，发送后在主容器内展开会话与业务内容。 */
  canvasInput?: boolean;
  showSummary?: boolean;
  active?: boolean;
  defaults?: ExecutionSelection;
  scopeKey: string;
  wakerId: string;
  directoryLabel: string;
  mainContent?: ChatWorkspaceMainContent;
  draft?: string;
  emptyState?:
    | ReactNode
    | ((context: {
        wakerName: string;
        avatar?: string;
        isRunning: boolean;
      }) => ReactNode);
  isHistoryLoading?: boolean;
  isSendDisabled?: boolean;
  prepareSubmission: (message: ChatMessageRequest) => PreparedChatSubmission;
  onAccepted: (result: AcceptedExecution) => void;
  onRefresh?: () => void;
  onConfigurationChange?: (selection: ChatSendSelection | undefined) => void;
  onRunningChange?: (running: boolean) => void;
  onNewChat?: () => void;
  history?: ReactNode;
};

type ChatPageContentProps = {
  chatId?: string;
  embedded?: boolean;
  routeTransitioning?: boolean;
  entry?: ChatWorkspaceEntry;
  presentation?: "workspace" | "composer";
  presentationScopeKey: string;
};

/** 呈现作用域拥有消息视图；接纳只绑定服务器 Chat，不重建同一草稿的视图。 */
export function ChatPageContent(props: ChatPageContentProps) {
  const [searchParams] = useSearchParams();
  const composerHost = useChatWorkspaceHost();
  const workspaceScopeKey =
    (props.presentation === "composer" ? composerHost?.scopeKey : undefined) ??
    props.entry?.scopeKey ??
    (props.chatId
      ? `chat:${props.chatId}`
      : `draft:${searchParams.get("draftId") ?? "default"}`);
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const member = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId && tab.kind === "member",
  );
  return (
    <ChatView
      key={JSON.stringify([props.presentationScopeKey, member?.id ?? null])}
      {...props}
      workspaceScopeKey={workspaceScopeKey}
    />
  );
}

function ChatView({
  chatId,
  embedded = false,
  routeTransitioning = false,
  entry,
  presentation = "workspace",
  workspaceScopeKey,
}: ChatPageContentProps & { workspaceScopeKey: string }) {
  const { t } = useTranslation("chat");
  const routeActive = useRouteSceneActive();
  const sceneActive = routeActive && (entry?.active ?? true);
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const outlet = useOutletContext<ChatOutletContext | null>();
  const [localPanel, setLocalPanel] =
    useState<ChatOutletContext["chatRightPanel"]>();
  const chatRightPanel = entry
    ? localPanel
    : (outlet?.chatRightPanel ?? localPanel);
  const setChatRightPanel = entry
    ? setLocalPanel
    : (outlet?.setChatRightPanel ?? setLocalPanel);
  const [draftWorkspaceSelections, selectDraftWorkspace] =
    useDraftWorkspaceSelections();
  const [selectedSkillIDsByWaker, setSelectedSkillIDsByWaker] = useState<
    Record<string, string[]>
  >({});
  const [selectedPluginIDsByWaker, setSelectedPluginIDsByWaker] = useState<
    Record<string, string[]>
  >({});
  const [selectedConnectorIDsByWaker, setSelectedConnectorIDsByWaker] =
    useState<Record<string, string[]>>({});
  const [composerRestoreRevision, setComposerRestoreRevision] = useState(0);
  const [sessionSwitch, setSessionSwitch] = useState<{
    chatId: string;
    patch: Partial<{
      accountId: string;
      model: string;
      permissionLevel: PermissionLevel;
      reasoning: string;
      runtimeId: string;
    }>;
    runtimeId: string;
    accountId: string;
    reason: "switch" | "recovery";
  } | null>(null);
  const [freshSession, setFreshSession] = useState<{
    chatId: string;
    runtimeId: string;
    accountId: string;
    historyTurns: number;
    historyMode: SessionTransitionChoice["mode"];
    summaryModel: string;
    summaryReasoning: string;
  } | null>(null);
  const offeredRecoveryRef = useRef("");
  const sceneActiveRef = useRef(sceneActive);
  useLayoutEffect(() => {
    sceneActiveRef.current = sceneActive;
  }, [sceneActive]);
  const pendingAcceptedRef = useRef<{
    key: string;
    result: AcceptedExecution;
  } | null>(null);
  const composerHost = useChatWorkspaceHost();
  const { state: chatViewState, update: updateChatViewState } =
    useChatViewState(workspaceScopeKey);
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const workbench = useWorkbench(workspaceScopeKey);
  const activeMemberTab = workspace.snapshot.tabs.find(
    (tab): tab is WorkspaceMemberTab =>
      tab.id === workspace.snapshot.activeTabId && tab.kind === "member",
  );
  const activeMemberWakerId = activeMemberTab?.wakerId;
  const activeChatId = activeMemberTab?.chatId ?? chatId;
  const currentChatKey = conversationKeyOf(workspaceScopeKey, activeChatId);
  const { accountId, selectAccount } = useChatAccountSelection(
    presentation === "composer"
      ? `composer:${entry?.scopeKey ?? workspaceScopeKey}`
      : activeChatId
        ? `chat:${activeChatId}`
        : workspaceScopeKey,
  );
  const resetComposerScope = useCallback(() => {
    setSelectedSkillIDsByWaker({});
    setSelectedPluginIDsByWaker({});
    setSelectedConnectorIDsByWaker({});
  }, []);
  const {
    composerClearRevision,
    companionScopeRevision,
    clearComposer,
    markSkipComposerClearOnCreate,
  } = useComposerScopeTransition(
    workspaceScopeKey,
    currentChatKey,
    resetComposerScope,
  );
  // 独立指令只提交当前输入，不消费原聊天已排队的消息。
  const runtimeQueueKey =
    presentation === "composer"
      ? `composer:${entry?.scopeKey ?? workspaceScopeKey}`
      : (activeChatId ?? workspaceScopeKey);
  const chatInvocationGuard = useChatInvocationGuard(currentChatKey);
  const {
    codeReferenceRequest,
    fileSource,
    model,
    openedCodeReference,
    permissionLevel,
    query: workspaceQuery,
    reasoning,
    runtimeId,
    selectedCodeReferences,
  } = chatViewState;

  const wakersQuery = useQuery(
    listWakersOptions({ query: { pageNum: 1, pageSize: 100 } }),
  );
  const detailQuery = useQuery({
    ...getChatOptions({
      path: { chatId: activeChatId ?? "pending" },
    }),
    enabled: sceneActive && Boolean(activeChatId),
  });
  useChatHistoryVisibility(activeChatId, sceneActive);
  const messagesQuery = useInfiniteQuery({
    ...chatMessagesInfiniteOptions(activeChatId ?? "pending"),
    enabled: sceneActive && Boolean(activeChatId),
  });
  const runtimesQuery = useQuery(listRuntimesOptions());
  const accountsQuery = useQuery(listAiAccountsOptions());
  const wakers = wakersQuery.data?.resources ?? [];
  const requestedWaker = searchParams.get("wakerId")?.trim() ?? "";
  const targetWakerID =
    activeMemberWakerId ||
    detailQuery.data?.chat?.wakerId ||
    entry?.wakerId ||
    requestedWaker ||
    wakers[0]?.id ||
    "";
  const selectedWaker =
    wakers.find((waker) => waker.id === targetWakerID) ??
    (targetWakerID ? undefined : wakers[0]);
  const selectedWakerID = targetWakerID || selectedWaker?.id;
  const projectsQuery = useQuery({
    ...listProjectsOptions({
      path: { wakerId: selectedWakerID ?? "pending" },
      query: { pageNum: 1, pageSize: 100 },
    }),
    enabled: Boolean(selectedWakerID),
  });
  const pluginsQuery = useInstalledPluginReferences(
    selectedWakerID,
    sceneActive,
  );
  const enabledPlugins = pluginsQuery.plugins;
  // 已选插件失效时保留明确意图，由 Core 拒绝；候选刷新不能静默降级发送。
  const selectedPluginIDs =
    selectedPluginIDsByWaker[selectedWakerID ?? ""] ?? [];
  const selectedPlugins = enabledPlugins.filter((plugin) =>
    selectedPluginIDs.includes(plugin.id),
  );
  const setSelectedPluginIDs = (ids: string[]) => {
    if (selectedWakerID)
      setSelectedPluginIDsByWaker((current) => ({
        ...current,
        [selectedWakerID]: ids,
      }));
  };
  const skillsQuery = useQuery({
    ...listWakerSkillsOptions({
      path: { wakerId: selectedWakerID ?? "pending" },
    }),
    enabled: Boolean(selectedWakerID),
  });
  const connectorsQuery = useQuery({
    ...listWakerConnectorsOptions({
      path: { wakerId: selectedWakerID ?? "pending" },
    }),
    enabled: Boolean(selectedWakerID),
  });
  const recentProjectQuery = useQuery({
    ...getWakerRecentProjectOptions({
      path: { wakerId: selectedWakerID ?? "pending" },
    }),
    enabled: sceneActive && !activeChatId && !entry && Boolean(selectedWakerID),
  });
  const recentProjectID = recentProjectQuery.data?.projectId ?? "";
  const projects = projectsQuery.data?.resources ?? [];
  const enabledSkills = useMemo(
    () =>
      (skillsQuery.data ?? []).flatMap((item) => {
        const skill = item.skill;
        if (!item.enabled || !skill?.id) return [];
        return [
          {
            id: skill.id,
            name: skill.name ?? t("skillFallback"),
            description: skill.description ?? "",
            version: item.currentVersion?.version ?? "",
          },
        ];
      }),
    [skillsQuery.data, t],
  );
  const enabledSkillIDs = useMemo(
    () => new Set(enabledSkills.map((skill) => skill.id)),
    [enabledSkills],
  );
  const availableSkillIDs = useMemo(() => {
    if (!skillsQuery.data) return undefined;
    return new Set(
      skillsQuery.data.flatMap((item) =>
        item.skill?.id ? [item.skill.id] : [],
      ),
    );
  }, [skillsQuery.data]);
  const selectedSkillIDs = (
    selectedSkillIDsByWaker[selectedWakerID ?? ""] ?? []
  ).filter((skillID) => enabledSkillIDs.has(skillID));
  const selectedSkills = enabledSkills.filter((skill) =>
    selectedSkillIDs.includes(skill.id),
  );
  const setSelectedSkillIDs = (skillIDs: string[]) => {
    if (!selectedWakerID) return;
    setSelectedSkillIDsByWaker((current) => ({
      ...current,
      [selectedWakerID]: skillIDs,
    }));
  };
  const enabledConnectors = useMemo(
    () =>
      (connectorsQuery.data ?? []).flatMap((item) => {
        const connector = item.connector;
        if (
          !item.enabled ||
          !connector?.id ||
          !connectorSelectableInComposer(connector.activationPolicy)
        ) {
          return [];
        }
        return [
          {
            id: connector.id,
            name: connector.name ?? t("connectorFallback"),
            description: connector.description ?? "",
          },
        ];
      }),
    [connectorsQuery.data, t],
  );
  const enabledConnectorIDs = useMemo(
    () => new Set(enabledConnectors.map((connector) => connector.id)),
    [enabledConnectors],
  );
  const availableConnectorIDs = useMemo(() => {
    if (!connectorsQuery.data) return undefined;
    return new Set(
      connectorsQuery.data.flatMap((item) =>
        item.connector?.id ? [item.connector.id] : [],
      ),
    );
  }, [connectorsQuery.data]);
  const selectedConnectorIDs = (
    selectedConnectorIDsByWaker[selectedWakerID ?? ""] ?? []
  ).filter((connectorID) => enabledConnectorIDs.has(connectorID));
  const selectedConnectors = enabledConnectors.filter((connector) =>
    selectedConnectorIDs.includes(connector.id),
  );
  const setSelectedConnectorIDs = (connectorIDs: string[]) => {
    if (!selectedWakerID) return;
    setSelectedConnectorIDsByWaker((current) => ({
      ...current,
      [selectedWakerID]: connectorIDs,
    }));
  };
  const messages = useMemo(() => {
    if (!activeChatId) return [];
    const orderedPages = [...(messagesQuery.data?.pages ?? [])].reverse();
    const seen = new Set<string>();
    return orderedPages.flatMap((page) =>
      (page.resources ?? []).filter((message) => {
        if (!message.id || seen.has(message.id)) return false;
        seen.add(message.id);
        return true;
      }),
    );
  }, [activeChatId, messagesQuery.data]);
  const runtimeMessages = messages;
  const workspaceFiles = useMemo(
    () => workspaceFilesOf(runtimeMessages),
    [runtimeMessages],
  );
  const latestMessageSequence =
    messagesQuery.data?.pages[0]?.latestSequence ?? 0;
  const runtimes = runtimesQuery.data?.resources ?? [];
  const readyRuntimes = selectableRuntimes(runtimes);
  const defaultRuntime = readyRuntimes.find((item) => item.isDefault);
  const historicalRuntime = runtimes.find(
    (item) =>
      item.id ===
      (detailQuery.data?.chat?.lastRuntimeId ?? entry?.defaults?.runtimeId),
  );
  const historicalRuntimeDisabled = Boolean(
    detailQuery.data?.chat?.lastRuntimeId &&
    historicalRuntime &&
    !historicalRuntime.enabled,
  );
  const effectiveRuntimeID =
    runtimeId ||
    (presentation === "composer" ? entry?.defaults?.runtimeId : undefined) ||
    detailQuery.data?.chat?.lastRuntimeId ||
    entry?.defaults?.runtimeId ||
    defaultRuntime?.id ||
    readyRuntimes[0]?.id ||
    "";
  const selectedRuntime = readyRuntimes.find(
    (item) => item.id === effectiveRuntimeID,
  );
  const selectedAccountId =
    accountId ??
    (presentation === "composer" ? entry?.defaults?.accountId : undefined) ??
    (activeChatId
      ? detailQuery.data?.chat?.lastAccountId ||
        entry?.defaults?.accountId ||
        "system"
      : entry?.defaults?.accountId ||
        selectedRuntime?.defaultAccountId ||
        "system");
  const matchesLastExecutionSelection =
    selectedAccountId === (detailQuery.data?.chat?.lastAccountId || "system") &&
    effectiveRuntimeID === detailQuery.data?.chat?.lastRuntimeId;
  const executionSelection = useExecutionSelection({
    runtime: selectedRuntime,
    value: {
      runtimeId: effectiveRuntimeID,
      accountId: selectedAccountId,
      model:
        model ||
        (presentation === "composer" ? entry?.defaults?.model : undefined) ||
        (matchesLastExecutionSelection
          ? detailQuery.data?.chat?.lastModel
          : undefined) ||
        entry?.defaults?.model ||
        undefined,
      reasoning:
        reasoning ||
        (presentation === "composer"
          ? entry?.defaults?.reasoning
          : undefined) ||
        (matchesLastExecutionSelection
          ? detailQuery.data?.chat?.lastReasoning
          : undefined) ||
        entry?.defaults?.reasoning ||
        undefined,
      permissionLevel:
        permissionLevel ||
        (presentation === "composer"
          ? entry?.defaults?.permissionLevel
          : undefined) ||
        detailQuery.data?.chat?.lastPermissionLevel ||
        entry?.defaults?.permissionLevel ||
        undefined,
    },
    defaults: true,
    enabled: Boolean(selectedRuntime),
  });
  const selectedAccount = executionSelection.accounts.find(
    (item) => item.id === selectedAccountId,
  );
  const availableModels = executionSelection.models;
  const selectedModel = executionSelection.effectiveModel;
  const selectedModelOption = availableModels.find(
    (item) => item.id === selectedModel,
  );
  const thinkingQuery = executionSelection.thinkingQuery;
  const supportedPermissions = executionSelection.supportedPermissions;
  const selectedPermissionLevel = executionSelection.selection.permissionLevel;
  const selectedPermissionUnsupported = Boolean(
    selectedRuntime && !supportedPermissions.has(selectedPermissionLevel),
  );
  const defaultPermissionWasDowngraded = Boolean(
    selectedRuntime &&
    !permissionLevel &&
    !detailQuery.data?.chat?.lastPermissionLevel &&
    (selectedRuntime?.defaultPermissionLevel !== selectedPermissionLevel ||
      selectedRuntime?.diagnostics?.some(
        (item) => item.code === "PERMISSION_DEFAULT_DOWNGRADED",
      )),
  );
  const availableReasoningOptions = executionSelection.reasoningOptions;
  const selectedReasoning = executionSelection.selection.reasoning;
  const summaryRuntime = historicalRuntime ?? selectedRuntime;
  const summaryAccountId = detailQuery.data?.chat?.lastAccountId || "system";
  const summaryAccount = (accountsQuery.data?.resources ?? []).find(
    (account) => account.id === summaryAccountId,
  );
  const summaryModels =
    summaryAccountId !== "system" && summaryAccount?.authMode !== "subscription"
      ? (summaryAccount?.selectedModels ?? []).map((id) => ({
          id,
          name: summaryAccount?.modelDisplayNames?.[id] || id,
          reasoningOptions: [] as Array<{ id: string; name: string }>,
        }))
      : (summaryRuntime?.models ?? []);
  const summaryDefaultModel =
    summaryModels.find((item) => item.id === detailQuery.data?.chat?.lastModel)
      ?.id ??
    summaryModels[0]?.id ??
    "";
  const summaryDefaultReasoning =
    detailQuery.data?.chat?.lastReasoning ||
    summaryAccount?.defaultReasoning ||
    summaryRuntime?.defaultReasoning ||
    "";
  const updateExecutionSelection = (
    patch: Partial<{
      accountId: string;
      model: string;
      permissionLevel: PermissionLevel;
      reasoning: string;
      runtimeId: string;
    }>,
  ) => {
    if (
      activeChatId &&
      runtimeMessages.length > 0 &&
      ((patch.runtimeId !== undefined &&
        patch.runtimeId !== effectiveRuntimeID) ||
        (patch.accountId !== undefined &&
          patch.accountId !== selectedAccountId))
    ) {
      setSessionSwitch({
        chatId: activeChatId,
        patch,
        runtimeId: patch.runtimeId ?? effectiveRuntimeID,
        accountId: patch.accountId ?? selectedAccountId,
        reason: "switch",
      });
      return;
    }
    const { accountId: selectedAccount, ...viewPatch } = patch;
    if (selectedAccount !== undefined) selectAccount(selectedAccount);
    updateChatViewState(viewPatch);
  };
  const explicitDraftSelection =
    !entry && selectedWakerID
      ? draftWorkspaceSelections[selectedWakerID]
      : undefined;
  const selectedDraftProject =
    explicitDraftSelection?.type === "project"
      ? projects.find(
          (project) =>
            project.id === explicitDraftSelection.projectId &&
            project.available,
        )
      : explicitDraftSelection
        ? undefined
        : projects.find(
            (project) => project.id === recentProjectID && project.available,
          );
  const draftWorkingDirectory =
    explicitDraftSelection?.type === "folder"
      ? explicitDraftSelection.path
      : "";
  const workspaceDirectory =
    detailQuery.data?.chat?.workspaceDirectory ??
    (entry ? "" : draftWorkingDirectory);
  const linkAdditionalWorkingDirectories =
    detailQuery.data?.chat?.additionalWorkingDirectories;
  const gitWorkspaceDirectories = resolveGitWorkspaceDirectories({
    additionalWorkingDirectories: linkAdditionalWorkingDirectories,
    selectedDraftProject,
    workspaceDirectory,
    managed: Boolean(activeChatId && !detailQuery.data?.chat?.projectName),
  });
  const terminalWorkingDirectory =
    detailQuery.data?.chat?.additionalWorkingDirectories?.[0] ?? "";
  const fileRoots = (() => {
    if (activeChatId && workspaceDirectory) return [workspaceDirectory];
    const project = selectedDraftProject;
    const projectRoots = (project?.sources ?? [])
      .filter((source) => source.available && source.path)
      .flatMap((source) => (source.path ? [source.path] : []));
    if (projectRoots.length > 0) return projectRoots;
    return workspaceDirectory ? [workspaceDirectory] : [];
  })();

  const refreshChatMetadata = useCallback(
    (targetChatId?: string, targetWakerId?: string) => {
      if (targetChatId) {
        void queryClient.invalidateQueries({
          queryKey: getChatQueryKey({ path: { chatId: targetChatId } }),
        });
      }
      if (targetWakerId) {
        void queryClient.invalidateQueries({
          queryKey: chatListKey(targetWakerId),
        });
      }
    },
    [queryClient],
  );
  const refreshEntry = entry?.onRefresh;
  const refreshDetail = useCallback(() => {
    refreshChatMetadata(activeChatId, selectedWakerID);
    refreshEntry?.();
  }, [activeChatId, refreshChatMetadata, selectedWakerID, refreshEntry]);
  const acceptStarted = useCallback(
    (result: AcceptedExecution) => {
      const createdID = result.chat?.id;
      if (entry) {
        if (createdID && !activeChatId) {
          markSkipComposerClearOnCreate();
          workbench.bindToChat(createdID);
          workspace.dispatch({
            type: "migrate",
            toScopeKey: `chat:${createdID}`,
          });
        }
        entry.onAccepted(result);
        entry.onRefresh?.();
      } else if (createdID && !chatId) {
        markSkipComposerClearOnCreate();
        workspace.dispatch({
          type: "migrate",
          toScopeKey: `chat:${createdID}`,
        });
        navigate(
          chatHref(createdID, result.chat?.wakerId ?? selectedWaker?.id ?? ""),
        );
      }
    },
    [
      activeChatId,
      chatId,
      entry,
      markSkipComposerClearOnCreate,
      navigate,
      selectedWaker?.id,
      workbench,
      workspace,
    ],
  );

  const send = useMutation(sendMessageMutation());
  const create = useMutation(createChatMutation());
  const cancel = useMutation(cancelChatExecutionMutation());
  const resolveApproval = useMutation(resolveApprovalMutation());
  const resolveElicitation = useMutation(resolveElicitationMutation());
  const notifyApproval = useCallback(
    (approval: PendingApproval) => {
      const approvalId = approval.approvalId;
      if (!approvalId) return;
      if (selectedWakerID) {
        void queryClient.invalidateQueries({
          queryKey: chatListKey(selectedWakerID),
        });
      }
    },
    [queryClient, selectedWakerID],
  );
  const chatRuntime = useChatRuntime({
    conversationKey: currentChatKey,
    queueKey: runtimeQueueKey,
    queueActive: sceneActive,
    chatId: activeChatId,
    messageSource: chatMessageSource(queryClient, activeChatId, {
      messages: runtimeMessages,
      latestSequence: latestMessageSequence,
      ready: !activeChatId || Boolean(messagesQuery.data),
    }),
    activeExecutionId: detailQuery.data?.chat?.activeExecutionId,
    initialPendingApprovals: detailQuery.data?.chat?.pendingApprovals,
    initialPendingElicitations: detailQuery.data?.chat?.pendingElicitations,
    isLoading: Boolean(
      activeChatId && (detailQuery.isLoading || messagesQuery.isLoading),
    ),
    isSendDisabled: Boolean(
      routeTransitioning ||
      !sceneActive ||
      entry?.isHistoryLoading ||
      entry?.isSendDisabled ||
      !selectedWakerID ||
      !effectiveRuntimeID ||
      selectedPermissionUnsupported ||
      (presentation === "composer" &&
        Boolean(detailQuery.data?.chat?.activeExecutionId)),
    ),
    serverManagedQueue: false,

    attachmentOnlyText: t("attachmentOnlyMessage"),
    referenceOnlyText: t("codeReferenceOnlyMessage"),
    incompleteErrorText: t("executionIncomplete"),
    selection: {
      accountId: selectedAccountId,
      runtimeId: selectedRuntime?.id ?? "",
      model: selectedModel,
      reasoning: selectedReasoning,
      permissionLevel: selectedPermissionLevel,
      skillIds: selectedSkillIDs,
      selectedSkills,
      pluginIds: selectedPluginIDs,
      selectedPlugins,
      connectorIds: selectedConnectorIDs,
      selectedConnectors,
      codeReferences: selectedCodeReferences,
    },
    onMessageAccepted: () => {
      clearComposer();
    },
    onMessageRejected: (selection) => {
      setComposerRestoreRevision((revision) => revision + 1);
      if (!selectedWakerID) return;
      setSelectedSkillIDsByWaker((current) => ({
        ...current,
        [selectedWakerID]: Array.from(
          new Set([
            ...(current[selectedWakerID] ?? []),
            ...(selection.skillIds ?? []),
          ]),
        ),
      }));
      setSelectedPluginIDsByWaker((current) => ({
        ...current,
        [selectedWakerID]: Array.from(
          new Set([
            ...(current[selectedWakerID] ?? []),
            ...(selection.pluginIds ?? []),
          ]),
        ),
      }));
      setSelectedConnectorIDsByWaker((current) => ({
        ...current,
        [selectedWakerID]: Array.from(
          new Set([
            ...(current[selectedWakerID] ?? []),
            ...(selection.connectorIds ?? []),
          ]),
        ),
      }));
    },
    onApprovalRequested: notifyApproval,
    suggestions: chatSuggestionKeys.map((key) => ({ prompt: t(key) })),
    prepareSubmission: (
      text,
      targetChatId,
      attachmentPaths = [],
      selection,
    ) => {
      const message = {
        text,
        accountId: selection?.accountId ?? "system",
        runtimeId: selection?.runtimeId ?? "",
        model: selection?.model ?? "",
        reasoning: selection?.reasoning ?? "",
        permissionLevel: selection?.permissionLevel ?? "auto",
        attachments: attachmentPaths.map((sourcePath) => ({ sourcePath })),
        skillIds: selection?.skillIds ?? [],
        pluginIds: selection?.pluginIds ?? [],
        connectorIds: selection?.connectorIds ?? [],
        codeReferences: (selection?.codeReferences ?? []).flatMap(
          ({ path, startLine, endLine, content }) => {
            if (!path) return [];
            if (!startLine && !endLine && !content) {
              return [{ path, startLine: 0, endLine: 0, content: "" }];
            }
            return startLine && endLine && content
              ? [{ path, startLine, endLine, content }]
              : [];
          },
        ),
      };
      const sessionTransition =
        targetChatId &&
        freshSession?.chatId === targetChatId &&
        freshSession.runtimeId === message.runtimeId &&
        freshSession.accountId === message.accountId
          ? freshSession
          : null;
      const body: ChatMessageRequest = {
        ...message,
        ...(sessionTransition
          ? {
              startNewSession: true,
              historyTurns: sessionTransition.historyTurns,
              historyMode: sessionTransition.historyMode,
              summaryModel: sessionTransition.summaryModel,
              summaryReasoning: sessionTransition.summaryReasoning,
            }
          : {}),
      };
      if (entry) return entry.prepareSubmission(body);
      if (targetChatId) {
        const requestScope = `${workspaceScopeKey}:send:${targetChatId}`;
        const prepared = withStableCreateRequest(requestScope, body);
        const submissionId = prepared.clientRequestId;
        return {
          submissionId,
          confirmAccepted: () => {
            clearStableCreateRequest(requestScope, submissionId);
            if (sessionTransition)
              setFreshSession((current) =>
                current === freshSession ? null : current,
              );
          },
          accept: () =>
            send.mutateAsync({
              path: { chatId: targetChatId },
              body: { ...body, submissionId },
            }),
        };
      }
      const createRequest = withStableCreateRequest(workspaceScopeKey, {
        ...message,
        wakerId: selectedWaker?.id ?? "",
        projectId: selectedDraftProject?.id ?? "",
        workingDirectory: selectedDraftProject ? "" : workspaceDirectory,
      });
      return {
        submissionId: createRequest.clientRequestId,
        confirmAccepted: () =>
          clearStableCreateRequest(
            workspaceScopeKey,
            createRequest.clientRequestId,
          ),
        accept: () => create.mutateAsync({ body: createRequest }),
      };
    },
    cancel: async () => {
      const invocation = chatInvocationGuard.capture(
        activeChatId,
        runtimeQueueKey,
      );
      if (!invocation.chatId || !chatInvocationGuard.isCurrent(invocation))
        return;
      await cancel.mutateAsync({ path: { chatId: invocation.chatId } });
    },
    onSettled: (result, invocation) => {
      const hasSnapshot = commitAcceptedChat(queryClient, result.chat);
      refreshChatMetadata(
        hasSnapshot ? undefined : (result.chat?.id ?? invocation.chatId),
        result.chat?.wakerId ?? selectedWakerID,
      );
    },
    onStarted: (result) => {
      if (!sceneActiveRef.current) {
        pendingAcceptedRef.current = { key: currentChatKey, result };
        return;
      }
      acceptStarted(result);
    },
    onTerminal: refreshDetail,
  });
  useEffect(() => {
    const pending = pendingAcceptedRef.current;
    if (!sceneActiveRef.current || !pending || pending.key !== currentChatKey)
      return;
    pendingAcceptedRef.current = null;
    acceptStarted(pending.result);
  }, [acceptStarted, sceneActive, currentChatKey]);
  const configurationChanged = entry?.onConfigurationChange;
  const runningChanged = entry?.onRunningChange;
  useEffect(() => {
    configurationChanged?.(
      sceneActive && !executionSelection.loading && effectiveRuntimeID
        ? {
            runtimeId: effectiveRuntimeID,
            accountId: selectedAccountId,
            model: selectedModel,
            reasoning: selectedReasoning,
            permissionLevel: selectedPermissionLevel,
          }
        : undefined,
    );
  }, [
    configurationChanged,
    sceneActive,
    executionSelection.loading,
    effectiveRuntimeID,
    selectedAccountId,
    selectedModel,
    selectedReasoning,
    selectedPermissionLevel,
  ]);
  useEffect(() => {
    if (sceneActive) runningChanged?.(chatRuntime.isRunning);
  }, [runningChanged, chatRuntime.isRunning, sceneActive]);
  useEffect(() => {
    if (chatRuntime.sendFailure.kind !== "session-not-found") {
      offeredRecoveryRef.current = "";
      return;
    }
    if (!activeChatId || sessionSwitch || freshSession) return;
    const key = `${activeChatId}:${effectiveRuntimeID}:${selectedAccountId}`;
    if (offeredRecoveryRef.current === key) return;
    offeredRecoveryRef.current = key;
    setSessionSwitch({
      chatId: activeChatId,
      patch: {},
      runtimeId: effectiveRuntimeID,
      accountId: selectedAccountId,
      reason: "recovery",
    });
  }, [
    activeChatId,
    chatRuntime.sendFailure.kind,
    effectiveRuntimeID,
    freshSession,
    selectedAccountId,
    sessionSwitch,
  ]);
  const fork = useChatFork({
    messages,
    latestExecutionId: messagesQuery.data?.pages[0]?.forkLatestExecutionId,
    chatId: activeMemberTab || entry ? undefined : activeChatId,
    wakerId: selectedWakerID,
    enabled: sceneActive && detailQuery.isSuccess && messagesQuery.isSuccess,
    busy:
      chatRuntime.isRunning ||
      chatRuntime.pendingApprovals.length > 0 ||
      chatRuntime.pendingElicitations.length > 0,
  });
  const {
    approvalSubmissionState,
    elicitationSubmissionState,
    resolveApprovalChoice,
    resolveElicitationChoice,
  } = useChatInteractions({
    chatId: activeChatId,
    conversationKey: currentChatKey,
    guard: chatInvocationGuard,
    onSettled: (targetChatId) =>
      refreshChatMetadata(targetChatId, selectedWakerID),
    pendingApprovals: chatRuntime.pendingApprovals,
    pendingElicitations: chatRuntime.pendingElicitations,
    queueKey: runtimeQueueKey,
    resolveApproval: (targetChatId, approvalId, optionId) =>
      resolveApproval.mutateAsync({
        path: { chatId: targetChatId, approvalId },
        body: { optionId },
      }),
    resolveElicitation: (targetChatId, elicitationId, content, declined) =>
      resolveElicitation.mutateAsync({
        path: { chatId: targetChatId, elicitationId },
        body: { content, declined },
      }),
  });
  const historyIsLoading = Boolean(
    entry?.isHistoryLoading ||
    routeTransitioning ||
    (activeChatId &&
      !messagesQuery.isSuccess &&
      !chatRuntime.hasVisibleMessages),
  );

  const pickDirectory = async () => {
    const selected = await selectDirectory();
    if (selected && selectedWaker?.id) {
      selectDraftWorkspace(selectedWaker.id, {
        type: "folder",
        path: selected,
      });
    }
  };
  const selectedModelName = selectedModelOption?.name ?? selectedModel;
  const wakerName = selectedWaker?.name ?? t("wakerFallback");
  const conversationAvatar = selectedWaker?.avatar;
  const conversationWakerId = selectedWakerID;
  const workspaceLabel =
    entry?.directoryLabel ||
    detailQuery.data?.chat?.projectName ||
    (activeChatId && !detailQuery.data?.chat?.projectName
      ? t("defaultWorkspace")
      : "") ||
    selectedDraftProject?.name ||
    (workspaceDirectory ? workspaceDirectory.split(/[\\/]/).pop() : "") ||
    t("selectWorkspace");
  const permissionWarning = selectedPermissionUnsupported
    ? t("permissionUnsupported")
    : defaultPermissionWasDowngraded
      ? t("permissionDowngraded")
      : "";

  const greetingPhrases = useMemo(
    () => [
      t("greeting"),
      t("greetingPhrases.start"),
      t("greetingPhrases.progress"),
      t("greetingPhrases.next"),
      t("greetingPhrases.share"),
    ],
    [t],
  );
  const emptyState = (
    <div className="mx-auto grid w-full max-w-[560px] justify-items-center px-6 pt-[clamp(64px,12vh,140px)] text-center">
      <Avatar
        name={wakerName}
        src={conversationAvatar}
        size="docked"
        preview
        summon={false}
        activity={chatRuntime.isRunning ? "starting" : "idle"}
      />
      {selectedWaker ? (
        <TypedPhrases
          as="h1"
          phrases={greetingPhrases}
          className="type-page-title mt-6 text-center font-semibold"
        />
      ) : (
        <h1 className="type-page-title mt-6 font-semibold">
          {t("createWakerFirst")}
        </h1>
      )}
      {selectedWaker && (
        <div className="mt-6 flex w-full flex-wrap justify-center gap-2 [&>div]:flex [&>div]:w-full [&>div]:flex-wrap [&>div]:justify-center [&>div]:gap-2">
          <ThreadPrimitive.Suggestions>
            {() => (
              <SuggestionPrimitive.Trigger className="bg-muted hover:border-border-strong hover:bg-surface type-body inline-flex max-w-full min-w-0 items-start gap-2 rounded-md border border-transparent px-3 py-2 text-left whitespace-normal transition-colors [&_*]:min-w-0 [&_*]:whitespace-normal">
                <Sparkles
                  className="text-muted-foreground size-icon-md shrink-0"
                  strokeWidth={1.5}
                />
                <SuggestionPrimitive.Title />
              </SuggestionPrimitive.Trigger>
            )}
          </ThreadPrimitive.Suggestions>
        </div>
      )}
    </div>
  );
  const contributedEmptyState =
    typeof entry?.emptyState === "function"
      ? entry.emptyState({
          wakerName,
          avatar: conversationAvatar,
          isRunning: chatRuntime.isRunning,
        })
      : entry?.emptyState;
  const historyLoadingState = <ChatHistoryLoadingState />;
  const messageEmptyState = historyIsLoading
    ? historyLoadingState
    : chatRuntime.hasVisibleMessages
      ? null
      : entry?.emptyState !== undefined
        ? contributedEmptyState
        : emptyState;

  const {
    composerSettings,
    composerEffects,
    renderContextStrip: renderComposerContextStrip,
  } = useChatComposerControls({
    workspaceReadOnly: Boolean(entry),
    selectedAccountId,
    activeChatId,
    availableReasoningOptions,
    modelOptionsOverride: availableModels,
    thinkingPending: Boolean(
      selectedAccount && thinkingQuery.isPending && thinkingQuery.isFetching,
    ),
    historicalRuntime,
    historicalRuntimeDisabled,
    onPickDirectory: pickDirectory,
    onSelectProject: (projectId) => {
      if (!selectedWakerID) return;
      selectDraftWorkspace(selectedWakerID, { type: "project", projectId });
    },
    projects,
    runtimeId,
    runtimes,
    selectedDraftProject,
    selectedModel,
    selectedModelName,
    selectedPermissionLevel,
    selectedReasoning,
    selectedRuntime,
    selectedWakerId: selectedWakerID,
    supportedPermissions,
    updateExecutionSelection,
    gitWorkspaceDirectories,
    workspaceDirectory,
    workspaceLabel,
  });
  const {
    addCodeReference,
    openAttachmentFile,
    openBrowserLink,
    openChatTerminal,
    openCodeReference,
    openExecutionFileChange,
    fileWorkspaceProps,
    changesProps,
  } = useChatWorkspaceBindings({
    additionalWorkingDirectories: linkAdditionalWorkingDirectories,
    activeChatId,
    fileSource,
    query: workspaceQuery,
    terminalWorkingDirectory,
    updateViewState: updateChatViewState,
    workspaceDirectory,
    workspaceFiles,
    workspacePanelOpen: chatRightPanel === "workspace",
    workspaceScopeKey,
    onOpenWorkbench: () => {
      if (
        presentation === "composer" &&
        composerHost?.scopeKey === workspaceScopeKey
      )
        composerHost.onOpen();
      else setChatRightPanel("workspace");
    },
  });
  const chatAdapter: ChatWorkspaceAdapter = {
    identity: {
      chatId: activeChatId,
      messageContributions:
        Boolean(detailQuery.data?.chat?.hiddenFromWakerHistory) && !entry,
      key: currentChatKey,
      scopeKey: workspaceScopeKey,
      terminalWorkingDirectory,
      workspaceDirectory,
    },
    snapshot: {
      isRunning: chatRuntime.isRunning,
      pendingApprovalCount: chatRuntime.pendingApprovals.length,
      pendingElicitationCount: chatRuntime.pendingElicitations.length,
      streamState: chatRuntime.streamState,
    },
    thread: {
      fileScopeChatId: activeChatId,
      resultSource:
        activeChatId && !entry
          ? {
              chatId: activeChatId,
              owner: { kind: "chat", id: activeChatId },
            }
          : undefined,
      fork: activeMemberTab || entry ? undefined : fork,
      forkSourceChatId: detailQuery.data?.chat?.forkSourceChatId,
      onOpenForkSource: async (sourceChatId: string) => {
        try {
          await queryClient.fetchQuery({
            ...getChatOptions({ path: { chatId: sourceChatId } }),
            staleTime: 0,
          });
          navigate(chatHref(sourceChatId, selectedWakerID ?? ""));
          return true;
        } catch {
          return false;
        }
      },
      forkBoundaryMessageId: detailQuery.data?.chat?.forkSourceChatId
        ? runtimeMessages.findLast((message) => message.inherited)?.id
        : undefined,
      runtime: chatRuntime.runtime,
      wakerName,
      wakerId: conversationWakerId,
      wakerAvatar: conversationAvatar,
      emptyState: messageEmptyState,
      isHistoryLoading: historyIsLoading,
      composerSettings,
      composerEffects,
      composerDraft: entry?.draft,
      renderComposerContextStrip,
      composerSkills: enabledSkills,
      composerSkillsError: skillsQuery.isError,
      composerPlugins: enabledPlugins,
      composerPluginsError: pluginsQuery.isError,
      selectedPluginIds: selectedPluginIDs,
      onPluginSelectionChange: setSelectedPluginIDs,
      composerConnectors: enabledConnectors,
      composerConnectorsError: connectorsQuery.isError,
      composerFileRoots: fileRoots,
      composerFileScopePath: projectFilesScopePath(activeChatId) || undefined,
      availableSkillIds: availableSkillIDs,
      availablePluginIds: pluginsQuery.data
        ? new Set(enabledPlugins.map((plugin) => plugin.id))
        : undefined,
      availableConnectorIds: availableConnectorIDs,
      selectedSkillIds: selectedSkillIDs,
      selectedConnectorIds: selectedConnectorIDs,
      onSkillSelectionChange: setSelectedSkillIDs,
      onConnectorSelectionChange: setSelectedConnectorIDs,
      codeReferenceRequest,
      composerClearRevision,
      companionScopeRevision,
      composerRestoreRevision,
      terminalEnabled: Boolean(activeChatId && workspaceDirectory),
      linkWorkspaceRoot: workspaceDirectory,
      linkAdditionalWorkingDirectories,
      onOpenTerminal: openChatTerminal,
      onOpenLink: openBrowserLink,
      onCodeReferencesChange: (references) =>
        updateChatViewState({ selectedCodeReferences: references }),
      onOpenCodeReference: openCodeReference,
      onOpenFile: openAttachmentFile,
      onOpenFileChange: openExecutionFileChange,
      contextMetrics: chatRuntime.contextMetrics,
      sendFailure: chatRuntime.sendFailure,
      errorScopes: [runtimeQueueKey, `chat:${activeChatId}`],
      onRetryExecution: chatRuntime.retryExecution,
      onRecoverSession: () => {
        if (!activeChatId) return;
        setSessionSwitch({
          chatId: activeChatId,
          patch: {},
          runtimeId: effectiveRuntimeID,
          accountId: selectedAccountId,
          reason: "recovery",
        });
      },
      hasEarlierMessages: sceneActive && Boolean(messagesQuery.hasNextPage),
      isLoadingEarlier: messagesQuery.isFetchingNextPage,
      onLoadEarlier: () =>
        messagesQuery
          .fetchNextPage({ cancelRefetch: false, throwOnError: true })
          .then(() => {}),
      warning: permissionWarning || undefined,
      requestedInteractionId: searchParams.get("request")?.split(":").at(-1),
      pendingApprovals: sceneActive ? chatRuntime.pendingApprovals : [],
      approvalSubmissionState,
      onResolveApproval: (approvalId, optionId) =>
        void resolveApprovalChoice(approvalId, optionId),
      pendingElicitations: sceneActive ? chatRuntime.pendingElicitations : [],
      elicitationSubmissionState,
      onResolveElicitation: (elicitationId, content, declined) =>
        void resolveElicitationChoice(elicitationId, content, declined),
    },
  };

  if (embedded) {
    return (
      <div className="bg-surface-scene h-full min-h-0 min-w-0">
        <ChatThread
          {...chatAdapter.thread}
          isRunning={chatAdapter.snapshot.isRunning}
        />
      </div>
    );
  }

  return (
    <>
      {presentation === "composer" ? (
        <ChatThread
          {...chatAdapter.thread}
          presentation="composer"
          composerAutoFocus
          composerDisabled={Boolean(
            entry?.isSendDisabled || detailQuery.data?.chat?.activeExecutionId,
          )}
          isRunning={chatAdapter.snapshot.isRunning}
        />
      ) : (
        <ChatWorkspaceSurface
          active={entry?.active}
          mainContent={entry?.mainContent}
          canvasInput={entry?.canvasInput}
          showSummary={entry?.showSummary}
          directoryLabel={entry?.directoryLabel}
          onNewChat={entry?.onNewChat}
          history={entry?.history}
          activeRightPanel={chatRightPanel}
          adapter={chatAdapter}
          onAddCodeReference={addCodeReference}
          onDismissRightPanel={() => setChatRightPanel(undefined)}
          onOpenRightPanel={() => setChatRightPanel("workspace")}
          openedCodeReference={openedCodeReference}
          gitRoots={gitWorkspaceDirectories}
          workspace={workspace}
          fileWorkspaceProps={fileWorkspaceProps}
          changesProps={changesProps}
        />
      )}
      <ChatSessionTransitionDialog
        open={Boolean(sessionSwitch && sessionSwitch.chatId === activeChatId)}
        reason={sessionSwitch?.reason ?? "switch"}
        models={summaryModels.flatMap((item) =>
          item.id
            ? [
                {
                  id: item.id,
                  name: item.name || item.id,
                  reasoningOptions: (item.reasoningOptions ?? []).flatMap(
                    (option) =>
                      option.id
                        ? [{ id: option.id, name: option.name || option.id }]
                        : [],
                  ),
                },
              ]
            : [],
        )}
        reasoningOptions={(summaryRuntime?.reasoningOptions ?? []).flatMap(
          (option) =>
            option.id
              ? [{ id: option.id, name: option.name || option.id }]
              : [],
        )}
        defaultModel={summaryDefaultModel}
        defaultReasoning={summaryDefaultReasoning}
        onCancel={() => setSessionSwitch(null)}
        onConfirm={(choice) => {
          if (!sessionSwitch || sessionSwitch.chatId !== activeChatId) return;
          const { accountId: nextAccount, ...viewPatch } = sessionSwitch.patch;
          if (nextAccount !== undefined) selectAccount(nextAccount);
          updateChatViewState(viewPatch);
          setFreshSession({
            chatId: sessionSwitch.chatId,
            runtimeId: sessionSwitch.runtimeId,
            accountId: sessionSwitch.accountId,
            historyTurns: choice.turns,
            historyMode: choice.mode,
            summaryModel: choice.summaryModel,
            summaryReasoning: choice.summaryReasoning,
          });
          setSessionSwitch(null);
        }}
      />
    </>
  );
}
