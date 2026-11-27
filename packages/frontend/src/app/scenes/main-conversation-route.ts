import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";
import type {
  AppSceneTab,
  ChatSceneOwner,
} from "@/shared/workspace/app-scene-model";
import type { RouteSceneLocation } from "./app-route-scene-model";

export const mainConversationPath = "chat/main/:subjectType/:subjectId";

export { mainConversationHref };

export function mainConversationOwner(
  pathname: string,
): ChatSceneOwner | undefined {
  const match = /^\/chat\/main\/(waker|team)\/([^/]+)$/.exec(pathname);
  const kind = match?.[1];
  if ((kind === "waker" || kind === "team") && match?.[2]) {
    try {
      return { kind, id: decodeURIComponent(match[2]) };
    } catch {
      return undefined;
    }
  }
}

/** 主体维持同一页签与挂载身份，切到普通历史后仍保留主会话草稿。 */
export function mainConversationScene(
  location: RouteSceneLocation,
  label: string,
): Omit<AppSceneTab, "lastUsed"> | undefined {
  const owner = mainConversationOwner(location.pathname);
  if (owner) return mainConversationTab(owner, label, location);
}

export function mainConversationTab(
  owner: ChatSceneOwner,
  label: string,
  location: RouteSceneLocation = {
    pathname: mainConversationHref(owner),
    search: "",
  },
): Omit<AppSceneTab, "lastUsed"> {
  const id = `main:${owner.kind}:${owner.id}`;
  return {
    id,
    kind: "route",
    mountKey: id,
    href: location.pathname + location.search,
    navigationState: location.state,
    chatOwner: owner,
    label,
    closeable: true,
    background: "preserve",
  };
}
