import { describe, expect, it } from "vitest";
import {
  mainConversationHref,
  mainConversationOwner,
  mainConversationScene,
} from "./main-conversation-route";

describe("主体主会话路由", () => {
  it("伙伴和团队即使同名也有各自页签，地址可还原含保留字符的主体 ID", () => {
    const id = "研发/周会";
    const state = { from: "history" };
    for (const kind of ["waker", "team"] as const) {
      const pathname = mainConversationHref({ kind, id });
      expect(mainConversationOwner(pathname)).toEqual({ kind, id });
      expect(
        mainConversationScene({ pathname, search: "?q=1", state }, "主会话"),
      ).toMatchObject({
        id: `main:${kind}:${id}`,
        mountKey: `main:${kind}:${id}`,
        href: `${pathname}?q=1`,
        navigationState: state,
        chatOwner: { kind, id },
        background: "preserve",
      });
    }
  });

  it("普通聊天、已有团队线程和损坏的地址交回应用路由处理", () => {
    for (const pathname of [
      "/chats/chat-1",
      "/teams/team-1/threads/thread-2",
      "/chat/main/team/%ZZ",
    ]) {
      expect(
        mainConversationScene({ pathname, search: "" }, "主会话"),
      ).toBeUndefined();
    }
  });
});
