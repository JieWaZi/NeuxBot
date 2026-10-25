import type { ThreadMessage } from "@assistant-ui/react";
import { describe, expect, it } from "vitest";
import { messageCompanionActivity } from "./companion-activity";

const tool = (action: string, status = "running") => ({
  type: "tool-call" as const,
  toolCallId: action,
  toolName: "tool",
  argsText: "{}",
  args: { category: "command", action, status },
});
function message(
  content: ThreadMessage["content"],
  status: ThreadMessage["status"] = { type: "running" },
) {
  return { content, status, isLast: true };
}

describe("消息驱动形象", () => {
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
    for (const status of [
      { type: "complete", reason: "stop" },
      { type: "incomplete", reason: "error" },
      { type: "incomplete", reason: "cancelled" },
    ] as const) {
      expect(
        messageCompanionActivity(
          { ...message([], status), isLast: false },
          "waiting",
        ),
      ).toBe("idle");
    }
  });
  it("开始、搜索、执行、思考和回答来自结构化消息", () => {
    expect(messageCompanionActivity(message([]))).toBe("starting");
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
      messageCompanionActivity(
        message(content, { type: "complete", reason: "stop" }),
        "waiting",
      ),
    ).toBe("completed");
    expect(
      messageCompanionActivity(
        message(content, { type: "incomplete", reason: "error" }),
      ),
    ).toBe("failed");
    expect(
      messageCompanionActivity(
        message(content, { type: "incomplete", reason: "cancelled" }),
      ),
    ).toBe("stopped");
    expect(messageCompanionActivity(message(content), "waiting")).toBe(
      "waiting",
    );
    expect(messageCompanionActivity(message(content), "stopping")).toBe(
      "stopped",
    );
  });
});
