import { useInfiniteQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import type { GetMainConversationResponse } from "@/shared/api/generated/types.gen";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";
import {
  authorizedSourceChat,
  sourceHistoryOptions,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import type { ChatRightPanel } from "../components/chat-right-panel-host";
import type { ChatWorkspaceAdapter } from "../components/chat-workspace-surface";
import { useChatWorkspaceBindings } from "../components/use-chat-workspace-bindings";
import { workspaceFilesOf } from "../model/chat-workspace-files";
import { conversationKeyOf } from "../model/conversation-identity";
import { resolveGitWorkspaceDirectories } from "../model/git-workspace-directories";
import { useChatViewState } from "../model/use-chat-view-state";
import type { ChatOutletContext } from "./chat-page-shared";

/** 主会话共享普通聊天的标题栏与右侧工作台，工作目录只绑定经核验的来源。 */
export function useMainConversationWorkspace({
  subject,
  source,
  active,
  messages,
  activeWork,
}: {
  subject: ConversationSubject;
  source?: string;
  active: boolean;
  messages: readonly PublicMessage[];
  activeWork: GetMainConversationResponse["activeWork"];
}) {
  const { t } = useTranslation("chat");
  const outlet = useOutletContext<ChatOutletContext | null>();
  const [localPanel, setLocalPanel] = useState<ChatRightPanel>();
  const rightPanel = outlet ? outlet.chatRightPanel : localPanel;
  const setRightPanel = outlet?.setChatRightPanel ?? setLocalPanel;
  const openWorkbench = useCallback(
    () => setRightPanel("workspace"),
    [setRightPanel],
  );
  const dismissWorkbench = useCallback(
    () => setRightPanel(undefined),
    [setRightPanel],
  );

  // 来源视图负责请求，工作台消费同一份已通过主体核验的快照。
  const workHistory = useInfiniteQuery({
    ...sourceHistoryOptions(subject, source ?? ""),
    enabled: false,
  });
  const sourceChat = source
    ? authorizedSourceChat(
        source,
        workHistory.data?.pages[0]?.chat,
        workHistory.error,
      )
    : undefined;
  const mainScopeKey = `main:${subject.subjectType}:${subject.subjectId}`;
  const scopeKey = sourceChat?.id
    ? `source:${sourceChat.id}`
    : source
      ? `${mainScopeKey}:source-pending`
      : mainScopeKey;
  const workspace = useSceneWorkspace(scopeKey);
  const { state, update } = useChatViewState(scopeKey);
  const workspaceDirectory = sourceChat?.workspaceDirectory ?? "";
  const additionalWorkingDirectories = sourceChat?.additionalWorkingDirectories;
  const gitRoots = resolveGitWorkspaceDirectories({
    additionalWorkingDirectories,
    workspaceDirectory,
    managed: Boolean(sourceChat?.id && !sourceChat.projectName),
  });
  const workspaceFiles = useMemo(() => {
    if (sourceChat)
      return workspaceFilesOf(
        workHistory.data?.pages
          .toReversed()
          .flatMap((page) => page.resources ?? []) ?? [],
      );
    if (source) return [];
    return workspaceFilesOf(
      messages.filter((row) => row.role === "user" && row.sourceChatId),
    );
  }, [messages, source, sourceChat, workHistory.data]);
  const bindings = useChatWorkspaceBindings({
    activeChatId: sourceChat?.id,
    additionalWorkingDirectories,
    fileSource: sourceChat ? state.fileSource : "chat",
    query: state.query,
    updateViewState: update,
    workspaceDirectory,
    workspaceFiles,
    workspacePanelOpen: active && rightPanel === "workspace",
    workspaceScopeKey: scopeKey,
    onOpenWorkbench: openWorkbench,
  });
  const shownWork = source
    ? activeWork?.filter((work) => work.workChatId === sourceChat?.id)
    : activeWork;
  const snapshot: ChatWorkspaceAdapter["snapshot"] = {
    isRunning: Boolean(shownWork?.length || sourceChat?.activeExecutionId),
    pendingApprovalCount:
      shownWork?.filter((work) => work.waitingApproval).length ?? 0,
    pendingElicitationCount:
      shownWork?.filter((work) => work.waitingElicitation).length ?? 0,
    streamState: "closed",
  };
  return {
    bindings,
    codeReferenceRequest: state.codeReferenceRequest,
    surface: {
      active,
      chatHistory: null,
      activeRightPanel: rightPanel,
      onOpenRightPanel: openWorkbench,
      onDismissRightPanel: dismissWorkbench,
      onAddCodeReference: sourceChat ? bindings.addCodeReference : undefined,
      defaultFileSource: source ? ("project" as const) : ("chat" as const),
      allowSideChat: false,
      gitRoots,
      workspace,
      fileWorkspaceProps: sourceChat
        ? bindings.fileWorkspaceProps
        : {
            ...bindings.fileWorkspaceProps,
            onDeleteChatFile: undefined,
          },
      changesProps: sourceChat ? bindings.changesProps : undefined,
    },
    adapter: {
      tabs: [],
      identity: {
        chatId: sourceChat?.id,
        key: conversationKeyOf(scopeKey, sourceChat?.id),
        scopeKey,
        workspaceDirectory,
        terminalWorkingDirectory: sourceChat?.id
          ? (additionalWorkingDirectories?.[0] ?? workspaceDirectory)
          : undefined,
      },
      snapshot,
      thread: {
        wakerName: t("mainConversation"),
        linkAdditionalWorkingDirectories: additionalWorkingDirectories,
        onOpenFile: bindings.openAttachmentFile,
        onOpenLink: bindings.openBrowserLink,
      },
    },
  };
}
