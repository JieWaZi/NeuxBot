import { getWakerRecentProjectOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { useInstalledPluginReferences } from "@/modules/plugin";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { useDraftWorkspaceSelections } from "./use-draft-workspace-selections";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext, useSearchParams } from "react-router-dom";

import {
  cancelChatExecutionMutation,
  chatListKey,
  chatMessagesInfiniteOptions,
  chatMessageSource,
  commitAcceptedChat,
  useChatHistoryVisibility,
  getChatOptions,
  getChatQueryKey,
  listRuntimesOptions,
  listWakerSkillsOptions,
  resolveApprovalMutation,
  resolveElicitationMutation,
  sendMessageMutation,
} from "../api/chat-queries";
import { ChatHistoryLoadingState } from "../components/chat-history-loading-state";
import { TeamWorkStatus } from "../components/team-work-status";
import {
  ChatWorkspaceSurface,
  type ChatWorkspaceAdapter,
} from "../components/chat-workspace-surface";
import { useChatComposerControls } from "../components/chat-composer-controls";
import { workspaceFilesOf } from "../model/chat-workspace-files";
import { resolveGitWorkspaceDirectories } from "../model/git-workspace-directories";
import { useChatWorkspaceBindings } from "../components/use-chat-workspace-bindings";
import { useComposerScopeTransition } from "../model/use-composer-scope-transition";
import { useChatViewState } from "../model/use-chat-view-state";
import { projectFilesScopePath } from "../model/project-files-scope";
import {
  conversationKeyOf,
  useChatInvocationGuard,
  type ChatScopeKey,
} from "../model/conversation-identity";
import { obsoleteTeamMemberTabIds } from "../model/team-member-tabs";
import { useChatInteractions } from "../model/use-conversation-interactions";
import { useChatRuntime } from "../runtime/use-chat-runtime";
import {
  useRouteSceneActive,
  useScenePresentationKey,
} from "@/shared/workspace/app-scene-workspace";
import { listProjectsOptions } from "@/modules/project";
import {
  connectorSelectableInComposer,
  listWakerConnectorsOptions,
} from "@/modules/connector";
import { listWakersOptions } from "@/modules/waker";
import { selectDirectory } from "@/shared/platform";
import {
  reportChatError,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import {
  type TeamChatContext,
  type TeamMemberExecutionConfig,
} from "@/modules/team";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChat as Chat,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval,
} from "@/shared/api/generated/types.gen";
import {
  clearStableCreateRequest,
  withStableCreateRequest,
} from "@/shared/lib/create-request-idempotency";
import { selectableRuntimes } from "../model/selectable-runtimes";
import { Avatar } from "@/shared/ui/avatar";
import { TypedPhrases } from "@/shared/ui/typed-phrases";
import { useWorkbench } from "../workbench/workbench-store";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";
import type { WorkspaceMemberTab } from "@/shared/workspace/workspace-tab";
import type { ChatOutletContext } from "./chat-page-shared";
import {
  teamChatPresentation,
  teamMainChatMessageVisible,
} from "./team-chat-presentation";

type TeamChatPageContentProps = {
  chatId?: string;
  routeTransitioning?: boolean;
  teamContext: TeamChatContext;
  presentationScopeKey?: string;
};
type TeamDraftConfigurations = Record<
  string,
  Record<string, TeamMemberExecutionConfig>
>;

/** 团队主聊天与成员工作区分别拥有呈现生命周期，首发沿用 Scene 身份。 */
export function TeamChatPageContent(props: TeamChatPageContentProps) {
  const scopeKey = useScenePresentationKey(props.presentationScopeKey);
  const [searchParams] = useSearchParams();
  const workspaceScopeKey = `team:${props.teamContext.channelId}:${props.teamContext.threadId ?? `draft:${searchParams.get("draftId") ?? "default"}`}`;
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const memberConfigurations = useState<TeamDraftConfigurations>({});
  const createdTeamThreadIds = useRef(new Map<ChatScopeKey, string>());
  const member = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId && tab.kind === "member",
  );
  return (
    <TeamChatView
      key={JSON.stringify([scopeKey, member?.id ?? null])}
      {...props}
      workspaceScopeKey={workspaceScopeKey}
      memberConfigurations={memberConfigurations}
      createdTeamThreadIds={createdTeamThreadIds}
    />
  );
}

function TeamChatView({
  chatId,
  routeTransitioning = false,
  teamContext,
  workspaceScopeKey,
  memberConfigurations,
  createdTeamThreadIds,
}: TeamChatPageContentProps & {
  workspaceScopeKey: string;
  memberConfigurations: [
    TeamDraftConfigurations,
    Dispatch<SetStateAction<TeamDraftConfigurations>>,
  ];
  createdTeamThreadIds: RefObject<Map<ChatScopeKey, string>>;
}) {
  const { t } = useTranslation("chat");
  const sceneActive = useRouteSceneActive();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const { chatRightPanel, setChatRightPanel, renderTeamMembers } =
    useOutletContext<ChatOutletContext>();
  const [teamMentionWakerId, setTeamMentionWakerId] = useState("");
  const [draftTeamMemberConfigsByScope, setDraftTeamMemberConfigsByScope] =
    memberConfigurations;
  const [draftWorkspaceSelections, selectDraftWorkspace] =
    useDraftWorkspaceSelections();
  const draftWorkspacePreferenceKey = `team:${teamContext.channelId}`;
  const [selectedSkillIDsByWaker, setSelectedSkillIDsByWaker] = useState<
    Record<string, string[]>
  >({});
  const [selectedPluginIDsByWaker, setSelectedPluginIDsByWaker] = useState<
    Record<string, string[]>
  >({});
  const [selectedConnectorIDsByWaker, setSelectedConnectorIDsByWaker] =
    useState<Record<string, string[]>>({});
  const [composerRestoreRevision, setComposerRestoreRevision] = useState(0);
  const { state: chatViewState, update: updateChatViewState } =
    useChatViewState(workspaceScopeKey);
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const taskWorkbench = useWorkbench(workspaceScopeKey);
  const openedTaskLink = useRef("");
  const requestedTask = searchParams.get("task");
  useEffect(() => {
    if (!sceneActive || !requestedTask) {
      openedTaskLink.current = "";
      return;
    }
    if (!teamContext.threadId) return;
    const key = `${workspaceScopeKey}:${requestedTask}`;
    if (openedTaskLink.current === key) return;
    openedTaskLink.current = key;
    taskWorkbench.dispatch({
      type: "open-task-board",
      title: t("team:taskBoard.boardTitle"),
    });
    setChatRightPanel("workspace");
  }, [
    sceneActive,
    requestedTask,
    teamContext.threadId,
    workspaceScopeKey,
    taskWorkbench,
    setChatRightPanel,
    t,
  ]);
  const activeMemberTab = workspace.snapshot.tabs.find(
    (tab): tab is WorkspaceMemberTab =>
      tab.id === workspace.snapshot.activeTabId && tab.kind === "member",
  );
  const activeMemberWakerId = activeMemberTab?.wakerId;
  const activeChatId = activeMemberTab?.chatId ?? chatId;
  const isTeamMainChat = !activeMemberTab;
  const currentChatKey = conversationKeyOf(workspaceScopeKey, activeChatId);
  const resetComposerScope = useCallback(() => {
    setSelectedSkillIDsByWaker({});
    setSelectedPluginIDsByWaker({});
    setSelectedConnectorIDsByWaker({});
    setTeamMentionWakerId("");
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
  const runtimeQueueKey = activeChatId ?? workspaceScopeKey;
  const chatInvocationGuard = useChatInvocationGuard(currentChatKey);
  useEffect(() => {
    if (!teamContext.threadId || teamContext.threadDetailLoading) return;
    for (const tabId of obsoleteTeamMemberTabIds(
      workspace.snapshot.tabs,
      teamContext.members,
    )) {
      workspace.dispatch({ type: "close-tab", tabId });
    }
  }, [
    teamContext.members,
    teamContext.threadDetailLoading,
    teamContext.threadId,
    workspace,
    workspaceScopeKey,
  ]);
  const draftTeamMemberConfigs = useMemo(
    () => draftTeamMemberConfigsByScope[workspaceScopeKey] ?? {},
    [draftTeamMemberConfigsByScope, workspaceScopeKey],
  );
  const {
    codeReferenceRequest,
    fileSource,
    openedCodeReference,
    query: workspaceQuery,
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
  useChatHistoryVisibility(
    isTeamMainChat ? undefined : activeChatId,
    sceneActive,
  );
  const messagesQuery = useInfiniteQuery({
    ...chatMessagesInfiniteOptions(activeChatId ?? "pending"),
    enabled: sceneActive && !isTeamMainChat && Boolean(activeChatId),
  });
  const runtimesQuery = useQuery(listRuntimesOptions());
  const wakers = wakersQuery.data?.resources ?? [];
  const requestedWaker = searchParams.get("wakerId")?.trim() ?? "";
  const targetWakerID =
    activeMemberWakerId ||
    teamContext.leadWakerId ||
    detailQuery.data?.chat?.wakerId ||
    requestedWaker ||
    wakers[0]?.id ||
    "";
  const selectedWaker =
    wakers.find((waker) => waker.id === targetWakerID) ??
    (targetWakerID ? undefined : wakers[0]);
  const selectedWakerID = targetWakerID || selectedWaker?.id;
  const effectiveTeamConfigTargetWakerId =
    activeMemberWakerId ?? teamContext.leadWakerId ?? selectedWakerID;
  const teamConfigDisplayWakerId = effectiveTeamConfigTargetWakerId;
  const activeTeamMember = teamContext.members.find(
    (member) => member.id === teamConfigDisplayWakerId,
  );
  const teamExecutionConfig = activeTeamMember
    ? teamContext.threadId
      ? activeTeamMember
      : (draftTeamMemberConfigs[activeTeamMember.id] ?? activeTeamMember)
    : undefined;
  const workspaceOwnerWakerId = teamContext.leadWakerId ?? selectedWakerID;
  const projectsQuery = useQuery({
    ...listProjectsOptions({
      path: { wakerId: workspaceOwnerWakerId ?? "pending" },
      query: { pageNum: 1, pageSize: 100 },
    }),
    enabled: Boolean(workspaceOwnerWakerId),
  });
  const pluginsQuery = useInstalledPluginReferences(selectedWakerID);
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
      path: { wakerId: teamContext.leadWakerId || "pending" },
    }),
    enabled:
      sceneActive && !teamContext.threadId && Boolean(teamContext.leadWakerId),
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
  const runtimeMessages = useMemo(
    () =>
      isTeamMainChat ? teamContext.messageSource.snapshot.messages : messages,
    [isTeamMainChat, messages, teamContext],
  );
  const teamMessageAuthors = useMemo(() => {
    if (!isTeamMainChat) return undefined;
    const memberByID = new Map(
      teamContext.members.map((member) => [member.id, member] as const),
    );
    return Object.fromEntries(
      teamContext.timeline.flatMap((item) => {
        const messageID = item.message.id;
        const member = item.wakerId ? memberByID.get(item.wakerId) : undefined;
        if (!messageID || !item.wakerId) return [];
        return [
          [
            messageID,
            {
              name: member?.name ?? t("wakerFallback"),
              avatar: member?.avatar,
              sourceChatId: item.message.sourceChatId ?? item.chatId,
              displayAsParticipant:
                item.message.role === "user" &&
                (item.message.actor === "team_lead" ||
                  item.message.actor === "team_member"),
              kind:
                item.message.actor === "team_member" && !item.directDelivered
                  ? ("member-report" as const)
                  : undefined,
              truncated: item.message.truncated,
            },
          ] as const,
        ];
      }),
    );
  }, [isTeamMainChat, t, teamContext]);
  const workspaceFiles = useMemo(
    () =>
      workspaceFilesOf(
        isTeamMainChat
          ? teamContext.timeline.map((item) => item.message)
          : runtimeMessages,
      ),
    [isTeamMainChat, runtimeMessages, teamContext],
  );
  const latestMessageSequence =
    messagesQuery.data?.pages[0]?.latestSequence ?? 0;
  const runtimes = runtimesQuery.data?.resources ?? [];
  const readyRuntimes = selectableRuntimes(runtimes);
  const configuredRuntimeId =
    teamExecutionConfig?.runtimeId ||
    readyRuntimes.find((item) => item.isDefault)?.id ||
    readyRuntimes[0]?.id;
  const configuredRuntime = readyRuntimes.find(
    (item) => item.id === configuredRuntimeId,
  );
  const executionSelection = useExecutionSelection({
    runtime: configuredRuntime,
    value: {
      ...teamExecutionConfig,
      permissionLevel:
        detailQuery.data?.chat?.lastPermissionLevel ||
        teamExecutionConfig?.permissionLevel,
    },
    enabled: Boolean(configuredRuntime),
    defaults: true,
  });
  const historicalRuntime = runtimes.find(
    (item) => item.id === detailQuery.data?.chat?.lastRuntimeId,
  );
  const historicalRuntimeDisabled = false;
  const effectiveRuntimeID = executionSelection.selection.runtimeId;
  const selectedRuntime = executionSelection.runtime;
  const selectedModel = executionSelection.effectiveModel;
  const selectedModelOption = executionSelection.models.find(
    (item) => item.id === selectedModel,
  );
  const supportedPermissions = executionSelection.supportedPermissions;
  const selectedPermissionLevel = executionSelection.selection.permissionLevel;
  const selectedPermissionUnsupported = Boolean(
    selectedRuntime && !supportedPermissions.has(selectedPermissionLevel),
  );
  const defaultPermissionWasDowngraded = Boolean(
    selectedRuntime?.diagnostics?.some(
      (item) => item.code === "PERMISSION_DEFAULT_DOWNGRADED",
    ),
  );
  const availableReasoningOptions = executionSelection.reasoningOptions;
  const selectedReasoning = executionSelection.selection.reasoning;
  const updateExecutionSelection = (
    patch: Partial<TeamMemberExecutionConfig>,
  ) => {
    const targetWakerIds = effectiveTeamConfigTargetWakerId
      ? [effectiveTeamConfigTargetWakerId]
      : [];
    const updates = targetWakerIds.flatMap((wakerId) => {
      const member = teamContext.members.find((item) => item.id === wakerId);
      if (!member) return [];
      const current = teamContext.threadId
        ? member
        : (draftTeamMemberConfigs[wakerId] ?? member);
      const config: TeamMemberExecutionConfig = {
        accountId: patch.accountId ?? current.accountId,
        model: patch.model ?? current.model,
        permissionLevel: patch.permissionLevel ?? current.permissionLevel,
        reasoning: patch.reasoning ?? current.reasoning,
        runtimeId: patch.runtimeId ?? current.runtimeId,
      };
      return [{ ...config, wakerId }];
    });
    if (updates.length === 0) return;
    if (!teamContext.threadId) {
      setDraftTeamMemberConfigsByScope((current) => ({
        ...current,
        [workspaceScopeKey]: {
          ...(current[workspaceScopeKey] ?? {}),
          ...Object.fromEntries(
            updates.map(({ wakerId, ...config }) => [wakerId, config]),
          ),
        },
      }));
      return;
    }
    const operation = `team-config:${teamContext.threadId}`;
    const recovery = captureErrorNoticeRecovery(operation);
    void teamContext
      .onUpdateMemberConfigs(updates)
      .then(() => resolveErrorNoticeRecovery(recovery))
      .catch((error: unknown) => {
        reportChatError(
          error,
          `team:${teamContext.threadId}`,
          t("team.memberConfigUpdateFailed"),
          operation,
        );
      });
  };
  const explicitDraftSelection =
    draftWorkspaceSelections[draftWorkspacePreferenceKey];
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
    teamContext.workspaceDirectory ??
    detailQuery.data?.chat?.workspaceDirectory ??
    draftWorkingDirectory;
  const linkAdditionalWorkingDirectories =
    teamContext.additionalWorkingDirectories ??
    detailQuery.data?.chat?.additionalWorkingDirectories;
  const gitWorkspaceDirectories = resolveGitWorkspaceDirectories({
    additionalWorkingDirectories: linkAdditionalWorkingDirectories,
    selectedDraftProject,
    workspaceDirectory,
    managed: Boolean(activeChatId && !detailQuery.data?.chat?.projectName),
  });
  const terminalWorkingDirectory =
    teamContext.additionalWorkingDirectories?.[0] ??
    detailQuery.data?.chat?.additionalWorkingDirectories?.[0] ??
    "";
  const fileRoots = (() => {
    if (teamContext.workspaceDirectory) {
      return [
        teamContext.workspaceDirectory,
        ...(teamContext.additionalWorkingDirectories ?? []),
      ].filter(Boolean);
    }
    if (activeChatId && workspaceDirectory) return [workspaceDirectory];
    const project = selectedDraftProject;
    const projectRoots = (project?.sources ?? [])
      .filter((source) => source.available && source.path)
      .flatMap((source) => (source.path ? [source.path] : []));
    if (projectRoots.length > 0) return projectRoots;
    return workspaceDirectory ? [workspaceDirectory] : [];
  })();

  const refreshDetail = (includeChat = true) => {
    if (includeChat && activeChatId) {
      void queryClient.invalidateQueries({
        queryKey: getChatQueryKey({ path: { chatId: activeChatId } }),
      });
    }
    if (selectedWakerID) {
      void queryClient.invalidateQueries({
        queryKey: chatListKey(selectedWakerID),
      });
    }
  };
  const sendMemberMessage = useMutation(sendMessageMutation());
  const cancelMemberExecution = useMutation(cancelChatExecutionMutation());
  const resolveApproval = useMutation(resolveApprovalMutation());
  const resolveElicitation = useMutation(resolveElicitationMutation());
  const notifyApproval = (approval: PendingApproval) => {
    const approvalId = approval.approvalId;
    if (!approvalId) return;
    if (selectedWakerID) {
      void queryClient.invalidateQueries({
        queryKey: chatListKey(selectedWakerID),
      });
    }
  };
  const chatRuntime = useChatRuntime({
    conversationKey: currentChatKey,
    queueKey: runtimeQueueKey,
    queueActive: sceneActive,
    chatId: activeChatId,
    messageSource: isTeamMainChat
      ? teamContext.messageSource
      : chatMessageSource(queryClient, activeChatId, {
          messages: runtimeMessages,
          latestSequence: latestMessageSequence,
          ready: !activeChatId || Boolean(messagesQuery.data),
        }),
    activeExecutionId: isTeamMainChat
      ? teamContext.leadActiveExecutionId
      : detailQuery.data?.chat?.activeExecutionId,
    initialPendingApprovals: detailQuery.data?.chat?.pendingApprovals,
    initialPendingElicitations: detailQuery.data?.chat?.pendingElicitations,
    isLoading: isTeamMainChat
      ? Boolean(teamContext.threadDetailLoading || teamContext.timelineLoading)
      : Boolean(
          activeChatId && (detailQuery.isLoading || messagesQuery.isLoading),
        ),
    isSendDisabled: Boolean(
      teamContext.isStopping ||
      !selectedWakerID ||
      !effectiveRuntimeID ||
      selectedPermissionUnsupported ||
      (!activeChatId && !workspaceDirectory && !selectedDraftProject),
    ),
    serverManagedQueue: isTeamMainChat,
    messageVisibility: isTeamMainChat ? teamMainChatMessageVisible : undefined,
    attachmentOnlyText: t("attachmentOnlyMessage"),
    referenceOnlyText: t("codeReferenceOnlyMessage"),
    incompleteErrorText: t("executionIncomplete"),
    selection: {
      accountId: executionSelection.selection.accountId,
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

    prepareSubmission: (
      text,
      _targetChatId,
      attachmentPaths = [],
      selection,
    ) => {
      const codeReferences = (selection?.codeReferences ?? []).flatMap(
        ({ path, startLine, endLine, content }) => {
          if (!path) return [];
          if (!startLine && !endLine && !content) {
            return [{ path, startLine: 0, endLine: 0, content: "" }];
          }
          return startLine && endLine && content
            ? [{ path, startLine, endLine, content }]
            : [];
        },
      );
      const memberChatId = activeMemberTab?.chatId;
      if (memberChatId) {
        const body = {
          text,
          accountId:
            selection?.accountId ?? executionSelection.selection.accountId,
          runtimeId: selection?.runtimeId ?? "",
          model: selection?.model ?? "",
          reasoning: selection?.reasoning ?? "",
          permissionLevel: selection?.permissionLevel ?? "auto",
          attachments: attachmentPaths.map((sourcePath) => ({ sourcePath })),
          skillIds: selection?.skillIds ?? [],
          pluginIds: selection?.pluginIds ?? [],
          connectorIds: selection?.connectorIds ?? [],
          codeReferences,
        };
        const requestScope = `${workspaceScopeKey}:send:${memberChatId}`;
        const { clientRequestId: submissionId } = withStableCreateRequest(
          requestScope,
          body,
        );
        return {
          submissionId,
          confirmAccepted: () =>
            clearStableCreateRequest(requestScope, submissionId),
          accept: () =>
            sendMemberMessage.mutateAsync({
              path: { chatId: memberChatId },
              body: { ...body, submissionId },
            }),
        };
      }
      const body = {
        text,
        mentionWakerId: teamMentionWakerId || undefined,
        attachments: attachmentPaths.map((sourcePath) => ({ sourcePath })),
        skillIds: selection?.skillIds ?? [],
        pluginIds: selection?.pluginIds ?? [],
        connectorIds: selection?.connectorIds ?? [],
        codeReferences,
      };
      if (teamContext.threadId) {
        const requestScope = `${workspaceScopeKey}:send:${teamContext.threadId}`;
        const { clientRequestId: submissionId } = withStableCreateRequest(
          requestScope,
          body,
        );
        return {
          submissionId,
          confirmAccepted: () =>
            clearStableCreateRequest(requestScope, submissionId),
          accept: () =>
            teamContext
              .onSendMessage({ ...body, submissionId })
              .then((result) => {
                return {
                  execution: result.execution,
                };
              }),
        };
      }
      const createRequest = withStableCreateRequest(workspaceScopeKey, {
        ...body,
        memberConfigs: teamContext.members.map((member) => {
          const config = draftTeamMemberConfigs[member.id] ?? member;
          return {
            accountId: config.accountId,
            model: config.model,
            permissionLevel: config.permissionLevel,
            reasoning: config.reasoning,
            runtimeId: config.runtimeId,
            wakerId: member.id,
          };
        }),
        projectId: selectedDraftProject?.id || undefined,
        workingDirectory: selectedDraftProject ? undefined : workspaceDirectory,
      });
      return {
        submissionId: createRequest.clientRequestId,
        confirmAccepted: () =>
          clearStableCreateRequest(
            workspaceScopeKey,
            createRequest.clientRequestId,
          ),
        accept: () =>
          teamContext.onCreateThread(createRequest).then((result) => {
            const thread = result.thread;
            const leadMember = result.members?.find((member) => member.isLead);
            if (thread?.id) {
              createdTeamThreadIds.current.set(currentChatKey, thread.id);
            }
            const createdChat: Chat | undefined = thread?.leadChatId
              ? {
                  additionalWorkingDirectories:
                    thread?.additionalWorkingDirectories,
                  hiddenFromWakerHistory: true,
                  id: thread.leadChatId,
                  lastModel: leadMember?.model,
                  lastPermissionLevel: leadMember?.permissionLevel,
                  lastRuntimeId: leadMember?.runtimeId,
                  projectId: thread?.projectId,
                  projectName: thread?.projectName,
                  title: thread?.title,
                  wakerId: leadMember?.wakerId,
                  workspaceDirectory: thread?.workspaceDirectory,
                }
              : undefined;
            return {
              chat: createdChat,
              execution: result.leadExecution,
            };
          }),
      };
    },
    cancel: async () => {
      const invocation = chatInvocationGuard.capture(
        activeChatId,
        runtimeQueueKey,
      );
      if (!chatInvocationGuard.isCurrent(invocation)) return;
      if (activeMemberTab?.chatId && invocation.chatId) {
        await cancelMemberExecution.mutateAsync({
          path: { chatId: invocation.chatId },
        });
        return;
      }
      if (!teamContext.threadId) return;
      const operation = `team-stop:${teamContext.threadId}`;
      const recovery = captureErrorNoticeRecovery(operation);
      try {
        await teamContext.onStopWork();
        resolveErrorNoticeRecovery(recovery);
      } catch (error) {
        reportChatError(
          error,
          `team:${teamContext.threadId}`,
          t("team.stopWorkFailed"),
          operation,
        );
        throw error;
      }
    },
    onSettled: (result, invocation) => {
      if (!isTeamMainChat)
        refreshDetail(!commitAcceptedChat(queryClient, result.chat));
      if (!invocation.current) {
        createdTeamThreadIds.current.delete(invocation.key);
      }
    },
    onStarted: (result, invocation) => {
      const createdID = result.chat?.id;
      const createdThreadId = createdTeamThreadIds.current.get(invocation.key);
      if (createdID && createdThreadId) {
        markSkipComposerClearOnCreate();
        workspace.dispatch({
          type: "migrate",
          toScopeKey: `team:${teamContext.channelId}:${createdThreadId}`,
        });
        createdTeamThreadIds.current.delete(invocation.key);
        teamContext.onThreadCreated(createdThreadId);
      }
    },
    onTerminal: () => {
      if (!isTeamMainChat) refreshDetail();
    },
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
    onSettled: (targetChatId) => {
      void queryClient.invalidateQueries({
        queryKey: getChatQueryKey({ path: { chatId: targetChatId } }),
      });
      if (!isTeamMainChat && targetChatId !== activeChatId) refreshDetail();
    },
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
  const teamThreadHydrating = Boolean(
    isTeamMainChat && teamContext.threadDetailLoading,
  );
  const teamShellLoading =
    Boolean(teamThreadHydrating || teamContext.timelineLoading) &&
    !teamContext.loadError;
  const historyIsLoading = isTeamMainChat
    ? teamShellLoading && !chatRuntime.hasVisibleMessages
    : Boolean(
        routeTransitioning ||
        (activeChatId &&
          !messagesQuery.isSuccess &&
          !chatRuntime.hasVisibleMessages),
      );

  const pickDirectory = async () => {
    const selected = await selectDirectory();
    if (selected) {
      selectDraftWorkspace(draftWorkspacePreferenceKey, {
        type: "folder",
        path: selected,
      });
    }
  };
  const selectedModelName = selectedModelOption?.name ?? selectedModel;
  const teamMainChat = isTeamMainChat;
  const teamWorkState = teamMainChat ? teamContext.workState : undefined;
  const selectedTeamMember = activeMemberWakerId
    ? teamContext.members.find((member) => member.id === activeMemberWakerId)
    : undefined;
  const teamLeadMember = teamContext.members.find((member) => member.isLead);
  const wakerName =
    selectedTeamMember?.name ??
    teamLeadMember?.name ??
    selectedWaker?.name ??
    t("wakerFallback");
  const conversationAvatar =
    selectedTeamMember?.avatar ??
    teamLeadMember?.avatar ??
    selectedWaker?.avatar;
  const conversationWakerId = selectedWakerID;
  const workspaceLabel =
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
  const teamGreetingPhrases = useMemo(
    () => [
      t("team.emptyTitle"),
      t("team.greetingPhrases.goal"),
      t("team.greetingPhrases.start"),
      t("team.greetingPhrases.assign"),
      t("team.greetingPhrases.share"),
    ],
    [t],
  );
  const memberGreetingPhrases = useMemo(
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
      <TypedPhrases
        as="h1"
        phrases={
          selectedTeamMember ? memberGreetingPhrases : teamGreetingPhrases
        }
        className="type-page-title mt-6 text-center font-semibold"
      />
    </div>
  );
  const historyLoadingState = <ChatHistoryLoadingState />;
  const resolvedMessageEmptyState = historyIsLoading
    ? historyLoadingState
    : chatRuntime.hasVisibleMessages
      ? null
      : emptyState;
  const messageEmptyState = resolvedMessageEmptyState;
  const pinTeamEmptyState = Boolean(
    teamMainChat && !chatRuntime.hasVisibleMessages && !historyIsLoading,
  );

  const {
    composerSettings,
    composerEffects,
    renderContextStrip: renderComposerContextStrip,
  } = useChatComposerControls({
    activeChatId,
    availableReasoningOptions,
    selectedAccountId: executionSelection.selection.accountId,
    modelOptionsOverride: executionSelection.models,
    thinkingPending: executionSelection.thinkingQuery.isFetching,
    historicalRuntime,
    historicalRuntimeDisabled,
    onPickDirectory: pickDirectory,
    onSelectProject: (projectId) => {
      selectDraftWorkspace(draftWorkspacePreferenceKey, {
        type: "project",
        projectId,
      });
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
    onOpenWorkbench: () => setChatRightPanel("workspace"),
  });
  const teamTaskBoard = teamContext.renderTaskBoard();
  const teamPresentation = teamChatPresentation();
  const workingMembers = teamContext.members.filter(
    (member) =>
      member.chatId &&
      (member.activeExecutionId ||
        member.taskStatus === "in_progress" ||
        (member.isLead &&
          (teamContext.leadActiveExecutionId || teamContext.leadProcessing))),
  );
  const openWorkingMember = (memberId: string) => {
    const member = workingMembers.find((item) => item.id === memberId);
    if (!member?.chatId) return;
    if (member.isLead) {
      const mainTab = workspace.snapshot.tabs.find(
        (tab) => tab.kind === "chat",
      );
      if (mainTab)
        workspace.dispatch({ type: "select-tab", tabId: mainTab.id });
      return;
    }
    workspace.dispatch({
      type: "open-member",
      member: { chatId: member.chatId, label: member.name, wakerId: member.id },
    });
  };
  const stopTeamWork = () => {
    if (!teamContext.threadId || teamContext.isStopping) return;
    const operation = `team-stop:${teamContext.threadId}`;
    const recovery = captureErrorNoticeRecovery(operation);
    void teamContext
      .onStopWork()
      .then(() => resolveErrorNoticeRecovery(recovery))
      .catch((error: unknown) => {
        reportChatError(
          error,
          `team:${teamContext.threadId}`,
          t("team.stopWorkFailed"),
          operation,
        );
      });
  };
  const chatAdapter: ChatWorkspaceAdapter = {
    identity: {
      chatId: activeChatId,
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
    tabs: workspace.snapshot.tabs.map((tab) =>
      tab.kind === "chat"
        ? {
            ...tab,
            label:
              teamContext.members.find(
                (member) => member.id === teamContext.leadWakerId,
              )?.name ?? t("team.lead"),
          }
        : tab,
    ),
    thread: {
      resultSource: activeChatId
        ? {
            chatId: activeChatId,
            owner:
              isTeamMainChat && teamContext.threadId
                ? { kind: "team_thread", id: teamContext.threadId }
                : { kind: "chat", id: activeChatId },
          }
        : undefined,
      runtime: chatRuntime.runtime,
      wakerName,
      wakerId: conversationWakerId,
      wakerAvatar: conversationAvatar,
      messageAuthors: teamMessageAuthors,
      activityStatus:
        teamMainChat && teamContext.workProgress.phase !== "idle" ? (
          <TeamWorkStatus
            activeMembers={workingMembers}
            onOpenMember={openWorkingMember}
            progress={teamContext.workProgress}
          />
        ) : undefined,
      isWorkRunning: Boolean(teamMainChat && teamWorkState === "running"),
      isWorkStopping: teamContext.isStopping,
      onStopWork: stopTeamWork,
      emptyState: messageEmptyState,
      pinEmptyState: pinTeamEmptyState,
      isHistoryLoading: historyIsLoading,
      historyError: teamContext.loadError,
      composerSettings,
      composerEffects,
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
      composerMembers: activeMemberTab
        ? []
        : teamContext.members.map((member) => ({
            description: member.isLead ? t("team.lead") : t("team.member"),
            id: member.id,
            name: member.name,
          })),
      availableSkillIds: availableSkillIDs,
      availablePluginIds: pluginsQuery.data
        ? new Set(enabledPlugins.map((plugin) => plugin.id))
        : undefined,
      availableConnectorIds: availableConnectorIDs,
      selectedSkillIds: selectedSkillIDs,
      selectedConnectorIds: selectedConnectorIDs,
      onSkillSelectionChange: setSelectedSkillIDs,
      onConnectorSelectionChange: setSelectedConnectorIDs,
      onMemberMentionChange: (wakerId) => setTeamMentionWakerId(wakerId ?? ""),
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
        updateChatViewState({
          selectedCodeReferences: references,
        }),
      onOpenCodeReference: openCodeReference,
      onOpenFile: openAttachmentFile,
      onOpenFileChange: openExecutionFileChange,
      contextMetrics: chatRuntime.contextMetrics,
      showUsageMetrics: teamPresentation.showUsageMetrics,
      sendFailure: chatRuntime.sendFailure,
      errorScopes: [
        runtimeQueueKey,
        `chat:${activeChatId}`,
        `team:${teamContext.threadId}`,
      ],
      onRetryExecution: chatRuntime.retryExecution,
      hasEarlierMessages:
        sceneActive &&
        (teamMainChat
          ? Boolean(teamContext.hasEarlierTimelineMessages)
          : Boolean(messagesQuery.hasNextPage)),
      isLoadingEarlier: teamMainChat
        ? Boolean(teamContext.isLoadingEarlierTimelineMessages)
        : messagesQuery.isFetchingNextPage,
      onLoadEarlier: () =>
        teamMainChat
          ? (teamContext.onLoadEarlierTimelineMessages() ?? Promise.resolve())
          : messagesQuery
              .fetchNextPage({ cancelRefetch: false, throwOnError: true })
              .then(() => {}),
      warning: permissionWarning || undefined,
      requestedInteractionId: searchParams.get("request")?.split(":").at(-1),
      pendingApprovals: chatRuntime.pendingApprovals,
      approvalSubmissionState,
      onResolveApproval: (approvalId, optionId) =>
        void resolveApprovalChoice(approvalId, optionId),
      pendingElicitations: chatRuntime.pendingElicitations,
      elicitationSubmissionState,
      onResolveElicitation: (elicitationId, content, declined) =>
        void resolveElicitationChoice(elicitationId, content, declined),
    },
  };

  return (
    <ChatWorkspaceSurface
      activeRightPanel={chatRightPanel}
      adapter={chatAdapter}
      onAddCodeReference={addCodeReference}
      onDismissRightPanel={() => setChatRightPanel(undefined)}
      onOpenRightPanel={() => setChatRightPanel("workspace")}
      openedCodeReference={openedCodeReference}
      gitRoots={gitWorkspaceDirectories}
      renderTeamMembers={renderTeamMembers}
      showActivityIndicator={!teamMainChat}
      taskBoard={teamTaskBoard}
      workspace={workspace}
      fileWorkspaceProps={fileWorkspaceProps}
      changesProps={changesProps}
    />
  );
}
