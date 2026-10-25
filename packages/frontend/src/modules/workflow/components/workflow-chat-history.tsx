import { useQueries } from "@tanstack/react-query";
import { Folder } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ChatGroupHeader,
  ChatHistoryNavigationRow,
  ChatHistoryPanelSurface,
  ChatHistoryPopover,
  ChatTaskDialog,
  getChatOptions,
} from "@/modules/chat";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowCreationSession as ChatSession } from "@/shared/api/generated/types.gen";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { WorkflowQueryError } from "./query-feedback";

/** 工作流拥有会话关系；列表行、浮层与修改/删除确认复用普通聊天。 */
export function WorkflowChatHistory({
  sessions,
  selectedId,
  onSelect,
  onNew,
  onDeleted,
  onRefresh,
}: {
  sessions: ChatSession[];
  selectedId?: string;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDeleted: (id: string, chatId: string) => void;
  onRefresh: () => Promise<void>;
}) {
  const { t } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const [collapsed, setCollapsed] = useState(false);
  const [action, setAction] = useState<{
    mode: "rename" | "delete";
    sessionId: string;
    chatId: string;
    title: string;
  }>();
  const chats = useQueries({
    queries: sessions.map((session) => ({
      ...getChatOptions({ path: { chatId: session.chatId ?? "pending" } }),
      enabled: active && Boolean(session.chatId),
    })),
  });
  const items = sessions
    .flatMap((session, index) =>
      session.id && session.chatId ? [{ session, query: chats[index] }] : [],
    )
    .sort(
      (a, b) =>
        (b.query?.data?.chat?.updatedAt ?? b.session.updatedAt ?? 0) -
        (a.query?.data?.chat?.updatedAt ?? a.session.updatedAt ?? 0),
    );
  return (
    <>
      <ChatHistoryPopover>
        {(close) => (
          <ChatHistoryPanelSurface
            onNew={() => {
              close();
              onNew();
            }}
          >
            <ChatGroupHeader
              icon={Folder}
              label={t("defaultDirectory")}
              collapsed={collapsed}
              onToggle={() => setCollapsed((value) => !value)}
            />
            {!collapsed ? (
              <div className="ml-6 grid gap-1">
                {items.map(({ session, query }) => {
                  const chat = query?.data?.chat;
                  const title = chat?.title || t("untitledChat");
                  const waiting = Boolean(
                    chat?.pendingApprovals?.length ||
                    chat?.pendingElicitations?.length,
                  );
                  return (
                    <div key={session.id}>
                      <ChatHistoryNavigationRow
                        active={session.id === selectedId}
                        title={title}
                        status={
                          waiting
                            ? "waiting"
                            : chat?.activeExecutionId
                              ? "running"
                              : "idle"
                        }
                        onOpen={() => {
                          close();
                          onSelect(session.id ?? "");
                        }}
                        onAction={(mode) =>
                          setAction({
                            mode,
                            sessionId: session.id ?? "",
                            chatId: session.chatId ?? "",
                            title,
                          })
                        }
                      />
                      {query?.isError ? (
                        <WorkflowQueryError
                          error={query.error}
                          retrying={query.isFetching}
                          onRetry={() => void query.refetch()}
                        />
                      ) : null}
                    </div>
                  );
                })}
                {!items.length ? (
                  <p className="text-content-secondary type-body p-3">
                    {t("noChats")}
                  </p>
                ) : null}
              </div>
            ) : null}
          </ChatHistoryPanelSurface>
        )}
      </ChatHistoryPopover>
      {action ? (
        <ChatTaskDialog
          key={`${action.chatId}:${action.mode}`}
          chatId={action.chatId}
          mode={action.mode}
          title={action.title}
          open={active}
          onOpenChange={(open) => {
            if (!open) setAction(undefined);
          }}
          onChanged={onRefresh}
          onDeleted={() => onDeleted(action.sessionId, action.chatId)}
        />
      ) : null}
    </>
  );
}
