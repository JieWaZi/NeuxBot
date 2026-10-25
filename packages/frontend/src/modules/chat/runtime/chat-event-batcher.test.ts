import { afterEach, describe, expect, it, vi } from "vitest";

import { createChatEventBatcher } from "./chat-event-batcher";
import type { ChatEvent } from "@/shared/api/events";

afterEach(() => {
  vi.useRealTimers();
});

describe("chat event batcher", () => {
  it("在短窗口内一次提交多个增量并保持顺序", () => {
    vi.useFakeTimers();
    const onBatch = vi.fn();
    const batcher = createChatEventBatcher(onBatch);
    batcher.push({ sequence: 2 } as ChatEvent);
    batcher.push({ sequence: 3 } as ChatEvent);

    expect(onBatch).not.toHaveBeenCalled();
    vi.advanceTimersByTime(16);
    expect(onBatch).toHaveBeenCalledWith([
      expect.objectContaining({ sequence: 2 }),
      expect.objectContaining({ sequence: 3 }),
    ]);
  });

  it("同一消息在短窗口内只提交最新完整快照", () => {
    vi.useFakeTimers();
    const onBatch = vi.fn();
    const batcher = createChatEventBatcher(onBatch);

    for (let revision = 1; revision <= 100; revision += 1) {
      batcher.push({
        sequence: revision,
        type: "runtime.event",
        message: {
          id: "assistant-running",
          role: "assistant",
          status: "running",
          revision,
          displayParts: [{ type: "text", text: "内容".repeat(revision) }],
        },
      } as ChatEvent);
    }

    vi.advanceTimersByTime(16);

    expect(onBatch).toHaveBeenCalledTimes(1);
    expect(onBatch.mock.calls[0]?.[0]).toEqual([
      expect.objectContaining({
        sequence: 100,
        message: expect.objectContaining({ revision: 100 }),
      }),
    ]);
  });

  it("终态边界立即提交此前全部增量", () => {
    vi.useFakeTimers();
    const onBatch = vi.fn();
    const batcher = createChatEventBatcher(onBatch);
    batcher.push({ sequence: 2 } as ChatEvent);
    batcher.push({ sequence: 3 } as ChatEvent, true);

    expect(onBatch).toHaveBeenCalledTimes(1);
    expect(onBatch.mock.calls[0]?.[0]).toHaveLength(2);
  });
});
