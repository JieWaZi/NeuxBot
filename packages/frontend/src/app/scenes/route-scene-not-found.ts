import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useCallback, useSyncExternalStore } from "react";

import { ApiError } from "@/shared/api/errors";
import {
  getChannelQueryKey,
  getChannelThreadQueryKey,
  getChatQueryKey,
  getGlobalAutomaticTaskQueryKey,
  getProjectQueryKey,
  getWakerConnectorQueryKey,
  getWakerQueryKey,
  getWakerSkillQueryKey,
  getWorkflowQueryKey,
  getWorkflowRunQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { AppSceneTab } from "@/shared/workspace/app-scene-model";

/** 只匹配页面自身及所属身份，关联资源或弹窗查询失败不使整个页面失效。 */
function routeSceneQueryKeys(href: string): QueryKey[] {
  const [pathname = "", search] = href.split("?");
  const segments = pathname.split("/").slice(1);
  const [kind, id, section, resourceId] = segments;
  if (!id) return [];
  if (kind === "chats") return [getChatQueryKey({ path: { chatId: id } })];
  if (kind === "automatic-tasks")
    return [getGlobalAutomaticTaskQueryKey({ path: { taskId: id } })];
  if (kind === "workflows") {
    if (id === "new") return [];
    const keys: QueryKey[] = [
      getWorkflowQueryKey({
        path: { id },
        query: {
          revisionId: new URLSearchParams(search).get("revision") || undefined,
        },
      }),
    ];
    if (section === "runs" && resourceId)
      keys.push(getWorkflowRunQueryKey({ path: { id, runId: resourceId } }));
    return keys;
  }
  if (kind === "teams") {
    const keys: QueryKey[] = [getChannelQueryKey({ path: { channelId: id } })];
    if (section === "threads" && resourceId)
      keys.push(
        getChannelThreadQueryKey({
          path: { channelId: id, threadId: resourceId },
        }),
      );
    return keys;
  }
  if (kind !== "wakers") return [];
  const keys: QueryKey[] = [getWakerQueryKey({ path: { wakerId: id } })];
  if (resourceId) {
    if (section === "projects")
      keys.push(
        getProjectQueryKey({ path: { wakerId: id, projectId: resourceId } }),
      );
    if (section === "skills")
      keys.push(
        getWakerSkillQueryKey({ path: { wakerId: id, skillId: resourceId } }),
      );
    if (section === "connectors")
      keys.push(
        getWakerConnectorQueryKey({
          path: { wakerId: id, connectorId: resourceId },
        }),
      );
  }
  return keys;
}

/** 标签状态直接读取查询缓存，成功刷新或切换资源后自动恢复，不另存服务端事实。 */
export function useRouteSceneNotFound(tabs: readonly AppSceneTab[]) {
  const cache = useQueryClient().getQueryCache();
  const subscribe = useCallback(
    (listener: () => void) => cache.subscribe(listener),
    [cache],
  );
  const getSnapshot = useCallback(
    () =>
      tabs
        .map((tab) => {
          const missing =
            tab.kind === "route" &&
            tab.href &&
            routeSceneQueryKeys(tab.href).some((queryKey) =>
              cache
                .findAll({ queryKey })
                .some(
                  (query) =>
                    query.state.error instanceof ApiError &&
                    query.state.error.status === 404,
                ),
            );
          return missing ? "1" : "0";
        })
        .join(""),
    [cache, tabs],
  );
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
