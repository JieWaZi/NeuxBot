import { describe, expect, it } from "vitest";
import type { ChatLiveFrame } from "@/shared/api/events";
import { isChatLiveFrame } from "@/shared/api/events";
import { applyChatLiveFrame, ChatLiveTracker } from "./chat-live";
import type { AllyChatMessage } from "./chat-projection";

const baseline: AllyChatMessage = {
  id: "message",
  executionId: "message",
  role: "assistant",
  status: "running",
  revision: 3,
  content: "",
  displayParts: [{ id: "tool", type: "tool", toolStatus: "completed" }],
};
function frame(sequence = 1, baseRevision = 3, text = "你好"): ChatLiveFrame {
  return {
    generation: "stream-1",
    sequence,
    messageId: "message",
    executionId: "message",
    baseRevision,
    revision: baseRevision + 1,
    updates: [
      {
        baseRevision,
        revision: baseRevision + 1,
        updatedAt: 20,
        parts: [
          { kind: "text_append", partId: "reply", partType: "text", text },
        ],
      },
    ],
  };
}

describe("increment-only chat consumption", () => {
  it("追加正文保留已完成工具引用，持久结果接管后重复帧无副作用", () => {
    const next = applyChatLiveFrame(baseline, frame());
    expect(next.content).toBe("你好");
    expect(next.displayParts?.[0]).toBe(baseline.displayParts?.[0]);
    expect(next.displayParts?.[1]?.text).toBe("你好");
    expect(applyChatLiveFrame(next, frame())).toBe(next);
    expect(baseline.displayParts).toHaveLength(1);
    expect(baseline.content).toBe("");
  });

  it("多帧连续追加与显式阶段替换只更新命中节点", () => {
    const first = applyChatLiveFrame(baseline, frame());
    const second = applyChatLiveFrame(first, frame(2, 4, "世界"));
    expect(second.content).toBe("你好世界");
    expect(second.displayParts?.[0]).toBe(first.displayParts?.[0]);
    const reclassified = applyChatLiveFrame(second, {
      ...frame(3, 5),
      updates: [
        {
          baseRevision: 5,
          revision: 6,
          updatedAt: 30,
          parts: [
            {
              kind: "part_type",
              partId: "reply",
              partType: "process_commentary",
            },
          ],
        },
      ],
    });
    expect(reclassified.displayParts?.[1]).toMatchObject({
      type: "process_commentary",
      text: "你好世界",
    });
  });

  it("未知基线、断帧和新代次要求恢复，不接着猜测追加", () => {
    const tracker = new ChatLiveTracker();
    expect(tracker.accept(frame(), undefined)).toBe("recover");
    expect(tracker.accept(frame(), baseline)).toBe("accept");
    expect(tracker.accept(frame(), baseline)).toBe("duplicate");
    expect(tracker.accept(frame(3, 4), baseline)).toBe("recover");
    expect(
      tracker.accept({ ...frame(2, 4), generation: "new" }, baseline),
    ).toBe("recover");
    expect(tracker.accept(frame(2, 4), baseline)).toBe("accept");
  });

  it("恢复从任意帧号接续时仍核验覆盖版本，已持久前缀跳过", () => {
    const tracker = new ChatLiveTracker();
    const recovered = {
      ...baseline,
      revision: 4,
      content: "你好",
      displayParts: [{ id: "reply", type: "text" as const, text: "你好" }],
    };
    const replay = {
      ...frame(8, 3),
      revision: 5,
      updates: [...frame().updates, ...frame(9, 4, "世界").updates],
    };
    expect(tracker.accept(replay, recovered)).toBe("accept");
    expect(applyChatLiveFrame(recovered, replay).content).toBe("你好世界");
    expect(new ChatLiveTracker().accept(frame(8, 6), recovered)).toBe(
      "recover",
    );
  });

  it("拒绝不完整的公开增量载荷", () => {
    expect(isChatLiveFrame(frame())).toBe(true);
    expect(isChatLiveFrame({ ...frame(), updates: [{ revision: 4 }] })).toBe(
      false,
    );
    expect(isChatLiveFrame({ ...frame(), sequence: 0 })).toBe(false);
  });
});

it("交互元数据修订与正文覆盖独立合并，不丢实时用量或追加两次", async () => {
  const { mergeAuthoritativeMessages, applyChatEvent } =
    await import("./chat-projection");
  const local = {
    ...applyChatLiveFrame(baseline, frame()),
    revision: 6,
    liveRevision: 4,
    runMetrics: { contextUsedTokens: 200 },
  };
  const approval = {
    ...baseline,
    revision: 7,
    liveRevision: 3,
    status: "running" as const,
  };
  const merged = mergeAuthoritativeMessages([local], [approval])[0];
  expect(merged).toMatchObject({
    content: "你好",
    revision: 7,
    liveRevision: 4,
    runMetrics: { contextUsedTokens: 200 },
  });
  expect(merged?.displayParts).toBe(local.displayParts);
  const checkpoint = applyChatEvent([local], {
    type: "assistant.message",
    message: {
      ...baseline,
      revision: 7,
      liveRevision: 4,
      content: "你好",
      displayParts: local.displayParts,
    },
  })[0];
  if (!checkpoint) throw new Error("checkpoint message is missing");
  expect(checkpoint.runMetrics).toEqual({ contextUsedTokens: 200 });
  expect(applyChatLiveFrame(checkpoint, frame(2, 4, "世界")).content).toBe(
    "你好世界",
  );
  const result = applyChatEvent([local], {
    type: "workspace.changes",
    sequence: 9,
    fileChanges: {
      messageId: "message",
      record: {
        recordId: "result",
        version: 2,
        state: "ready",
        complete: true,
        totalFiles: 1,
      },
    },
  });
  expect(result[0]).toMatchObject({
    content: "你好",
    liveRevision: 4,
    revision: 9,
    fileChangeRecord: { recordId: "result" },
  });
  expect(result[0]?.displayParts).toBe(local.displayParts);
});
