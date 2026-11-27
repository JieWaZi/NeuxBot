import { expect, it } from "vitest";
import { waitingConversations, workEntryHref } from "@/modules/work-board";
import { workspaceAttentionOpenHref } from "./workspace-attention-navigation";

it.each([
  { target: { wakerId: "partner" }, path: "/chat/main/waker/partner" },
  { target: { channelId: "team" }, path: "/chat/main/team/team" },
])("桌面公开回报直达原消息 $path", ({ target, path }) => {
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "main_conversation", id: "public-conversation" },
      operation: "published",
      messageId: "public:message",
      ...target,
    }),
  ).toBe(path + "?message=public%3Amessage");
});

it("团队请求携带真正的成员 Chat，而验收保留原任务围栏", () => {
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "team_thread", id: "thread" },
      operation: "request_opened",
      channelId: "team",
      chatId: "member-chat",
      wakerId: "member",
      requestId: "original:request",
    }),
  ).toBe(
    "/teams/team/threads/thread?request=original%3Arequest&wakerId=member&requestChat=member-chat",
  );
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "team_thread", id: "thread" },
      operation: "request_opened",
      channelId: "team",
      requestId: "review",
      taskId: "task",
      requestFence: 7,
    }),
  ).toBe("/teams/team/threads/thread?task=task&fence=7");
});

it("桌面主会话待办保留公开主体和原工作来源", () => {
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "main_conversation", id: "public-team" },
      operation: "request_opened",
      channelId: "team",
      wakerId: "member",
      chatId: "member-work",
      requestId: "native:request",
    }),
  ).toBe(
    "/chat/main/team/team?request=native%3Arequest&requestChat=member-work",
  );
});

it("工作流 worker 请求进入实际 Chat，自动机会打开精确运行结果", () => {
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "workflow_run", id: "run" },
      operation: "request_opened",
      definitionId: "workflow",
      chatId: "worker",
      wakerId: "partner",
      requestId: "request",
    }),
  ).toBe("/chats/worker?wakerId=partner&request=request");
  expect(
    workspaceAttentionOpenHref({
      work: { kind: "automatic_run", id: "run" },
      operation: "failed",
      automaticTaskId: "task",
    }),
  ).toBe("/automatic-tasks/task?run=run");
});

it("工作看板打开工作流创作原会话和准确消息", () => {
  expect(
    workEntryHref(
      {
        ownerKind: "chat",
        chatId: "creation",
        ownerId: "creation",
        workflowId: "definition",
        firstUnreadMessageID: "message",
      },
      true,
    ),
  ).toBe("/workflows/definition?chat=creation&message=message");
});

it("请求选择只显示每个实际 Chat 的队首，成员对话各自选择", () => {
  const entries = waitingConversations([
    {
      id: "a-later",
      chatId: "member-a",
      kind: "approval",
      createdAt: 20,
      requestSequence: 8,
    },
    {
      id: "z-head",
      chatId: "member-a",
      kind: "approval",
      createdAt: 20,
      requestSequence: 7,
    },
    { id: "other", chatId: "member-b", kind: "input", createdAt: 10 },
  ]);
  expect(entries.map((entry) => entry.request.id)).toEqual(["other", "z-head"]);
});
