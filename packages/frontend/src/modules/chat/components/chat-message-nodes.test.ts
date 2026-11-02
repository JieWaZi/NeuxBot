import { describe, expect, it, vi } from "vitest";
import type { AllyChatMessage } from "../runtime/chat-projection";
import { applyChatLiveFrame } from "../runtime/chat-live";
import { ChatMessageProjection } from "./chat-message-nodes";

type Part = NonNullable<AllyChatMessage["displayParts"]>[number];
const tool = (id: string): Part => ({
  id,
  type: "tool",
  toolCallId: "native-reused",
  toolCategory: "network",
  toolStatus: "completed",
});
function required<T>(value: T | null | undefined): T {
  if (value == null) throw new Error("missing fixture");
  return value;
}

describe("canonical 消息节点投影", () => {
  it("100个完成工具之后100次追加正文，不重建工具节点或无关组", () => {
    const node = vi.fn(),
      group = vi.fn(),
      inspect = vi.fn();
    const projection = new ChatMessageProjection({ node, group, inspect });
    const tools = Array.from({ length: 100 }, (_, index) =>
      tool(`invocation:tool:${index}`),
    );
    const history: AllyChatMessage = {
      id: "history",
      role: "user",
      content: "先前消息",
    };
    let current: AllyChatMessage = {
      id: "assistant",
      role: "assistant",
      status: "running",
      revision: 0,
      displayParts: [...tools, { id: "body", type: "text", text: "开始" }],
    };
    const first = projection.update([history, current]);
    const process = required(first[1]).segments[0];
    node.mockClear();
    group.mockClear();
    inspect.mockClear();
    for (let index = 0; index < 100; index += 1) {
      current = applyChatLiveFrame(current, {
        messageId: "assistant",
        executionId: "assistant",
        generation: "g",
        sequence: index + 1,
        baseRevision: index,
        revision: index + 1,
        updates: [
          {
            baseRevision: index,
            revision: index + 1,
            updatedAt: 0,
            parts: [
              {
                kind: "text_append",
                partId: "body",
                partType: "text",
                text: "正文",
              },
            ],
          },
        ],
      });
      const next = projection.update([history, current]);
      expect(next[0]).toBe(first[0]);
      expect(required(next[1]).segments[0]).toBe(process);
      expect(required(next[1]).activity).toBe("responding");
    }
    expect(node).toHaveBeenCalledTimes(100);
    expect(group).not.toHaveBeenCalled();
    expect(inspect).toHaveBeenCalledTimes(100);
  });
  it("工具之后新增末尾正文及多批合并发布也只检查正文部件", () => {
    const inspect = vi.fn(),
      group = vi.fn();
    const projection = new ChatMessageProjection({ inspect, group });
    let current: AllyChatMessage = {
      id: "m",
      role: "assistant",
      status: "running",
      revision: 0,
      displayParts: Array.from({ length: 100 }, (_, index) =>
        tool(String(index)),
      ),
    };
    const first = required(projection.update([current])[0]);
    inspect.mockClear();
    group.mockClear();
    for (let index = 0; index < 20; index += 1)
      current = applyChatLiveFrame(current, {
        messageId: "m",
        executionId: "m",
        generation: "g",
        sequence: index + 1,
        baseRevision: index,
        revision: index + 1,
        updates: [
          {
            baseRevision: index,
            revision: index + 1,
            updatedAt: 0,
            parts: [
              {
                kind: "text_append",
                partId: "body",
                partType: "text",
                text: "正文",
              },
            ],
          },
        ],
      });
    const next = required(projection.update([current])[0]);
    expect(next.segments[0]).toBe(first.segments[0]);
    expect(next.segments).toHaveLength(2);
    expect(inspect).toHaveBeenCalledTimes(1);
    expect(group).not.toHaveBeenCalled();
  });
  it("同一提交的用户与助手独立，乐观接管维持角色内身份", () => {
    const projection = new ChatMessageProjection();
    const first = projection.update([
      {
        id: "local-user",
        submissionId: "submission",
        role: "user",
        content: "提交",
      },
      {
        id: "local-assistant",
        submissionId: "submission",
        role: "assistant",
        status: "running",
      },
    ]);
    const next = projection.update([
      {
        id: "real-user",
        submissionId: "submission",
        role: "user",
        content: "提交",
      },
      {
        id: "real-assistant",
        submissionId: "submission",
        role: "assistant",
        status: "completed",
      },
    ]);
    expect(first.map((view) => view.key)).toEqual(next.map((view) => view.key));
    expect(new Set(next.map((view) => view.key)).size).toBe(2);
  });
  it("已接纳消息的提交标识缺失或变化时按权威ID保留身份，新ID及退出窗口不继承", () => {
    const projection = new ChatMessageProjection();
    const optimistic: AllyChatMessage = {
      id: "local-assistant",
      submissionId: "submission",
      role: "assistant",
      status: "pending",
    };
    const first = required(projection.update([optimistic])[0]);
    const accepted = { ...optimistic, id: "real-assistant" };
    expect(required(projection.update([accepted])[0]).key).toBe(first.key);
    const authoritative = { ...accepted, submissionId: undefined };
    expect(required(projection.update([authoritative])[0]).key).toBe(first.key);
    expect(
      required(
        projection.update([{ ...authoritative, submissionId: "changed" }])[0],
      ).key,
    ).toBe(first.key);
    expect(
      required(projection.update([{ ...authoritative, id: "new-message" }])[0])
        .key,
    ).not.toBe(first.key);
    projection.update([]);
    expect(required(projection.update([authoritative])[0]).key).not.toBe(
      first.key,
    );
  });
  it("原生调用标识重复时按已记录的Invocation部件身份区分", () => {
    const projection = new ChatMessageProjection();
    const view = required(
      projection.update([
        {
          id: "m",
          role: "assistant",
          displayParts: [tool("first:tool"), tool("second:tool")],
        },
      ])[0],
    );
    const process = required(view.segments[0]);
    expect("kind" in process).toBe(true);
    if (!("kind" in process)) throw new Error("missing group");
    const group = required(process.nodes[0]);
    if (!("kind" in group)) throw new Error("missing tools");
    expect(group.nodes).toHaveLength(2);
    expect(group.nodes[0]?.key).not.toBe(group.nodes[1]?.key);
  });
  it("替换、清空、分页回收与不同聊天不复用旧内容", () => {
    const projection = new ChatMessageProjection();
    const old = required(
      projection.update([
        {
          id: "m",
          role: "assistant",
          displayParts: [{ id: "p", type: "text", text: "旧内容" }],
        },
      ])[0],
    );
    const next = required(
      projection.update([
        {
          id: "m",
          role: "assistant",
          displayParts: [{ id: "p", type: "text", text: "新内容" }],
        },
      ])[0],
    );
    expect(next.segments[0]).not.toBe(old.segments[0]);
    expect(projection.update([])).toEqual([]);
    const fresh = required(projection.update([old.message])[0]);
    expect(fresh).not.toBe(old);
    expect(
      new ChatMessageProjection(undefined, "other").update([old.message])[0]
        ?.key,
    ).not.toBe(old.key);
  });
});
