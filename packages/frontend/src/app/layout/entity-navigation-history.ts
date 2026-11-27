import type { ConversationDestination } from "@/shared/lib/main-conversation-navigation";
import { mainConversationOwner } from "../scenes/main-conversation-route";
import type {
  AppSceneTab,
  ChatSceneOwner,
} from "@/shared/workspace/app-scene-model";

export type EntityHistory = {
  entries: ConversationDestination[];
  index: number;
};
export function sameChatOwner(
  left: ChatSceneOwner | undefined,
  right: ChatSceneOwner | undefined,
) {
  return left?.kind === right?.kind && left?.id === right?.id;
}

/** 记录聊天主体与明确打开的工作来源。普通页签、文件和资料页沿用主体历史。 */
export function recordEntityHistory(
  history: EntityHistory,
  visit: ConversationDestination | undefined,
  target?: number,
): EntityHistory {
  if (!visit) return history;
  const sameVisit = (entry: ConversationDestination | undefined) =>
    sameChatOwner(entry, visit) && entry?.workChatId === visit.workChatId;
  if (target !== undefined && sameVisit(history.entries[target]))
    return target === history.index ? history : { ...history, index: target };
  const current = history.entries[history.index];
  if (sameVisit(current)) return history;
  const entries = history.entries.slice(0, history.index + 1);
  if (visit.workChatId && !sameChatOwner(current, visit))
    entries.push({ kind: visit.kind, id: visit.id });
  entries.push(visit);
  return { entries, index: entries.length - 1 };
}

export function nearestEntityHistory(
  history: EntityHistory,
  offset: -1 | 1,
  available: (owner: ConversationDestination) => boolean,
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
  const main = mainConversationOwner(pathname);
  if (main) return main;
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
    ? tabs.filter(
        (tab) =>
          !tab.id.startsWith("chat-entry:") &&
          sameChatOwner(chatOwnerForScene(tab), owner),
      )
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
        (tab.id.startsWith("chat-draft:") || tab.id.startsWith("team-draft:")),
    )
    .sort((left, right) => right.lastUsed - left.lastUsed)[0];
}
