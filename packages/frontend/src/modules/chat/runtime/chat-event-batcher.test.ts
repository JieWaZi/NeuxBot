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
    batcher.push({ transient: true, sequence: 2 } as ChatEvent);
    batcher.push({ transient: true, sequence: 3 } as ChatEvent);

    expect(onBatch).not.toHaveBeenCalled();
    vi.advanceTimersToNextFrame();
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
        transient: true,
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

    vi.advanceTimersToNextFrame();

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
    batcher.push({ transient: true, sequence: 2 } as ChatEvent);
    batcher.push({ sequence: 3 } as ChatEvent, true);

    expect(onBatch).toHaveBeenCalledTimes(1);
    expect(onBatch.mock.calls[0]?.[0]).toHaveLength(2);
  });

  it("保留同一消息的全部有序增量，下一显示帧发布，关闭后释放待交付帧", () => {
    vi.useFakeTimers();
    const onBatch = vi.fn();
    const batcher = createChatEventBatcher(onBatch);
    batcher.push({
      transient: true,
      live: { messageId: "message", sequence: 1 },
    } as ChatEvent);
    batcher.push({
      transient: true,
      live: { messageId: "message", sequence: 2 },
    } as ChatEvent);
    vi.advanceTimersByTime(1);
    expect(onBatch).not.toHaveBeenCalled();
    vi.advanceTimersToNextFrame();
    expect(onBatch.mock.calls[0]?.[0]).toHaveLength(2);
    batcher.push({ transient: true } as ChatEvent);
    batcher.close();
    vi.advanceTimersByTime(64);
    expect(onBatch).toHaveBeenCalledTimes(1);
  });

  it("稀疏输入在各自下一帧交付，终态立即冲刷且没有迟到提交", () => {
    vi.useFakeTimers();
    const onBatch = vi.fn();
    const batcher = createChatEventBatcher(onBatch);
    batcher.push({ transient: true, sequence: 1 } as ChatEvent);
    vi.advanceTimersToNextFrame();
    expect(onBatch).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(80);
    batcher.push({ transient: true, sequence: 2 } as ChatEvent);
    vi.advanceTimersToNextFrame();
    expect(onBatch).toHaveBeenCalledTimes(2);
    batcher.push({ transient: true, sequence: 3 } as ChatEvent);
    batcher.push({ type: "execution.completed", sequence: 4 } as ChatEvent);
    expect(onBatch).toHaveBeenCalledTimes(3);
    expect(onBatch.mock.calls[2]?.[0]).toEqual([
      expect.objectContaining({ sequence: 3 }),
      expect.objectContaining({ sequence: 4 }),
    ]);
    vi.advanceTimersByTime(80);
    expect(onBatch).toHaveBeenCalledTimes(3);
    batcher.close();
  });
});
