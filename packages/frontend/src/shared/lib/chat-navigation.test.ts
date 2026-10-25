import { describe, expect, it, vi } from "vitest";

import {
  clearChatLastActiveIfMatches,
  chatDraftHref,
  chatEntryHref,
  chatHref,
  chatLastActiveFromLocation,
  chatRailHref,
  readChatLastActive,
  readChatLastWakerId,
  writeChatLastActive,
  writeChatLastWakerId,
} from "./chat-navigation";

describe("chat-navigation", () => {
  it("builds draft and entry hrefs", () => {
    vi.stubGlobal("crypto", { randomUUID: () => "draft-1" });

    expect(chatDraftHref("waker-1")).toBe(
      "/chat?draftId=draft-1&wakerId=waker-1",
    );
    expect(chatEntryHref("waker-1")).toBe("/chat?wakerId=waker-1");
    expect(chatEntryHref()).toBe("/chat");
    expect(chatHref("chat-1", "waker-1")).toBe("/chats/chat-1?wakerId=waker-1");
  });

  it("persists the last active waker and chat", () => {
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => {
        storage.set(key, value);
      },
    });

    expect(readChatLastWakerId()).toBeUndefined();
    expect(readChatLastActive()).toBeUndefined();
    writeChatLastWakerId("waker-2");
    writeChatLastActive("waker-2", "chat-2");
    expect(readChatLastWakerId()).toBe("waker-2");
    expect(readChatLastActive()).toEqual({
      wakerId: "waker-2",
      chatId: "chat-2",
    });
  });

  it("clears the last active chat when it is deleted", () => {
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => {
        storage.set(key, value);
      },
      removeItem: (key: string) => {
        storage.delete(key);
      },
    });
    writeChatLastActive("waker-2", "chat-2");

    clearChatLastActiveIfMatches("chat-2");
    expect(readChatLastActive()).toBeUndefined();
  });

  it("routes the chat rail through the recent-chat entry", () => {
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => {
        storage.set(key, value);
      },
    });
    writeChatLastWakerId("waker-1");
    writeChatLastActive("waker-1", "chat-2");
    expect(
      chatRailHref({
        pathname: "/chats/chat-9",
        search: "?wakerId=waker-9",
      }),
    ).toBe("/chats/chat-9?wakerId=waker-9");
    expect(
      chatRailHref({
        pathname: "/wakers/waker-1",
        search: "",
      }),
    ).toBe("/chats/chat-2?wakerId=waker-1");
    expect(
      chatRailHref(
        { pathname: "/wakers/waker-1", search: "" },
        { lastActive: undefined },
      ),
    ).toBe("/chat?wakerId=waker-1");
  });

  it("derives the active chat from the canonical route", () => {
    expect(
      chatLastActiveFromLocation({
        pathname: "/chats/chat-b",
        search: "?wakerId=waker-1",
      }),
    ).toEqual({
      chatId: "chat-b",
      wakerId: "waker-1",
    });
    expect(
      chatLastActiveFromLocation({
        pathname: "/chat",
        search: "?wakerId=waker-1",
      }),
    ).toBeUndefined();
  });
});
