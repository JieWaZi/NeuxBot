import { resolveChatEntryWakerId } from "./resolve-chat-entry-waker";

type ChatSummary = {
  id?: string;
  wakerId?: string;
};

export type ChatRecentTarget =
  | { kind: "chat"; chatId: string; wakerId: string }
  | { kind: "draft"; wakerId: string };

export function resolveChatRecentTarget({
  requestedWakerId,
  lastWakerId,
  lastActive,
  wakers,
  latestChat,
}: {
  requestedWakerId?: string;
  lastWakerId?: string;
  lastActive?: ChatSummary;
  wakers: readonly { id?: string }[];
  latestChat?: ChatSummary;
}): ChatRecentTarget {
  const wakerId = resolveChatEntryWakerId({
    requestedWakerId,
    lastWakerId,
    wakers,
  });

  if (!wakerId) {
    return { kind: "draft", wakerId: "" };
  }

  if (
    lastActive?.id &&
    (!lastActive.wakerId || lastActive.wakerId === wakerId)
  ) {
    return {
      kind: "chat",
      chatId: lastActive.id,
      wakerId: lastActive.wakerId ?? wakerId,
    };
  }

  if (
    latestChat?.id &&
    (!latestChat.wakerId || latestChat.wakerId === wakerId)
  ) {
    return {
      kind: "chat",
      chatId: latestChat.id,
      wakerId: latestChat.wakerId ?? wakerId,
    };
  }

  return { kind: "draft", wakerId };
}
