import type { SendWorkflowCreationMessageData } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { X } from "lucide-react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
  RetainedChatPageContent,
  type ChatWorkspaceEntry,
} from "@/modules/chat";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";
import { useTranslation } from "react-i18next";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowCreationSession as ChatSession } from "@/shared/api/generated/types.gen";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import {
  sendWorkflowCreationMessageMutation,
  createWorkflowMutation,
  getWorkflowRunOptions,
} from "../api/workflow-queries";
import type { ExecutionDefaults } from "../model/types";

type CanvasReference = NonNullable<
  SendWorkflowCreationMessageData["body"]["canvasReferences"]
>[number] & { kind: "phase" | "node"; id: string; revisionId: string };

/** 工作流只负责 scoped 发送；对话界面完整复用普通 Chat。 */
export function WorkflowChatPane({
  id,
  draftId,
  onCreated,
  session,
  closedChatIds,
  conversationKey,
  onSession,
  mainContent,
  history,
  onNewChat,
  wakerId,
  draft,
  optimizeRunId,
  canvasReferences = [],
  canvasReferenceLabels = {},
  onRemoveCanvasReference,
  onCanvasReferencesSent,
  emptyState,
  isHistoryLoading = false,
  onConfigurationChange,
  onRunningChange,
  onFirstMessageSubmitted,
}: {
  id: string;
  draftId?: string;
  onCreated?: (id: string) => void;
  session?: ChatSession;
  closedChatIds?: readonly string[];
  conversationKey?: string;
  onSession?: (sessionId: string) => void;
  mainContent: ChatWorkspaceEntry["mainContent"];
  history?: ReactNode;
  onNewChat: () => void;
  wakerId?: string;
  draft?: string;
  optimizeRunId?: string;
  canvasReferences?: CanvasReference[];
  canvasReferenceLabels?: Record<string, string>;
  onRemoveCanvasReference?: (target: CanvasReference) => void;
  onCanvasReferencesSent?: () => void;
  emptyState?: ChatWorkspaceEntry["emptyState"];
  isHistoryLoading?: boolean;
  onConfigurationChange?: (defaults: ExecutionDefaults | undefined) => void;
  onRunningChange?: (running: boolean) => void;
  onFirstMessageSubmitted?: () => void;
}) {
  const { t } = useTranslation("workflow");
  const canvasReferenceKey = JSON.stringify(canvasReferences);
  const visibleCanvasReferenceKey = useRef(canvasReferenceKey);
  useLayoutEffect(() => {
    visibleCanvasReferenceKey.current = canvasReferenceKey;
  }, [canvasReferenceKey]);
  const send = useMutation(
    withLocalErrorMutation(sendWorkflowCreationMessageMutation()),
  );
  const create = useMutation(withLocalErrorMutation(createWorkflowMutation()));
  const definitionRequest = useRef(draftId || crypto.randomUUID());
  const createdDefinition = useRef<string | undefined>(undefined);
  const requests = useRef(new Map<string, { signature: string; id: string }>());
  const acceptedSessions = useRef(new Map<string, string>());
  const [draftScopeId] = useState(() => draftId ?? id);
  const [firstMessageSubmitted, setFirstMessageSubmitted] = useState(false);
  const [draftBinding, setDraftBinding] = useState<{
    scopeKey: string;
    chatId: string;
  }>();
  const active = useRouteSceneActive();
  const reference = useQuery({
    ...getWorkflowRunOptions({ path: { id, runId: optimizeRunId ?? "" } }),
    enabled: active && Boolean(optimizeRunId) && id !== "new",
  });
  const sourceRun = reference.data?.run;
  const runReference =
    optimizeRunId && sourceRun?.id === optimizeRunId
      ? {
          runId: optimizeRunId,
          revisionId: sourceRun.revisionId,
        }
      : undefined;
  const scopeKey = session?.chatId
    ? `chat:${session.chatId}`
    : `draft:workflow:${draftScopeId}:${conversationKey ?? "default"}`;
  const currentScope = useRef({
    key: scopeKey,
    active,
    chatId: session?.chatId,
  });
  const pendingAccepted = useRef<
    { scopeKey: string; chatId: string; sessionId?: string } | undefined
  >(undefined);
  useLayoutEffect(() => {
    currentScope.current = { key: scopeKey, active, chatId: session?.chatId };
    const pending = pendingAccepted.current;
    if (!active || pending?.scopeKey !== scopeKey) return;
    pendingAccepted.current = undefined;
    setDraftBinding({ scopeKey, chatId: pending.chatId });
    if (pending.sessionId && pending.chatId !== session?.chatId)
      onSession?.(pending.sessionId);
    if (id === "new" && createdDefinition.current)
      onCreated?.(createdDefinition.current);
  }, [active, scopeKey, session?.chatId, id, onSession, onCreated]);

  return (
    <RetainedChatPageContent
      draftBinding={draftBinding}
      closedChatIds={closedChatIds}
      chatId={session?.chatId || undefined}
      entry={{
        scopeKey,
        wakerId: wakerId || PERSONAL_ASSISTANT_WAKER_ID,
        defaults: session?.defaults,
        directoryLabel: t("defaultDirectory"),
        mainContent,
        canvasInput: id === "new" && !firstMessageSubmitted,
        showSummary: false,
        showFileChanges: false,
        history,
        onNewChat,
        onAccepted: (result) => {
          const chatId = result.chat?.id;
          if (!chatId) return;
          const sessionId = acceptedSessions.current.get(chatId);
          if (scopeKey !== currentScope.current.key) return;
          if (!currentScope.current.active) {
            pendingAccepted.current = { scopeKey, chatId, sessionId };
            return;
          }
          setDraftBinding({ scopeKey, chatId });
          if (sessionId && chatId !== currentScope.current.chatId)
            onSession?.(sessionId);
          if (id === "new" && createdDefinition.current)
            onCreated?.(createdDefinition.current);
        },
        draft,
        isSendDisabled: Boolean(optimizeRunId && !runReference),
        composerContext: (
          <>
            {canvasReferences.length ? (
              <div className="flex flex-wrap gap-2">
                {canvasReferences.map((reference) => (
                  <span
                    key={`${reference.kind}:${reference.id}`}
                    className="bg-accent-surface type-caption rounded-control inline-flex max-w-full items-center gap-1 pl-2"
                  >
                    <span className="min-w-0 truncate">
                      {canvasReferenceLabels[
                        `${reference.kind}:${reference.id}`
                      ] ?? reference.id}
                    </span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t("removeReference", {
                        label:
                          canvasReferenceLabels[
                            `${reference.kind}:${reference.id}`
                          ] ?? reference.id,
                      })}
                      onClick={() => onRemoveCanvasReference?.(reference)}
                    >
                      <X />
                    </Button>
                  </span>
                ))}
              </div>
            ) : null}
          </>
        ),
        emptyState,
        isHistoryLoading,
        onConfigurationChange,
        onRunningChange,
        prepareSubmission: (message, targetChatId) => {
          const {
            text,
            runtimeId,
            accountId,
            model,
            reasoning,
            permissionLevel,
            ...input
          } = message;
          const body = {
            ...input,
            sessionId: targetChatId
              ? (acceptedSessions.current.get(targetChatId) ??
                (session?.chatId === targetChatId ? session.id : undefined))
              : session?.id,
            content: text,
            runReference,
            canvasReferences: canvasReferences.length
              ? canvasReferences
              : undefined,
            defaults: {
              runtimeId,
              accountId,
              model,
              reasoning,
              permissionLevel,
            },
          };
          const signature = JSON.stringify(body);
          const previousRequest = requests.current.get(scopeKey);
          const request =
            previousRequest?.signature === signature
              ? previousRequest
              : { signature, id: crypto.randomUUID() };
          requests.current.set(scopeKey, request);
          let confirmed = false;
          return {
            submissionId: request.id,
            confirmAccepted: () => {
              if (confirmed) return;
              confirmed = true;
              if (visibleCanvasReferenceKey.current === canvasReferenceKey)
                onCanvasReferencesSent?.();
              if (requests.current.get(scopeKey) === request)
                requests.current.delete(scopeKey);
            },
            accept: async () => {
              setFirstMessageSubmitted(true);
              onFirstMessageSubmitted?.();
              let definitionId = id;
              if (id === "new") {
                if (!createdDefinition.current) {
                  const definition = await create.mutateAsync({
                    body: { requestId: definitionRequest.current },
                  });
                  if (!definition.id) throw new Error("workflow_not_created");
                  createdDefinition.current = definition.id;
                }
                definitionId = createdDefinition.current;
              }
              const accepted = await send.mutateAsync({
                path: { id: definitionId },
                body: { ...body, requestId: request.id },
              });
              if (!accepted.chatId || !accepted.acceptedExecutionId)
                throw new Error("workflow_conversation_not_accepted");
              if (accepted.id)
                acceptedSessions.current.set(accepted.chatId, accepted.id);
              // 交付已接纳的权威快照，由共享 Chat 查询按持久序号接纳。
              return {
                chat: accepted.chat,
                execution: accepted.execution,
              };
            },
          };
        },
      }}
    />
  );
}
