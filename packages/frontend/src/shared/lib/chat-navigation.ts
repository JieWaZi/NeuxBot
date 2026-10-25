export const chatLastWakerStorageKey = "ally.chat.last-waker-id";
export const chatLastActiveStorageKey = "ally.chat.last-active";

export type ChatLastActive = {
  chatId: string;
  wakerId: string;
};

type ChatNavigationListener = () => void;

let navigationSnapshotRaw: string | null | undefined;
let navigationSnapshot: ChatLastActive | undefined;
const navigationListeners = new Set<ChatNavigationListener>();

function readStoredString(key: string) {
  try {
    return typeof window.localStorage?.getItem === "function"
      ? window.localStorage.getItem(key)
      : null;
  } catch {
    return null;
  }
}

function writeStoredString(key: string, value: string) {
  try {
    if (typeof window.localStorage?.setItem === "function") {
      window.localStorage.setItem(key, value);
    }
  } catch {
    // 本地偏好不可用时不阻断导航。
  }
}

export function readChatLastWakerId() {
  const value = readStoredString(chatLastWakerStorageKey)?.trim();
  return value || undefined;
}

export function writeChatLastWakerId(wakerId: string) {
  const trimmed = wakerId.trim();
  if (!trimmed) return;
  writeStoredString(chatLastWakerStorageKey, trimmed);
}

function parseChatLastActive(raw: string | null) {
  if (!raw) return undefined;
  try {
    const parsed = JSON.parse(raw) as {
      chatId?: string;
      wakerId?: string;
    };
    const chatId = parsed.chatId?.trim();
    const wakerId = parsed.wakerId?.trim();
    if (!chatId || !wakerId) return undefined;
    return { chatId, wakerId };
  } catch {
    return undefined;
  }
}

export function readChatLastActive(): ChatLastActive | undefined {
  return parseChatLastActive(readStoredString(chatLastActiveStorageKey));
}

export function writeChatLastActive(wakerId: string, chatId: string) {
  const trimmedWakerId = wakerId.trim();
  const trimmedChatId = chatId.trim();
  if (!trimmedWakerId || !trimmedChatId) return;
  writeStoredString(
    chatLastActiveStorageKey,
    JSON.stringify({
      chatId: trimmedChatId,
      wakerId: trimmedWakerId,
    }),
  );
}

export function subscribeChatNavigation(listener: ChatNavigationListener) {
  navigationListeners.add(listener);
  return () => navigationListeners.delete(listener);
}

export function getChatLastActiveSnapshot() {
  const raw = readStoredString(chatLastActiveStorageKey);
  if (raw !== navigationSnapshotRaw) {
    navigationSnapshotRaw = raw;
    navigationSnapshot = parseChatLastActive(raw);
  }
  return navigationSnapshot;
}

export function clearChatLastActiveIfMatches(chatId: string) {
  const trimmedChatId = chatId.trim();
  if (!trimmedChatId) return;
  const current = readChatLastActive();
  if (current?.chatId !== trimmedChatId) return;
  try {
    if (typeof window.localStorage?.removeItem === "function") {
      window.localStorage.removeItem(chatLastActiveStorageKey);
    }
  } catch {
    // 本地偏好不可用时不阻断导航。
  }
  navigationSnapshotRaw = null;
  navigationSnapshot = undefined;
  for (const listener of navigationListeners) listener();
}

export function rememberChatLastActive(wakerId: string, chatId: string) {
  const next = {
    chatId: chatId.trim(),
    wakerId: wakerId.trim(),
  };
  if (!next.chatId || !next.wakerId) return;
  const raw = JSON.stringify(next);
  if (
    navigationSnapshot?.chatId === next.chatId &&
    navigationSnapshot.wakerId === next.wakerId &&
    navigationSnapshotRaw === raw
  ) {
    return;
  }
  writeStoredString(chatLastActiveStorageKey, raw);
  writeStoredString(chatLastWakerStorageKey, next.wakerId);
  navigationSnapshot = next;
  navigationSnapshotRaw = raw;
  for (const listener of navigationListeners) listener();
}

export function chatHref(chatId: string, wakerId: string) {
  const params = new URLSearchParams({ wakerId: wakerId.trim() });
  return `/chats/${chatId.trim()}?${params.toString()}`;
}

export function chatDraftHref(wakerId?: string) {
  const params = new URLSearchParams({ draftId: crypto.randomUUID() });
  if (wakerId?.trim()) params.set("wakerId", wakerId.trim());
  return `/chat?${params.toString()}`;
}

export function chatEntryHref(wakerId?: string) {
  if (!wakerId?.trim()) return "/chat";
  const params = new URLSearchParams({ wakerId: wakerId.trim() });
  return `/chat?${params.toString()}`;
}

export function chatLastActiveFromLocation(location: {
  pathname: string;
  search: string;
}): ChatLastActive | undefined {
  const match = /^\/chats\/([^/]+)$/.exec(location.pathname);
  const chatId = match?.[1]?.trim();
  const wakerId = new URLSearchParams(location.search).get("wakerId")?.trim();
  if (!chatId || !wakerId) return undefined;
  return { chatId, wakerId };
}

export function chatRailHref(
  location: { pathname: string; search: string },
  options?: { lastActive?: ChatLastActive },
) {
  const currentChat = chatLastActiveFromLocation(location);
  if (currentChat) {
    return chatHref(currentChat.chatId, currentChat.wakerId);
  }
  const lastActive = options ? options.lastActive : readChatLastActive();
  if (lastActive) {
    return chatHref(lastActive.chatId, lastActive.wakerId);
  }
  return chatEntryHref(readChatLastWakerId());
}
