import { expect, it } from "vitest";
import { waitingConversations, workEntryHref } from "@/modules/work-board";
import { workspaceAttentionOpenHref } from "./workspace-attention-navigation";

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
