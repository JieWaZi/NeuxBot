import { useState, type ComponentProps } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ChatPane, FloatingChat } from "@/modules/chat";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";
import type { ResourceRef } from "@/shared/api/knowledge";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { knowledgeKeys } from "./knowledge-queries";
import { resourceKey } from "./knowledge-transfer";
import type { WikiPage } from "./knowledge-wiki-queries";
import { createWikiMaintenanceSender } from "./knowledge-maintenance-send";
import { useWikiCopy } from "./use-wiki-copy";

export type WikiMaintenanceSession = {
  resource: ResourceRef;
  page: WikiPage;
  draft?: string;
  openRequest: number;
};

function createSession(
  resource: ResourceRef,
  page: WikiPage,
  openRequest: number,
) {
  const target = { resource: { ...resource }, page: { ...page } };
  return {
    target,
    openRequest,
    sender: createWikiMaintenanceSender({
      resource: target.resource,
      pageId: target.page.id ?? "",
      expectedVersion: target.page.version ?? 0,
    }),
  };
}
/** 入口装配随聊天按需加载；同一页面复用组件身份。 */
export function renderWikiMaintenance(
  resource: ResourceRef,
  page: WikiPage,
  draft?: string,
) {
  return (
    <KnowledgeWikiMaintenanceChat
      key={`${resourceKey(resource)}:${page.id}`}
      resource={resource}
      page={page}
      draft={draft}
      openRequest={Date.now()}
    />
  );
}
/** 最小化只隐藏浮窗，消息、输入、执行与审批保持同一普通 Chat 身份。 */
export function KnowledgeWikiMaintenanceChat({
  resource,
  page,
  draft,
  openRequest,
}: WikiMaintenanceSession) {
  const t = useWikiCopy();
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [session, setSession] = useState(() =>
    createSession(resource, page, openRequest),
  );
  if (session.openRequest !== openRequest) {
    const refresh =
      resourceKey(resource) === resourceKey(session.target.resource) &&
      page.id === session.target.page.id &&
      (page.version ?? 0) > 0 &&
      session.sender.canRefreshRejectedVersion();
    setSession(
      refresh
        ? createSession(resource, page, openRequest)
        : { ...session, openRequest },
    );
  }
  const { target, sender } = session;
  const [initialDraft] = useState(draft);
  const [chatId, setChatId] = useState<string>();
  const [snapshot, setSnapshot] = useState<
    ComponentProps<typeof FloatingChat>["snapshot"]
  >({
    isRunning: false,
    pendingApprovalCount: 0,
    pendingElicitationCount: 0,
    streamState: "open",
  });
  const [scopeKey] = useState(
    () =>
      `knowledge-maintenance:${resourceKey(target.resource)}:${target.page.id}:${crypto.randomUUID()}`,
  );
  const refreshWiki = () => {
    void client.invalidateQueries({
      queryKey: [...knowledgeKeys.resource(target.resource), "wiki"],
    });
  };
  return (
    // 此入口只使用 Portal 浮窗；普通宿主退出布局，避免占满知识 Scene 的高度。
    <div hidden>
      <FloatingChat
        chatId={chatId}
        scopeKey={scopeKey}
        fullView={active}
        openRequest={openRequest}
        snapshot={snapshot}
        title={`${t("aiRevise")} · ${target.page.title ?? ""}`}
      >
        <div className="flex h-full min-h-0 flex-col">
          <p className="type-ui text-content-muted px-4 py-3">
            {t("maintenanceContext", {
              title: target.page.title ?? "",
              version: target.page.version ?? 0,
            })}
          </p>
          <p className="type-ui text-content-muted px-4 pb-3">
            {t("maintenanceContinue")}
          </p>
          <ChatPane
            scopeKey={scopeKey}
            chatId={chatId}
            wakerId={PERSONAL_ASSISTANT_WAKER_ID}
            title={t("aiRevise")}
            allowExecutionRetry={false}
            draft={initialDraft ?? t("maintenanceDraft")}
            onSend={async (text, attachments, selection) => {
              try {
                return await sender.send(text, attachments, selection);
              } catch (cause) {
                if (sender.canRefreshRejectedVersion()) refreshWiki();
                throw cause;
              }
            }}
            onAccepted={(result) => setChatId(result.chat?.id)}
            onRefresh={refreshWiki}
            onSnapshotChange={setSnapshot}
          />
        </div>
      </FloatingChat>
    </div>
  );
}
