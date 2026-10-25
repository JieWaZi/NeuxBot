import { expect, it } from "vitest";
import type { NotificationItem } from "@/modules/notification";
import { workAttentionHref } from "@/modules/work-board/model/navigation";
import { notificationHref } from "./notification-navigation";

it.each(["approval", "input"] as const)(
  "工作流创作%s定位实际创作聊天，关闭提示仍保留相同入口",
  (kind) => {
    const notice: NotificationItem = {
      id: `${kind}:request-1`,
      kind,
      bucket: "action",
      source: "workflow",
      title: "待处理",
      description: "创作工作流",
      createdAt: 1,
      version: 1,
      executor: { id: "flow", kind: "workflow", name: "工作流" },
      target: {
        kind,
        definitionId: "flow",
        chatId: "creation-chat",
        ownerKind: "chat",
        ownerId: "creation-chat",
        requestId: "request-1",
      },
      dismissed: false,
    };
    const href = notificationHref(notice);
    const url = new URL(href, "https://example.com");
    expect(url.pathname).toBe("/workflows/flow");
    expect(url.searchParams.get("chat")).toBe("creation-chat");
    expect(url.searchParams.get("request")).toBe(notice.id);
    expect(notificationHref({ ...notice, dismissed: true })).toBe(href);
    const boardURL = new URL(
      workAttentionHref({
        ownerKind: "chat",
        ownerId: "creation-chat",
        chatId: "creation-chat",
        workflowId: "flow",
        id: notice.id,
      }),
      "https://example.com",
    );
    expect(boardURL.pathname).toBe(url.pathname);
    expect(boardURL.searchParams.get("chat")).toBe("creation-chat");
    expect(
      notificationHref({
        ...notice,
        target: { ...notice.target, runId: "worker-run" },
      }),
    ).toContain("/chats/creation-chat?");
    expect(
      notificationHref({
        ...notice,
        target: { kind, chatId: "ordinary-chat", wakerId: "waker" },
      }),
    ).toBe("/chats/ordinary-chat?wakerId=waker");
    expect(
      notificationHref({
        ...notice,
        target: {
          kind,
          definitionId: "flow",
          runId: "run",
          requestId: "request-1",
        },
      }),
    ).toBe(`/workflows/flow/runs/run?request=${encodeURIComponent(notice.id)}`);
    expect(
      notificationHref({
        ...notice,
        target: { kind, channelId: "team", threadId: "thread" },
      }),
    ).toBe("/teams/team/threads/thread?");
  },
);
