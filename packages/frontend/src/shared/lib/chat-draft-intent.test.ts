import { expect, it, vi } from "vitest";
import {
  chatPromptDestination,
  readChatDraftIntent,
} from "./chat-draft-intent";

it("creates an independent draft and carries text without putting it in the URL", () => {
  vi.spyOn(crypto, "randomUUID")
    .mockReturnValueOnce("00000000-0000-4000-8000-000000000001")
    .mockReturnValueOnce("00000000-0000-4000-8000-000000000002");
  const first = chatPromptDestination(
    "Review the changes",
    "partner-1",
    "plugin-1",
  );
  const second = chatPromptDestination("Another example", "partner-1");
  expect(first.to).toBe(
    "/chat?draftId=00000000-0000-4000-8000-000000000001&wakerId=partner-1",
  );
  expect(second.to).toContain("draftId=00000000-0000-4000-8000-000000000002");
  expect(
    readChatDraftIntent(
      first.state,
      "00000000-0000-4000-8000-000000000001",
      "partner-1",
    ),
  ).toEqual({
    draftId: "00000000-0000-4000-8000-000000000001",
    wakerId: "partner-1",
    pluginId: "plugin-1",
    text: "Review the changes",
  });
  expect(
    readChatDraftIntent(
      second.state,
      "00000000-0000-4000-8000-000000000002",
      "partner-1",
    )?.pluginId,
  ).toBeUndefined();
  vi.restoreAllMocks();
});

it("rejects a draft from a different partner or scope and malformed route state", () => {
  const { state } = chatPromptDestination("Text", "partner-1", "plugin-1");
  expect(
    readChatDraftIntent(state, "other-draft", "partner-1"),
  ).toBeUndefined();
  expect(
    readChatDraftIntent(state, state.chatDraftIntent.draftId, "partner-2"),
  ).toBeUndefined();
  for (const value of [
    null,
    "text",
    { chatDraftIntent: null },
    { chatDraftIntent: { draftId: "draft", wakerId: "partner", text: [] } },
  ])
    expect(readChatDraftIntent(value, "draft", "partner")).toBeUndefined();
});

it("carries an independent skill identity without copying its instructions or inventing a plugin", () => {
  const destination = chatPromptDestination(
    "帮我使用这个技能。",
    "partner-1",
    undefined,
    "skill-1",
  );
  expect(
    readChatDraftIntent(
      destination.state,
      destination.state.chatDraftIntent.draftId,
      "partner-1",
    ),
  ).toEqual({
    draftId: destination.state.chatDraftIntent.draftId,
    wakerId: "partner-1",
    text: "帮我使用这个技能。",
    skillId: "skill-1",
  });
});

it("carries a connector in the optional fifth argument and keeps its configuration out of the destination", () => {
  const destination = chatPromptDestination(
    "帮我使用这个连接器。",
    "partner-1",
    undefined,
    undefined,
    "connector-1",
  );
  expect(destination.to).not.toMatch(/connector|token|command|帮我/);
  expect(
    readChatDraftIntent(
      destination.state,
      destination.state.chatDraftIntent.draftId,
      "partner-1",
    ),
  ).toEqual({
    draftId: destination.state.chatDraftIntent.draftId,
    wakerId: "partner-1",
    text: "帮我使用这个连接器。",
    connectorId: "connector-1",
  });
  expect(
    readChatDraftIntent(destination.state, "another-draft", "partner-1"),
  ).toBeUndefined();
  expect(
    readChatDraftIntent(
      destination.state,
      destination.state.chatDraftIntent.draftId,
      "partner-2",
    ),
  ).toBeUndefined();
});

it("rejects conflicting or malformed reference identities instead of selecting multiple resources", () => {
  const intent = { draftId: "draft", wakerId: "partner", text: "Text" };
  for (const references of [
    { connectorId: 123 },
    { connectorId: ["connector"] },
    { connectorId: { id: "connector" } },
    { connectorId: null },
    { pluginId: true },
    { skillId: undefined },
    { connectorId: " " },
    { pluginId: "plugin", connectorId: "connector" },
    { skillId: "skill", connectorId: "connector" },
    { skillId: "skill", pluginId: "plugin" },
  ])
    expect(
      readChatDraftIntent(
        { chatDraftIntent: { ...intent, ...references } },
        "draft",
        "partner",
      ),
    ).toBeUndefined();
  expect(
    readChatDraftIntent(
      {
        chatDraftIntent: { ...intent, pluginId: "", connectorId: "connector" },
      },
      "draft",
      "partner",
    ),
  ).toEqual({ ...intent, connectorId: "connector" });
});

it("does not read inherited, array or accessor properties as a draft intent", () => {
  const intent = {
    draftId: "draft",
    wakerId: "partner",
    text: "Text",
    connectorId: "connector",
  };
  const getter = vi.fn(() => intent);
  const textGetter = vi.fn(() => "Text");
  const connectorGetter = vi.fn(() => "connector");
  for (const state of [
    Object.create({ chatDraftIntent: intent }),
    { chatDraftIntent: Object.create(intent) },
    Object.assign([], { chatDraftIntent: intent }),
    { chatDraftIntent: Object.assign([], intent) },
    Object.defineProperty({}, "chatDraftIntent", { get: getter }),
    {
      chatDraftIntent: Object.defineProperty({ ...intent }, "text", {
        get: textGetter,
      }),
    },
    {
      chatDraftIntent: Object.defineProperty({ ...intent }, "connectorId", {
        get: connectorGetter,
      }),
    },
  ])
    expect(readChatDraftIntent(state, "draft", "partner")).toBeUndefined();
  expect(getter).not.toHaveBeenCalled();
  expect(textGetter).not.toHaveBeenCalled();
  expect(connectorGetter).not.toHaveBeenCalled();
});
