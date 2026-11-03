import { describe, expect, it } from "vitest";
import { chatContinuations } from "./chat-continuation";
import type { AllyChatMessage } from "../runtime/chat-projection";

const first: AllyChatMessage = {
  id: "first",
  executionId: "run-1",
  role: "assistant",
  status: "canceled",
  displayParts: [{ id: "a", type: "text", text: "前一段" }],
  runMetrics: {
    durationMs: 9000,
    inputTokens: 10,
    outputTokens: 20,
    contextUsedTokens: 100,
  },
};
const user: AllyChatMessage = { id: "user", role: "user", content: "追加要求" };
const next: AllyChatMessage = {
  id: "next",
  executionId: "run-2",
  role: "assistant",
  status: "completed",
  steerExecutionId: "run-1",
  displayParts: [{ id: "b", type: "text", text: "后一段" }],
  runMetrics: {
    durationMs: 5000,
    inputTokens: 30,
    outputTokens: 40,
    contextUsedTokens: 200,
  },
};

describe("steer continuation", () => {
  it("连接被 steer 的前后两段，只在最后汇总耗时、用量与复制正文", () => {
    const result = chatContinuations([first, user, next]);
    expect(result.get("first")?.continued).toBe(true);
    expect(result.get("first")?.activityMessage).toBe(next);
    expect(result.get("next")?.continuation).toBe(true);
    expect(result.get("next")?.footerMessage.runMetrics).toMatchObject({
      durationMs: 14000,
      inputTokens: 40,
      outputTokens: 60,
      contextUsedTokens: 200,
    });
    expect(
      result.get("next")?.footerMessage.displayParts?.map((p) => p.text),
    ).toEqual(["前一段", "后一段"]);
  });
  it("普通停止后再发消息仍是两次独立任务", () => {
    expect(
      chatContinuations([first, user, { ...next, steerExecutionId: undefined }])
        .size,
    ).toBe(0);
  });
  it("不连接已完成任务或不匹配的执行", () => {
    expect(
      chatContinuations([{ ...first, status: "completed" }, user, next]).size,
    ).toBe(0);
    expect(
      chatContinuations([first, user, { ...next, steerExecutionId: "other" }])
        .size,
    ).toBe(0);
  });
  it("支持连续追加，并保持最后一次上下文读数", () => {
    const last = {
      ...next,
      id: "last",
      executionId: "run-3",
      steerExecutionId: "run-2",
    };
    const result = chatContinuations([
      first,
      user,
      { ...next, status: "canceled" },
      user,
      last,
    ]);
    expect(result.get("next")?.continued).toBe(true);
    expect(result.get("last")?.footerMessage.runMetrics?.durationMs).toBe(
      19000,
    );
    expect(result.get("first")?.activityMessage).toBe(last);
  });
  it("取消到下一段接纳之间隐藏中间收尾，发送失败撤去临时标记后恢复", () => {
    expect(
      chatContinuations([{ ...first, steerPending: true }]).get("first")
        ?.continued,
    ).toBe(true);
    expect(chatContinuations([first]).size).toBe(0);
  });
});
