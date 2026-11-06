import type {
  AppSceneTab,
  ChatSceneOwner,
} from "@/shared/workspace/app-scene-model";

export type EntityHistory = { entries: ChatSceneOwner[]; index: number };
export function sameChatOwner(
  left: ChatSceneOwner | undefined,
  right: ChatSceneOwner | undefined,
) {
  return left?.kind === right?.kind && left?.id === right?.id;
}

/** 历史只保存聊天实体，不保存 Router、文件或覆盖页的访问。 */
export function recordEntityHistory(
  history: EntityHistory,
  owner: ChatSceneOwner | undefined,
  target?: number,
): EntityHistory {
  if (!owner) return history;
  if (target !== undefined && sameChatOwner(history.entries[target], owner)) {
    return target === history.index ? history : { ...history, index: target };
  }
  if (sameChatOwner(history.entries[history.index], owner)) return history;
  return {
    entries: [...history.entries.slice(0, history.index + 1), owner],
    index: history.index + 1,
  };
}

export function nearestEntityHistory(
  history: EntityHistory,
  offset: -1 | 1,
  available: (owner: ChatSceneOwner) => boolean,
): number | undefined {
  for (
    let index = history.index + offset;
    index >= 0 && index < history.entries.length;
    index += offset
  ) {
    const entry = history.entries[index];
    if (entry && available(entry)) return index;
  }
  return undefined;
}

/** Query 贡献优先于 URL 提示；尚未解析的裸 Chat 深链不猜测身份。 */
export function chatOwnerForScene(
  tab: AppSceneTab | undefined,
): ChatSceneOwner | undefined {
  if (tab?.kind !== "route" || !tab.href) return undefined;
  const [pathname = "", search = ""] = tab.href.split("?");
  const team = /^\/teams\/([^/]+)(?:\/threads\/[^/]+)?$/.exec(pathname);
  if (team?.[1]) return { kind: "team", id: team[1] };
  if (
    pathname !== "/" &&
    pathname !== "/chat" &&
    !/^\/chats\/[^/]+$/.test(pathname)
  )
    return undefined;
  if (tab.chatOwner) return tab.chatOwner;
  const id = new URLSearchParams(search).get("wakerId")?.trim();
  return id ? { kind: "waker", id } : undefined;
}

export function chatScenesForOwner(
  tabs: readonly AppSceneTab[],
  owner: ChatSceneOwner | undefined,
) {
  return owner
    ? tabs.filter((tab) => sameChatOwner(chatOwnerForScene(tab), owner))
    : [];
}

/** 未提交会话复用已有 Scene，保留输入与工作区；已落库会话不按标题判断。 */
export function unsentChatSceneForOwner(
  tabs: readonly AppSceneTab[],
  owner: ChatSceneOwner | undefined,
): AppSceneTab | undefined {
  return chatScenesForOwner(tabs, owner)
    .filter(
      (tab) =>
        !tab.notFound &&
        (tab.id.startsWith("chat-draft:") ||
          tab.id.startsWith("chat-entry:") ||
          tab.id.startsWith("team-draft:")),
    )
    .sort((left, right) => right.lastUsed - left.lastUsed)[0];
}
