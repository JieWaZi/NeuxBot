import type { AllyChatMessage } from "./chat-projection";
import { afterEach, describe, expect, it, vi } from "vitest";
import { messageCompanionActivity } from "./companion-activity";
import { companionPresentation } from "@/shared/companion/playback";

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
  it("准备期间持续显示环绕圈圈，原生接纳后才进入思考", () => {
    const pending = messageCompanionActivity(message([], "pending", true));
    expect(companionPresentation(pending).state).toBe("swirl");
    const accepted = messageCompanionActivity(message([]));
    expect(companionPresentation(accepted).motion).toBe("thinking");
    expect(companionPresentation(accepted).state).toBe("thinking");
  });
  it("会话等待与停止只影响最新消息，历史消息即使保留 running 也保持空闲", () => {
    const messages = [
      { ...message([tool("execute_command")]), isLast: false },
      { ...message([tool("search_web")]), isLast: true },
    ];
    expect(
      messages.map((item) => messageCompanionActivity(item, "waiting")),
    ).toEqual(["idle", "waiting"]);
    expect(
      messages.map((item) => messageCompanionActivity(item, "stopping")),
    ).toEqual(["idle", "stopped"]);
    expect(messages.map((item) => messageCompanionActivity(item))).toEqual([
      "idle",
      "searching",
    ]);
  });
  it("新消息出现后，之前的终态和工作动画都退出状态跟随", () => {
    const latest = message([tool("execute_command")]);
    expect(messageCompanionActivity(latest)).toBe("working");
    expect(messageCompanionActivity({ ...latest, isLast: false })).toBe("idle");
    for (const status of ["completed", "failed", "canceled"] as const) {
      expect(
        messageCompanionActivity(
          { ...message([], status), isLast: false },
          "waiting",
        ),
      ).toBe("idle");
    }
  });
  it("开始、搜索、执行、思考和回答来自结构化消息", () => {
    expect(messageCompanionActivity(message([], "pending", true))).toBe(
      "connecting",
    );
    expect(messageCompanionActivity(message([], "pending"))).toBe("thinking");
    expect(messageCompanionActivity(message([]))).toBe("thinking");
    expect(messageCompanionActivity(message([tool("search_web")]))).toBe(
      "searching",
    );
    expect(messageCompanionActivity(message([tool("execute_command")]))).toBe(
      "working",
    );
    expect(
      messageCompanionActivity(message([{ type: "reasoning", text: "规划" }])),
    ).toBe("thinking");
    expect(
      messageCompanionActivity(
        message([{ type: "text", text: "搜索只是正文" }]),
      ),
    ).toBe("responding");
  });
  it("已完成的工具不会遮盖回答，仍在运行的工具优先于旧正文", () => {
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
        message([tool("search_web"), tool("execute_command", "completed")]),
      ),
    ).toBe("searching");
  });
  it("终态优先于残留工具与审批，等待与停止优先于运行中工具", () => {
    const content = [tool("execute_command")];
    expect(
      messageCompanionActivity(message(content, "completed"), "waiting"),
    ).toBe("completed");
    expect(messageCompanionActivity(message(content, "failed"))).toBe("failed");
    expect(messageCompanionActivity(message(content, "canceled"))).toBe(
      "stopped",
    );
    expect(messageCompanionActivity(message(content), "waiting")).toBe(
      "waiting",
    );
    expect(messageCompanionActivity(message(content), "stopping")).toBe(
      "stopped",
    );
  });
});
