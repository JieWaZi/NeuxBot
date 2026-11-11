import type { AllyChatMessage } from "./chat-projection";
import { afterEach, describe, expect, it, vi } from "vitest";
import { messageCompanionActivity } from "./companion-activity";
import { companionPlayback } from "@/shared/companion/playback";

afterEach(() => vi.useRealTimers());

type Part = NonNullable<AllyChatMessage["displayParts"]>[number];
const tool = (action: string, status = "running"): Part => ({
  type: "tool",
  toolCategory: "command",
  toolAction: action as Part["toolAction"],
  toolStatus: status as Part["toolStatus"],
});
function message(
  displayParts: Part[],
  status: AllyChatMessage["status"] = "running",
  preparingSession = false,
) {
  return { displayParts, status, preparingSession, isLast: true };
}

describe("消息驱动形象", () => {
  it("工作跟随真实消息，完成保持通知", () => {
    const stages = [
      message([], "pending", true),
      message([{ type: "reasoning", text: "规划" }]),
      message([tool("execute_command")]),
      message([tool("search_web")]),
      message([{ type: "text", text: "答复" }]),
      message([{ type: "text", text: "答复" }], "completed"),
    ];
    expect(stages.map((stage) => messageCompanionActivity(stage))).toEqual([
      "thinking",
      "thinking",
      "working",
      "working",
      "responding",
      "completed",
    ]);
    expect(
      stages.map(
        (stage) => companionPlayback(messageCompanionActivity(stage), 10).state,
      ),
    ).toEqual([
      "thinking",
      "thinking",
      "thinking",
      "thinking",
      "wink",
      "notify",
    ]);
  });
  it.each(["mcp", "network", "file_search", "file_read", "command"] as const)(
    "%s 都归工具执行，不额外创建连接、搜索或阅读动作",
    (toolCategory) => {
      expect(
        messageCompanionActivity(
          message([{ type: "tool", toolCategory, toolStatus: "running" }]),
        ),
      ).toBe("working");
    },
  );
  it("已完成工具不遮盖新正文，运行中工具优先于旧正文", () => {
    expect(
      messageCompanionActivity(
        message([
          tool("search_files", "completed"),
          { type: "text", text: "结果" },
        ]),
      ),
    ).toBe("responding");
    expect(
      messageCompanionActivity(
        message([{ type: "text", text: "先执行" }, tool("search_web")]),
      ),
    ).toBe("working");
  });
  it("按最新内容区分思考与正文，不按文字关键词猜工具状态", () => {
    expect(
      messageCompanionActivity(
        message([{ type: "text", text: "搜索只是正文" }]),
      ),
    ).toBe("responding");
    expect(
      messageCompanionActivity(
        message([
          { type: "text", text: "先说明" },
          { type: "reasoning", text: "继续思考" },
        ]),
      ),
    ).toBe("thinking");
    expect(
      messageCompanionActivity(message([tool("execute_command", "completed")])),
    ).toBe("thinking");
  });
  it("历史等待停止使用默认，失败警示与完成通知保留", () => {
    const running = message([tool("execute_command")]);
    expect(messageCompanionActivity({ ...running, isLast: false })).toBe(
      "idle",
    );
    expect(messageCompanionActivity(running, "waiting")).toBe("idle");
    expect(messageCompanionActivity(running, "stopping")).toBe("idle");
    expect(messageCompanionActivity(message([], "completed"))).toBe(
      "completed",
    );
    expect(messageCompanionActivity(message([], "failed"))).toBe("failed");
    expect(messageCompanionActivity(message([], "canceled"))).toBe("idle");
  });
});
