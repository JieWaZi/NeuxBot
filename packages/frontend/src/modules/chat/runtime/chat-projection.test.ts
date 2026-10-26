import { describe, expect, it } from "vitest";

import {
  applyChatEvent,
  latestContextMetrics,
  mergeAuthoritativeMessages,
  toThreadMessage,
  type AllyChatMessage,
} from "./chat-projection";
import type { ChatEvent } from "@/shared/api/events";

describe("chat projection", () => {
  it("同一消息中复用原生工具标识的不同调用保留独立展示身份", () => {
    const projected = toThreadMessage({
      id: "assistant-retried",
      role: "assistant",
      status: "running",
      displayParts: [
        {
          id: "invocation-one:call-1",
          type: "tool",
          toolCallId: "call-1",
          toolStatus: "completed",
          toolCategory: "file_read",
          toolAction: "read_file",
          toolSafeSummary: { path: "one.go" },
        },
        {
          id: "invocation-two:call-1",
          type: "tool",
          toolCallId: "call-1",
          toolStatus: "running",
          toolCategory: "file_read",
          toolAction: "read_file",
          toolSafeSummary: { path: "two.go" },
        },
      ],
    });

    expect(projected.content).toMatchObject([
      {
        type: "tool-call",
        toolCallId: "invocation-one:call-1",
        args: { status: "completed", safeSummary: { path: "one.go" } },
      },
      {
        type: "tool-call",
        toolCallId: "invocation-two:call-1",
        args: { status: "running", safeSummary: { path: "two.go" } },
      },
    ]);
  });

  it("技能活动在 API 消息转换后仍保留独立类别与动作", () => {
    const projected = toThreadMessage({
      id: "skill-message",
      role: "assistant",
      status: "completed",
      displayParts: [
        {
          type: "tool",
          toolCallId: "skill-1",
          toolCategory: "skill",
          toolAction: "use_skill",
          toolLabel: "使用技能 ally",
          toolStatus: "completed",
        },
      ],
    });
    expect(projected.content[0]).toMatchObject({
      type: "tool-call",
      toolName: "skill",
      args: { category: "skill", action: "use_skill", label: "使用技能 ally" },
    });
  });

  it("活跃执行期间也会并入 REST 返回的第二轮消息", () => {
    const current: AllyChatMessage[] = [
      { id: "user-1", role: "user", content: "第一轮", updatedAt: 1 },
    ];
    const authoritative: AllyChatMessage[] = [
      ...current,
      { id: "user-2", role: "user", content: "你好", updatedAt: 2 },
      {
        id: "assistant-2",
        role: "assistant",
        content: "",
        status: "pending",
        updatedAt: 2,
      },
    ];

    expect(mergeAuthoritativeMessages(current, authoritative)).toEqual(
      authoritative,
    );
  });

  it("保留尚未被后端权威消息对账的乐观消息", () => {
    const optimistic: AllyChatMessage = {
      id: "optimistic:user:1",
      role: "user",
      content: "立即显示",
      optimistic: true,
      updatedAt: 2,
    };

    expect(
      mergeAuthoritativeMessages(
        [
          { id: "user-1", role: "user", content: "第一轮", updatedAt: 1 },
          optimistic,
        ],
        [{ id: "user-1", role: "user", content: "第一轮", updatedAt: 1 }],
      ),
    ).toContainEqual(optimistic);
  });

  it("POST 返回稳定 ID 后仍保留尚未进入 REST 投影的消息", () => {
    const acknowledged: AllyChatMessage = {
      id: "user-2",
      role: "user",
      content: "立即显示",
      executionId: "execution-2",
      optimistic: false,
      updatedAt: 2,
    };

    expect(
      mergeAuthoritativeMessages(
        [
          { id: "user-1", role: "user", content: "第一轮", updatedAt: 1 },
          acknowledged,
        ],
        [{ id: "user-1", role: "user", content: "第一轮", updatedAt: 1 }],
      ),
    ).toContainEqual(acknowledged);
  });

  it("稳定 ID 对账后由 REST 权威附件替换乐观版本", () => {
    const result = mergeAuthoritativeMessages(
      [
        {
          id: "user-2",
          role: "user",
          content: "请查看附件",
          optimistic: true,
          updatedAt: 20,
          attachments: [
            {
              name: "draft.txt",
              path: "/tmp/draft.txt",
              managed: false,
            },
          ],
        },
      ],
      [
        {
          id: "user-2",
          role: "user",
          content: "请查看附件",
          updatedAt: 10,
          attachments: [
            {
              name: "draft.txt",
              path: "attachments/user-2/draft.txt",
              managed: true,
            },
          ],
        },
      ],
    );

    expect(result[0]?.attachments?.[0]).toMatchObject({
      path: "attachments/user-2/draft.txt",
      managed: true,
    });
  });

  it("按稳定消息 ID 合并 SSE 创建事件且不会重复插入", () => {
    const event = {
      type: "message.user_created",
      recordedAt: 20,
      message: {
        id: "user-2",
        role: "user",
        content: "你好",
        executionId: "execution-2",
        updatedAt: 20,
      },
    } as unknown as ChatEvent;
    const existing: AllyChatMessage[] = [
      {
        id: "user-2",
        role: "user",
        content: "你好",
        optimistic: true,
        updatedAt: 10,
      },
    ];

    const result = applyChatEvent(existing, event);

    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      id: "user-2",
      executionId: "execution-2",
      optimistic: false,
      updatedAt: 20,
    });
  });

  it("流式快照只替换目标消息引用，避免历史消息跟随增量重复渲染", () => {
    const history = Array.from({ length: 50 }, (_, index) => ({
      id: `history-${index}`,
      role: index % 2 === 0 ? "user" : "assistant",
      content: `历史消息 ${index}`,
      status: index % 2 === 0 ? undefined : "completed",
      revision: 1,
    })) satisfies AllyChatMessage[];
    const running: AllyChatMessage = {
      id: "assistant-running",
      role: "assistant",
      status: "running",
      revision: 1,
      displayParts: [{ type: "text", text: "第一段" }],
    };
    const current = [...history, running];

    const result = applyChatEvent(current, {
      type: "runtime.event",
      message: {
        ...running,
        revision: 2,
        displayParts: [{ type: "text", text: "第一段，第二段" }],
      },
    } as ChatEvent);

    expect(result).not.toBe(current);
    expect(result.at(-1)).not.toBe(running);
    expect(result.slice(0, -1)).toEqual(history);
    history.forEach((message, index) => {
      expect(result[index]).toBe(message);
    });
  });

  it("团队主会话可以丢弃内部系统输入而不影响普通用户消息", () => {
    const visible = (message: AllyChatMessage) =>
      message.role !== "user" || message.actor === "user";
    const systemResult = applyChatEvent(
      [],
      {
        type: "message.user_created",
        message: {
          id: "internal-announce",
          role: "user",
          actor: "system",
          content: "[System Message] Team member completed task",
        },
      } as ChatEvent,
      visible,
    );
    const userResult = applyChatEvent(
      [],
      {
        type: "message.user_created",
        message: {
          id: "user-message",
          role: "user",
          actor: "user",
          content: "继续补充要求",
        },
      } as ChatEvent,
      visible,
    );

    expect(systemResult).toEqual([]);
    expect(userResult).toHaveLength(1);
  });

  it("终态完整快照不会被较旧的部分内容覆盖", () => {
    const complete = applyChatEvent(
      [
        {
          id: "assistant-1",
          role: "assistant",
          status: "running",
          revision: 8,
          displayParts: [{ type: "text", text: "完整回答" }],
        },
      ],
      {
        sequence: 12,
        type: "execution.completed",
        message: {
          id: "assistant-1",
          role: "assistant",
          status: "completed",
          revision: 9,
          displayParts: [{ type: "text", text: "完整回答" }],
        },
      } as ChatEvent,
    );

    const afterStaleCheckpoint = applyChatEvent(complete, {
      sequence: 11,
      type: "runtime.event",
      message: {
        id: "assistant-1",
        role: "assistant",
        status: "running",
        revision: 8,
        displayParts: [{ type: "text", text: "只显示一半" }],
      },
    } as ChatEvent);

    expect(afterStaleCheckpoint[0]).toMatchObject({
      status: "completed",
      revision: 9,
      displayParts: [{ type: "text", text: "完整回答" }],
    });
  });

  it("从用户消息 SSE 创建事件恢复有序附件引用", () => {
    const event = {
      type: "message.user_created",
      recordedAt: 30,
      message: {
        id: "user-3",
        role: "user",
        content: "附件",
        updatedAt: 30,
        attachments: [
          {
            name: "one.txt",
            path: "one.txt",
            managed: false,
          },
          {
            name: "two.txt",
            path: "attachments/user-3/two.txt",
            managed: true,
          },
        ],
      },
    } as unknown as ChatEvent;

    const result = applyChatEvent([], event);

    expect(
      result[0]?.attachments?.map((attachment) => attachment.name),
    ).toEqual(["one.txt", "two.txt"]);
  });

  it("把 NeuxBot 终态映射为 assistant-ui 消息状态", () => {
    const completed = toThreadMessage({
      id: "assistant-1",
      role: "assistant",
      content: "完成",
      status: "completed",
    });
    const failed = toThreadMessage({
      id: "assistant-2",
      role: "assistant",
      content: "",
      status: "failed",
      errorMessage:
        "400 Invalid max_tokens value, the valid range is [1, 393216]",
    });

    expect(completed.status).toEqual({ type: "complete", reason: "stop" });
    expect(failed.status).toEqual({
      type: "incomplete",
      reason: "error",
      error: {
        message: "400 Invalid max_tokens value, the valid range is [1, 393216]",
      },
    });
  });

  it("失败作为消息元数据展示，不写入可复制正文", () => {
    const failed = toThreadMessage({
      id: "assistant-plan-upgrade",
      role: "assistant",
      content: "",
      status: "failed",
      errorCode: "PLAN_UPGRADE_REQUIRED",
      errorMessage: "Internal error",
    });

    expect(failed.content).toEqual([]);
    expect(failed.metadata?.custom).toMatchObject({
      failureMessage: "Internal error",
      retryable: false,
    });
    expect(failed.status).toEqual({
      type: "incomplete",
      reason: "error",
      error: {
        message: "Internal error",
      },
    });
  });

  it("系统故障同样通过消息错误元数据展示 CLI 原始诊断", () => {
    const failed = toThreadMessage({
      id: "assistant-process-exited",
      role: "assistant",
      content: "",
      status: "failed",
      errorCode: "PROCESS_EXITED",
      errorMessage: "child process exited with private diagnostics",
    });

    expect(failed.content).toEqual([]);
    expect(failed.metadata?.custom).toMatchObject({
      failureMessage: "child process exited with private diagnostics",
      retryable: false,
    });
    expect(failed.status).toEqual({
      type: "incomplete",
      reason: "error",
      error: {
        message: "child process exited with private diagnostics",
      },
    });
  });

  it("Assistant 正文只消费当前 displayParts 契约", () => {
    const projected = toThreadMessage({
      id: "assistant-current-contract",
      role: "assistant",
      content: "不再作为展示兼容来源",
      status: "completed",
      displayParts: [],
    });

    expect(projected.content).toEqual([]);
  });

  it.each(["running", "completed"] as const)(
    "%s 消息的纯空白正文、过程说明和思考不生成展示片段",
    (status) => {
      const displayParts = (
        ["text", "process_commentary", "reasoning"] as const
      ).flatMap((type) =>
        [undefined, "", "\n\r\n", " \t\n\u3000"].map((text) => ({
          type,
          text,
        })),
      );
      const message: AllyChatMessage = {
        id: "assistant-whitespace",
        role: "assistant",
        status,
        displayParts,
      };
      const originalParts = structuredClone(displayParts);

      expect(toThreadMessage(message).content).toEqual([]);
      expect(message.displayParts).toEqual(originalParts);
    },
  );

  it("包含正文的片段保留首尾换行、段落分隔与代码缩进", () => {
    const text = "\n\n第一段\n\n```python\n    print('你好')\n```\n";
    const projected = toThreadMessage({
      id: "assistant-formatted",
      role: "assistant",
      status: "completed",
      displayParts: [
        { type: "text", text },
        { type: "process_commentary", text },
        { type: "reasoning", text },
      ],
    });

    expect(projected.content).toEqual([
      { type: "text", text },
      { type: "data", name: "chat.process_commentary", data: { text } },
      { type: "reasoning", text },
    ]);
  });

  it("流式片段从纯换行增长为正文时，重新展示完整原文", () => {
    const message: AllyChatMessage = {
      id: "assistant-growing",
      role: "assistant",
      status: "running",
      displayParts: [{ type: "text", text: "\n\n" }],
    };
    expect(toThreadMessage(message).content).toEqual([]);

    const text = "\n\n第一段\n\n第二段\n";
    const updated: AllyChatMessage = {
      ...message,
      displayParts: [{ type: "text", text }],
    };
    expect(toThreadMessage(updated).content).toEqual([{ type: "text", text }]);
    expect(message.displayParts?.[0]?.text).toBe("\n\n");
  });

  it("直接消费后端跨 Runtime 消息展示投影并保留工作过程顺序", () => {
    const projected = toThreadMessage({
      id: "assistant-display-parts",
      role: "assistant",
      content: "完成",
      status: "completed",
      displayParts: [
        { type: "process_commentary", text: "先检查。" },
        { type: "reasoning", text: "需要读取项目说明" },
        {
          type: "tool",
          toolCallId: "tool-1",
          toolCategory: "file_read",
          toolAction: "read_file",
          toolLabel: "读取 README.md",
          toolLoadingLabel: "正在读取 README.md…",
          toolStatus: "completed",
          toolSafeSummary: {
            path: "README.md",
          },
        },
        { type: "text", text: "完成" },
      ],
    } as AllyChatMessage);

    expect(projected.content).toEqual([
      {
        type: "data",
        name: "chat.process_commentary",
        data: { text: "先检查。" },
      },
      { type: "reasoning", text: "需要读取项目说明" },
      {
        type: "tool-call",
        toolCallId: "tool-1",
        toolName: "file_read",
        args: {
          category: "file_read",
          action: "read_file",
          label: "读取 README.md",
          loadingLabel: "正在读取 README.md…",
          safeSummary: {
            path: "README.md",
          },
          status: "completed",
        },
        argsText: "{}",
        result: {},
        isError: false,
      },
      { type: "text", text: "完成" },
    ]);
  });

  it("从最近一条助手消息提取上下文占用", () => {
    expect(
      latestContextMetrics([
        { id: "user-1", role: "user", content: "hi" },
        {
          id: "assistant-1",
          role: "assistant",
          content: "ok",
          runMetrics: {
            contextUsedTokens: 17_400,
            contextWindowTokens: 1_000_000,
          },
        },
      ] as AllyChatMessage[]),
    ).toEqual({
      contextUsedTokens: 17_400,
      contextWindowTokens: 1_000_000,
    });
  });

  it("把执行级文件变更放入消息元数据，供终态卡片稳定展示", () => {
    const projected = toThreadMessage({
      id: "assistant-file-changes",
      role: "assistant",
      content: "完成",
      status: "completed",
      fileChanges: [{ path: "README.md", addedLines: 3, deletedLines: 1 }],
    } as AllyChatMessage);

    expect(projected.metadata?.custom).toMatchObject({
      isTerminal: true,
      fileChanges: [{ path: "README.md", addedLines: 3, deletedLines: 1 }],
    });
  });

  it("把统一执行耗时与 Token 摘要放入消息 metadata", () => {
    const projected = toThreadMessage({
      id: "assistant-metrics",
      role: "assistant",
      content: "完成",
      status: "completed",
      runMetrics: {
        startedAt: 100,
        durationMs: 17_700,
        inputTokens: 234,
        outputTokens: 1482,
        cacheReadTokens: 40_448,
      },
    } as AllyChatMessage);

    expect(projected.metadata?.custom).toEqual({
      isTerminal: true,
      runMetrics: {
        startedAt: 100,
        durationMs: 17_700,
        inputTokens: 234,
        outputTokens: 1482,
        cacheReadTokens: 40_448,
      },
    });
  });

  it("不会从公开展示投影向 assistant-ui 泄露原始工具命令或结果", () => {
    const projected = toThreadMessage({
      id: "assistant-safe-tool",
      role: "assistant",
      content: "完成",
      status: "completed",
      displayParts: [
        {
          type: "tool",
          toolCallId: "tool-1",
          toolCategory: "command",
          toolStatus: "completed",
          toolSafeSummary: {
            addedLines: 2,
            deletedLines: 1,
          },
        },
      ],
    } as unknown as AllyChatMessage);

    const encoded = JSON.stringify(projected.content);
    expect(encoded).toContain("safeSummary");
    expect(encoded).toContain("addedLines");
    expect(encoded).not.toContain("rm -rf");
    expect(encoded).not.toContain("contents");
    expect(encoded).not.toContain("README secret");
  });

  it("公开事件用安全消息快照更新 Assistant 消息", () => {
    const event = {
      type: "runtime.event",
      sequence: 22,
      recordedAt: 30,
      message: {
        id: "assistant-2",
        role: "assistant",
        executionId: "execution-2",
        content: "",
        status: "running",
        updatedAt: 30,
        displayParts: [
          {
            type: "tool",
            toolCallId: "tool-1",
            toolCategory: "file_search",
            toolStatus: "running",
            toolSafeSummary: { path: "src" },
          },
        ],
      },
    } as unknown as ChatEvent;
    const current: AllyChatMessage[] = [
      {
        id: "assistant-2",
        role: "assistant",
        executionId: "execution-2",
        content: "",
        status: "running",
      },
    ];

    const once = applyChatEvent(current, event);
    const twice = applyChatEvent(once, event);
    const projectedMessage = twice[0];
    if (!projectedMessage) throw new Error("projected message is missing");
    expect(projectedMessage).toMatchObject({
      id: "assistant-2",
      status: "running",
      optimistic: false,
      updatedAt: 30,
    });
    expect(toThreadMessage(projectedMessage).content).toEqual([
      {
        type: "tool-call",
        toolCallId: "tool-1",
        toolName: "file_search",
        args: {
          category: "file_search",
          action: "other",
          safeSummary: { path: "src" },
          status: "running",
        },
        argsText: "{}",
        isError: false,
      },
    ]);
  });

  it("用消息 revision 保留领先于 REST 的流式快照并忽略乱序事件", () => {
    const live: AllyChatMessage = {
      id: "assistant-live",
      role: "assistant",
      content: "正在流式输出",
      revision: 12,
      updatedAt: 120,
    };
    const staleRest: AllyChatMessage = {
      id: "assistant-live",
      role: "assistant",
      content: "正在流式",
      revision: 10,
      updatedAt: 200,
    };

    expect(mergeAuthoritativeMessages([live], [staleRest])).toEqual([live]);

    const staleEvent = {
      sequence: 11,
      message: staleRest,
    } as ChatEvent;
    expect(applyChatEvent([live], staleEvent)).toEqual([live]);
  });

  it("公开终态事件通过完整消息快照更新执行状态", () => {
    const event = {
      type: "execution.failed",
      recordedAt: 40,
      executionStatus: "failed",
      errorCode: "runtime_failed",
      errorMessage: "执行失败",
      message: {
        id: "assistant-2",
        role: "assistant",
        executionId: "execution-2",
        content: "",
        status: "failed",
        errorCode: "runtime_failed",
        errorMessage: "执行失败",
        updatedAt: 40,
      },
    } as unknown as ChatEvent;
    const result = applyChatEvent(
      [
        {
          id: "assistant-2",
          role: "assistant",
          executionId: "execution-2",
          content: "",
          status: "running",
        },
      ],
      event,
    );

    expect(result[0]).toMatchObject({
      status: "failed",
      errorCode: "runtime_failed",
      errorMessage: "执行失败",
      updatedAt: 40,
    });
  });
});

it("历史插件引用保留原始版本和修订，不从当前资源覆盖", () => {
  const frozen = {
    id: "plugin-office",
    name: "飞书办公",
    versionId: "version-old",
    version: "1.0.0",
    configurationRevision: 2,
    componentIds: ["component"],
  };
  const projected = toThreadMessage({
    id: "message",
    role: "user",
    content: "整理文档",
    plugins: [frozen],
  });
  expect(projected.metadata?.custom?.plugins).toEqual([frozen]);
});
