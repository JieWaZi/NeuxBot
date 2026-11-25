import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest";
import type {
  ReasoningMessagePartProps,
  ThreadAssistantMessagePart,
  ToolCallMessagePart,
  ToolCallMessagePartProps,
} from "@assistant-ui/react";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  AssistantMessage as PureAssistantMessage,
  ChatMessageList,
  ComposerContextUsage,
  MessageRunStatus as PureMessageRunStatus,
  ReasoningPart,
  RunMetricsBadge,
  ToolActivityRow as PureToolActivityRow,
  WorkProcess as PureWorkProcess,
} from "./chat-thread";
import {
  CHAT_PROCESS_COMMENTARY_PART,
  type BotChatMessage,
  type ChatRunMetrics,
} from "../runtime/chat-projection";
import {
  ChatMessageProjection,
  isTerminalMessage,
  type ChatToolGroup,
} from "./chat-message-nodes";
import type { ComponentProps, ReactNode } from "react";

type MockMessageState = {
  id: string;
  role: "assistant";
  isLast?: boolean;
  createdAt?: Date;
  metadata?: { custom?: Record<string, unknown> };
  status?:
    | { type: "running" }
    | { type: "complete"; reason: "stop" }
    | { type: "incomplete"; reason: "cancelled" | "error" };
  content: ThreadAssistantMessagePart[];
};

const { messageState } = vi.hoisted(() => ({
  messageState: {
    messages: undefined as
      { id: string; role: "assistant" | "user" }[] | undefined,
    current: {
      role: "assistant",
      id: "message-1",
      status: { type: "complete", reason: "stop" },
      content: [] as ThreadAssistantMessagePart[],
    } as MockMessageState,
  },
}));

type DisplayPart = NonNullable<BotChatMessage["displayParts"]>[number];
function canonicalPart(
  part: ThreadAssistantMessagePart,
  index: number,
): DisplayPart {
  if (part.type === "tool-call")
    return {
      id: part.toolCallId || String(index),
      type: "tool",
      toolCategory: part.args.category as DisplayPart["toolCategory"],
      toolAction: part.args.action as DisplayPart["toolAction"],
      toolStatus: part.isError
        ? "failed"
        : part.result !== undefined
          ? "completed"
          : (part.args.status as DisplayPart["toolStatus"]),
      toolLabel:
        typeof part.args.label === "string" ? part.args.label : undefined,
      toolLoadingLabel:
        typeof part.args.loadingLabel === "string"
          ? part.args.loadingLabel
          : undefined,
      toolSafeSummary: part.args.safeSummary as DisplayPart["toolSafeSummary"],
      toolApprovalResult: part.args
        .approvalResult as DisplayPart["toolApprovalResult"],
    };
  if (part.type === "data")
    return {
      id: String(index),
      type: "process_commentary",
      text: (part.data as { text: string }).text,
    };
  return {
    id: String(index),
    type: part.type === "reasoning" ? "reasoning" : "text",
    text: "text" in part ? part.text : "",
  };
}
function canonicalMessage(): BotChatMessage {
  const source = messageState.current;
  const meta = source.metadata?.custom;
  return {
    id: source.id,
    role: source.role,
    createdAt: source.createdAt?.getTime(),
    status:
      source.status?.type === "complete"
        ? "completed"
        : source.status?.type === "incomplete"
          ? source.status.reason === "cancelled"
            ? "canceled"
            : "failed"
          : "running",
    displayParts: source.content.map(canonicalPart),
    runMetrics: meta?.runMetrics as ChatRunMetrics | undefined,
    executionId:
      typeof meta?.executionId === "string" ? meta.executionId : source.id,
    errorMessage:
      typeof meta?.failureMessage === "string"
        ? meta.failureMessage
        : undefined,
    errorCode:
      typeof meta?.failureCode === "string" ? meta.failureCode : undefined,
    errorMessageSource:
      meta?.errorMessageSource === "public" ? "public" : undefined,
  };
}
function AssistantMessage(
  props: Omit<
    ComponentProps<typeof PureAssistantMessage>,
    "view" | "latest" | "last"
  >,
) {
  const view = new ChatMessageProjection().update([canonicalMessage()])[0];
  if (!view) throw new Error("missing test message");
  const latest = messageState.messages?.at(-1)?.id ?? view.message.id;
  return (
    <PureAssistantMessage
      {...props}
      view={view}
      latest={latest === view.message.id}
      last={messageState.current.isLast !== false}
    />
  );
}
function WorkProcess({
  indices,
  children,
}: {
  indices: readonly number[];
  children?: ReactNode;
  onOpenFileChange?: unknown;
}) {
  const message = canonicalMessage();
  const group: ChatToolGroup = {
    key: "test-tools",
    kind: "tools",
    nodes: indices.flatMap((index) => {
      const part = message.displayParts?.[index];
      return part?.type === "tool" ? [{ key: String(index), part }] : [];
    }),
  };
  return (
    <PureWorkProcess
      group={group}
      terminal={isTerminalMessage(message)}
      current={
        !isTerminalMessage(message) &&
        indices.at(-1) === (message.displayParts?.length ?? 0) - 1
      }
    >
      {children}
    </PureWorkProcess>
  );
}
function ToolActivityRow(props: ToolCallMessagePartProps) {
  const part = canonicalPart({ ...props, type: "tool-call" }, 0);
  return (
    <PureToolActivityRow
      part={part}
      terminal={isTerminalMessage(canonicalMessage())}
    />
  );
}
function MessageRunStatus(props: { showUsageDetails?: boolean }) {
  return <PureMessageRunStatus {...props} message={canonicalMessage()} />;
}

afterEach(() => {
  vi.useRealTimers();
  messageState.messages = undefined;
  messageState.current = {
    role: "assistant",
    id: "message-1",
    status: { type: "complete", reason: "stop" },
    content: [],
  };
});

function processPart(text: string): ThreadAssistantMessagePart {
  return {
    type: "data",
    name: CHAT_PROCESS_COMMENTARY_PART,
    data: { text },
  };
}

function toolPart(
  category: string,
  status: "running" | "completed" | "failed" = "completed",
): ToolCallMessagePart {
  return {
    type: "tool-call",
    toolCallId: "tool-" + category,
    toolName: category,
    args: {
      category,
      action: "other",
      status,
      safeSummary: {
        path: "src/chat.ts",
        addedLines: 0,
        deletedLines: 0,
      },
    },
    argsText: "{}",
    ...(status === "running" ? {} : { result: {} }),
    isError: status === "failed",
  };
}

describe("chat work process", () => {
  it("失败回复成为历史后自动收起，原失败详情和正文阅读标记只在展开时出现", () => {
    messageState.current = {
      role: "assistant",
      id: "failed-message",
      createdAt: new Date(0),
      isLast: true,
      content: [],
      status: { type: "incomplete", reason: "error" },
      metadata: {
        custom: {
          executionId: "failed-execution",
          failureMessage: "模型服务暂时不可用",
          errorMessageSource: "public",
        },
      },
    };
    const { rerender, container } = render(<AssistantMessage name="NeuxBot" />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    messageState.current = { ...messageState.current, isLast: false };
    rerender(<AssistantMessage name="NeuxBot" />);
    expect(screen.queryByText("模型服务暂时不可用")).not.toBeInTheDocument();
    expect(container.querySelector("[data-chat-readable-body]")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "展开失败详情" }));
    expect(screen.getByText("模型服务暂时不可用")).toBeInTheDocument();
    expect(container.querySelector("[data-chat-readable-body]")).not.toBeNull();
  });

  it("工作流工具摘要保留读取、校验和保存动作，重复读取只合并一次", () => {
    const labels = ["读取工作流", "检查工作流", "读取工作流", "保存工作流"];
    messageState.current.content = labels.map((label, index) => {
      const part = toolPart("mcp");
      return part.type === "tool-call"
        ? { ...part, toolCallId: String(index), args: { ...part.args, label } }
        : part;
    });
    render(
      <WorkProcess indices={[0, 1, 2, 3]}>
        <div>详细过程</div>
      </WorkProcess>,
    );
    expect(
      screen.getByRole("button", {
        name: "已读取工作流、检查工作流、保存工作流",
      }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  it("终态未收到工具结果时，折叠摘要显示中断而非已完成", () => {
    messageState.current.content = [toolPart("command", "running")];
    render(
      <WorkProcess indices={[0]}>
        <div>详细过程</div>
      </WorkProcess>,
    );
    const toggle = screen.getByRole("button", { name: "已中断：执行命令" });
    expect(toggle).toHaveAttribute("aria-busy", "false");
    expect(toggle).not.toHaveAttribute("data-running-shimmer");
    expect(
      screen.queryByRole("button", { name: "已执行命令" }),
    ).not.toBeInTheDocument();
  });
  it("摘要按首次出现顺序合并工具类别，不展示计数和过程消息", () => {
    messageState.current = {
      role: "assistant",
      id: "message-1",
      status: { type: "complete", reason: "stop" },
      content: [
        toolPart("command", "failed"),
        toolPart("file_search"),
        toolPart("file_read"),
        toolPart("file_modify"),
        toolPart("file_read"),
      ],
    };

    render(
      <WorkProcess indices={[0, 1, 2, 3, 4]}>
        <div>过程内容</div>
      </WorkProcess>,
    );

    const toggle = screen.getByRole("button", {
      name: "已执行命令搜索读取编辑文件",
    });
    expect(toggle).not.toHaveAccessibleName(/工具调用|条消息/);
    expect(toggle).not.toHaveAccessibleName(/src\/chat/);
    const [icon, arrow] = toggle.querySelectorAll("svg");
    expect(toggle.querySelectorAll("svg")).toHaveLength(2);
    expect(icon).toHaveClass("lucide-terminal");
    expect(arrow).toHaveClass(
      "lucide-chevron-right",
      "opacity-0",
      "group-hover/tool-process:opacity-100",
      "group-focus-visible/tool-process:opacity-100",
    );
    expect(toggle).toHaveClass("group/tool-process");
    expect(toggle).not.toHaveClass("group");
    expect(toggle.lastElementChild).toBe(arrow);
    expect(toggle.closest("section")).not.toHaveClass("border", "bg-muted");
    fireEvent.click(toggle);
    expect(arrow).toHaveClass("opacity-100", "rotate-90");
  });

  it("失败仅在对应工具行展示，摘要合并同类活动", () => {
    messageState.current.content = [
      {
        ...toolPart("mcp", "failed"),
        args: {
          category: "mcp",
          status: "failed",
          label: "连接 MCP 服务 calendar",
        },
      },
      {
        ...toolPart("mcp"),
        toolCallId: "success",
        args: { category: "mcp", status: "completed", label: "查询日程" },
      },
    ];
    render(
      <WorkProcess indices={[0, 1]}>
        <div>详细过程</div>
      </WorkProcess>,
    );
    expect(
      screen.getByRole("button", {
        name: "已连接 MCP 服务 calendar、查询日程",
      }),
    ).toBeInTheDocument();
  });

  it("技能活动使用星光图标，MCP 活动使用连接器图标", () => {
    messageState.current.content = [toolPart("skill")];
    const { rerender } = render(
      <WorkProcess indices={[0]}>
        <div>详细过程</div>
      </WorkProcess>,
    );
    expect(
      screen.getByRole("button", { name: "已使用技能" }).querySelector("svg"),
    ).toHaveClass("lucide-sparkles");
    messageState.current.content = [toolPart("mcp")];
    rerender(
      <WorkProcess indices={[0]}>
        <div>详细过程</div>
      </WorkProcess>,
    );
    expect(
      screen
        .getByRole("button", { name: "已调用外部服务" })
        .querySelector("svg"),
    ).toHaveClass("lucide-cable");
  });

  it("运行中的当前工具段默认收起并闪光，完成后停止", () => {
    messageState.current = {
      role: "assistant",
      id: "message-1",
      status: { type: "running" },
      content: [toolPart("file_read", "running")],
    };

    const { rerender } = render(
      <WorkProcess indices={[0]}>
        <div>过程内容</div>
      </WorkProcess>,
    );
    const toggle = screen.getByRole("button", { name: "正在读取文件" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveAttribute("data-running-shimmer", "true");
    expect(screen.queryByText("过程内容")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    messageState.current = {
      ...messageState.current,
      status: { type: "complete", reason: "stop" },
      content: [toolPart("file_read", "completed")],
    };
    rerender(
      <WorkProcess indices={[0]}>
        <div>过程内容</div>
      </WorkProcess>,
    );
    expect(screen.getByRole("button", { name: "已读取文件" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(toggle).not.toHaveAttribute("data-running-shimmer");
  });

  it("后续出现过程说明时，先前的工具段停止闪光", () => {
    messageState.current = {
      role: "assistant",
      id: "member-running-message",
      status: { type: "running" },
      content: [toolPart("file_read", "completed"), processPart("继续检查")],
    };

    render(
      <WorkProcess indices={[0]}>
        <div>过程内容</div>
      </WorkProcess>,
    );

    const toggle = screen.getByRole("button", { name: "已读取文件" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).not.toHaveAttribute("data-running-shimmer");
    expect(screen.queryByText("过程内容")).not.toBeInTheDocument();
  });

  it("同一消息内的多个折叠段各自限定箭头悬停范围", () => {
    messageState.current = {
      role: "assistant",
      id: "message-hover-scope",
      status: { type: "complete", reason: "stop" },
      content: [
        toolPart("file_read"),
        processPart("继续处理"),
        toolPart("command"),
      ],
    };

    render(
      <div className="group">
        <WorkProcess indices={[0]} />
        <WorkProcess indices={[2]} />
      </div>,
    );

    for (const toggle of [
      screen.getByRole("button", { name: "已读取文件" }),
      screen.getByRole("button", { name: "已执行命令" }),
    ]) {
      expect(toggle).toHaveClass("group/tool-process");
      expect(toggle).not.toHaveClass("group");
      expect(toggle.lastElementChild).toHaveClass(
        "opacity-0",
        "group-hover/tool-process:opacity-100",
      );
      expect(toggle.lastElementChild).not.toHaveClass(
        "group-hover:opacity-100",
      );
    }
  });

  it("终态工具组不再从工具摘要生产文件成果", async () => {
    const onOpenFile = vi.fn();
    messageState.current = {
      role: "assistant",
      id: "message-process-file-changes",
      status: { type: "complete", reason: "stop" },
      metadata: {
        custom: {
          isTerminal: true,
          fileChanges: [
            { path: "projects/Desktop/note_one.txt", addedLines: 2 },
            { path: "projects/Desktop/note_two.txt", addedLines: 2 },
          ],
        },
      },
      content: [toolPart("file_modify")],
    };

    render(
      <WorkProcess indices={[0]} onOpenFileChange={onOpenFile}>
        <div>修改文件</div>
      </WorkProcess>,
    );

    const toggle = screen.getByRole("button", { name: "已编辑文件" });
    expect(screen.queryByText("已编辑 2 个文件")).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.getByText("修改文件")).toBeVisible();
    expect(screen.queryByText("已编辑 2 个文件")).not.toBeInTheDocument();
    expect(onOpenFile).not.toHaveBeenCalled();
  });

  it("多个工具组均不再承载文件成果汇总", async () => {
    messageState.current = {
      role: "assistant",
      id: "message-multiple-tool-groups",
      status: { type: "complete", reason: "stop" },
      metadata: {
        custom: {
          isTerminal: true,
          fileChanges: [{ path: "projects/Desktop/note.txt", addedLines: 1 }],
        },
      },
      content: [
        toolPart("file_modify"),
        processPart("继续验证"),
        toolPart("command"),
      ],
    };

    render(
      <>
        <WorkProcess indices={[0]}>
          <div>编辑明细</div>
        </WorkProcess>
        <WorkProcess indices={[2]}>
          <div>命令明细</div>
        </WorkProcess>
      </>,
    );

    fireEvent.click(screen.getByRole("button", { name: "已编辑文件" }));
    expect(screen.getByText("编辑明细")).toBeVisible();
    expect(screen.queryByText("已编辑 1 个文件")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "已执行命令" }));
    expect(screen.getByText("命令明细")).toBeVisible();
    expect(screen.queryByText("已编辑 1 个文件")).not.toBeInTheDocument();
  });

  it("运行中手动展开后，完成时保持展开", () => {
    messageState.current = {
      role: "assistant",
      id: "message-1",
      status: { type: "running" },
      content: [toolPart("file_read", "running")],
    };
    const { rerender } = render(
      <WorkProcess indices={[0]}>
        <div>过程内容</div>
      </WorkProcess>,
    );
    fireEvent.click(screen.getByRole("button", { name: "正在读取文件" }));
    expect(screen.getByText("过程内容")).toBeVisible();

    messageState.current = {
      ...messageState.current,
      status: { type: "complete", reason: "stop" },
      content: [toolPart("file_read", "completed")],
    };
    rerender(
      <WorkProcess indices={[0]}>
        <div>过程内容</div>
      </WorkProcess>,
    );
    expect(screen.getByRole("button", { name: "已读取文件" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByText("过程内容")).toBeVisible();
  });

  it("过程内容按需挂载，快速反向切换后保持最后状态", () => {
    messageState.current = {
      role: "assistant",
      id: "toggle-message",
      status: { type: "complete", reason: "stop" },
      content: [toolPart("file_read", "completed")],
    };
    const renderDetail = vi.fn();
    function Detail() {
      renderDetail();
      return <div>详细过程</div>;
    }
    render(
      <WorkProcess indices={[0]}>
        <Detail />
      </WorkProcess>,
    );
    expect(renderDetail).not.toHaveBeenCalled();
    const trigger = screen.getByRole("button", { name: "已读取文件" });
    fireEvent.click(trigger);
    expect(screen.getByText("详细过程")).toBeVisible();
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("详细过程")).toBeVisible();
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("详细过程")).not.toBeInTheDocument();
  });

  it("reasoning 长内容默认四行截断并支持键盘展开", () => {
    render(
      <ReasoningPart
        {...({
          type: "reasoning",
          text: "思考内容".repeat(40),
          status: { type: "complete" },
        } satisfies ReasoningMessagePartProps)}
      />,
    );

    const paragraph = screen.getByText(/思考内容/);
    expect(screen.getByText("思考过程")).toBeInTheDocument();
    expect(paragraph).toHaveClass(
      "line-clamp-4",
      "type-chat",
      "text-foreground",
    );
    expect(paragraph.parentElement).toHaveClass(
      "border-dashed",
      "bg-surface-tertiary",
    );
    fireEvent.keyDown(screen.getByRole("button", { name: "展开思考" }), {
      key: "Enter",
    });
    fireEvent.click(screen.getByRole("button", { name: "展开思考" }));
    expect(paragraph).not.toHaveClass("line-clamp-4");
    expect(screen.getByRole("button", { name: "收起" })).toBeInTheDocument();
  });

  it("思考过程只移除开头换行，保留正文中的空行和结尾换行", () => {
    render(
      <ReasoningPart
        {...({
          type: "reasoning",
          text: "\n\r\n第一行\n\n第二行\n",
          status: { type: "complete" },
        } satisfies ReasoningMessagePartProps)}
      />,
    );

    expect(screen.getByText(/第一行/).textContent).toBe("第一行\n\n第二行\n");
  });

  it("失败命令仅在对应行标淡红色，成功行保持普通样式", () => {
    const props = {
      type: "tool-call" as const,
      toolCallId: "failed-command",
      toolName: "Bash",
      args: {
        category: "command",
        status: "failed",
        label: "执行命令 · false",
      },
      argsText: "{}",
      isError: true,
      result: { output: "" },
      status: { type: "complete" as const },
      addResult: vi.fn(),
      resume: vi.fn(),
      respondToApproval: vi.fn(),
    } satisfies ToolCallMessagePartProps;
    const { container, rerender } = render(<ToolActivityRow {...props} />);
    expect(container.firstElementChild).toHaveClass(
      "bg-destructive/10",
      "text-destructive",
    );
    expect(screen.getByText("失败：")).toHaveClass("sr-only");
    rerender(
      <ToolActivityRow
        {...props}
        isError={false}
        args={{ ...props.args, status: "completed" }}
      />,
    );
    expect(container.firstElementChild).not.toHaveClass(
      "bg-destructive/10",
      "text-destructive",
    );
  });

  it.each(["error", "cancelled"] as const)(
    "%s 终态中没有结果的工具显示中断，保留已完成工具事实",
    (reason) => {
      messageState.current = {
        ...messageState.current,
        status: { type: "incomplete", reason },
      };
      const props = {
        type: "tool-call" as const,
        toolCallId: "unfinished",
        toolName: "Bash",
        args: {
          category: "command",
          status: "running",
          label: "执行命令",
          loadingLabel: "正在执行命令…",
        },
        argsText: "{}",
        status: { type: "running" as const },
        addResult: vi.fn(),
        resume: vi.fn(),
        respondToApproval: vi.fn(),
      } satisfies ToolCallMessagePartProps;
      const { rerender } = render(<ToolActivityRow {...props} />);
      expect(screen.getByText("已中断：")).toBeVisible();
      expect(screen.queryByText("正在执行命令…")).not.toBeInTheDocument();
      rerender(
        <ToolActivityRow
          {...props}
          args={{ ...props.args, status: "completed" }}
          result={{}}
        />,
      );
      expect(screen.queryByText("已中断：")).not.toBeInTheDocument();
      expect(screen.getByText("执行命令")).toBeVisible();
    },
  );

  it("工具行展示语义短语和安全摘要，不展示原始工具名或结果正文", () => {
    messageState.current = {
      ...messageState.current,
      status: { type: "complete", reason: "stop" },
    };
    render(
      <ToolActivityRow
        {...({
          type: "tool-call",
          toolCallId: "tool-1",
          toolName: "Bash",
          args: {
            category: "command",
            action: "run_tests",
            status: "completed",
            label: "运行测试 · go test ./...",
            loadingLabel: "正在运行测试 · go test ./...…",
            safeSummary: {
              path: "src/app.ts",
              addedLines: 44,
              deletedLines: 2,
            },
          },
          argsText: "{}",
          result: { output: "secret output" },
          status: { type: "complete" },
          addResult: vi.fn(),
          resume: vi.fn(),
          respondToApproval: vi.fn(),
        } satisfies ToolCallMessagePartProps)}
      />,
    );

    const phrase = screen.getByText("运行测试 · go test ./...");
    expect(phrase).toBeInTheDocument();
    expect(phrase.parentElement).not.toHaveClass("border", "border-b");
    expect(screen.queryByText(/src\/app\.ts/)).not.toBeInTheDocument();
    expect(screen.getByText("+44")).toBeInTheDocument();
    expect(screen.getByText("-2")).toBeInTheDocument();
    expect(screen.queryByText(/退出码/)).not.toBeInTheDocument();
    expect(screen.queryByText("Bash")).not.toBeInTheDocument();
    expect(screen.queryByText(/secret output/)).not.toBeInTheDocument();
  });

  it("工具结果标记失败时仍使用中性动作短语", () => {
    render(
      <ToolActivityRow
        {...({
          type: "tool-call",
          toolCallId: "tool-command-failed",
          toolName: "command",
          args: {
            category: "command",
            action: "execute_command",
            status: "failed",
          },
          argsText: "{}",
          result: {},
          isError: true,
          status: { type: "complete" },
          addResult: vi.fn(),
          resume: vi.fn(),
          respondToApproval: vi.fn(),
        } satisfies ToolCallMessagePartProps)}
      />,
    );

    expect(screen.getByText("执行命令")).toBeInTheDocument();
    expect(screen.queryByLabelText("失败")).not.toBeInTheDocument();
    expect(screen.getByText("失败：")).toHaveClass("sr-only");
  });

  it("工具行展示用户实际选择的审批结果", () => {
    messageState.current = {
      ...messageState.current,
      status: { type: "complete", reason: "stop" },
    };
    render(
      <ToolActivityRow
        {...({
          type: "tool-call",
          toolCallId: "tool-approved",
          toolName: "command",
          args: {
            category: "command",
            action: "execute_command",
            status: "completed",
            label: "执行操作",
            approvalResult: {
              result: "selected",
              optionKind: "reject_once",
              optionName: "Reject once",
              command: "curl https://chatgpt.com/",
            },
          },
          argsText: "{}",
          result: {},
          status: { type: "complete" },
          addResult: vi.fn(),
          resume: vi.fn(),
          respondToApproval: vi.fn(),
        } satisfies ToolCallMessagePartProps)}
      />,
    );

    expect(screen.getByText("执行操作")).toBeInTheDocument();
    expect(screen.getByText("curl https://chatgpt.com/")).toBeInTheDocument();
    expect(screen.getByText("已拒绝")).toBeInTheDocument();
    expect(screen.queryByText("Reject once")).not.toBeInTheDocument();
  });

  it("消息运行中按文件展示修改路径和增删行数", () => {
    messageState.current = {
      ...messageState.current,
      status: { type: "running" },
    };
    render(
      <ToolActivityRow
        {...({
          type: "tool-call",
          toolCallId: "tool-files",
          toolName: "file_modify",
          args: {
            category: "file_modify",
            action: "modify_file",
            status: "completed",
            label: "修改文件",
            safeSummary: {
              path: "cmd/acp-agent/main.go",
              addedLines: 11,
              deletedLines: 8,
              files: [
                {
                  path: "cmd/acp-agent/main.go",
                  addedLines: 5,
                  deletedLines: 6,
                },
                {
                  path: "pkg/acpserver/doc.go",
                  addedLines: 5,
                },
                {
                  path: "pkg/acpserver/registry.go",
                  addedLines: 1,
                  deletedLines: 2,
                },
              ],
            },
          },
          argsText: "{}",
          result: {},
          status: { type: "complete" },
          addResult: vi.fn(),
          resume: vi.fn(),
          respondToApproval: vi.fn(),
        } satisfies ToolCallMessagePartProps)}
      />,
    );

    expect(screen.getByRole("list")).toHaveClass("border-l");
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    expect(screen.getByText("cmd/acp-agent/main.go")).toBeInTheDocument();
    expect(screen.getByText("pkg/acpserver/doc.go")).toBeInTheDocument();
    expect(screen.getByText("pkg/acpserver/registry.go")).toBeInTheDocument();
    expect(screen.getAllByText("+5")).toHaveLength(2);
    expect(screen.getByText("-6")).toBeInTheDocument();
    expect(screen.getByText("+1")).toBeInTheDocument();
    expect(screen.getByText("-2")).toBeInTheDocument();
    expect(screen.queryByText("+11")).not.toBeInTheDocument();
    expect(screen.queryByText("-8")).not.toBeInTheDocument();
  });

  it("消息完成后文件修改工具只展示动作", () => {
    messageState.current = {
      ...messageState.current,
      status: { type: "complete", reason: "stop" },
    };
    render(
      <ToolActivityRow
        {...({
          type: "tool-call",
          toolCallId: "tool-files-completed",
          toolName: "file_modify",
          args: {
            category: "file_modify",
            action: "modify_file",
            status: "completed",
            label: "修改文件",
            safeSummary: {
              path: "projects/Desktop/note_one.txt",
              addedLines: 4,
              deletedLines: 0,
              files: [
                {
                  path: "projects/Desktop/note_one.txt",
                  addedLines: 2,
                },
                {
                  path: "projects/Desktop/note_two.txt",
                  addedLines: 2,
                },
              ],
            },
          },
          argsText: "{}",
          result: {},
          status: { type: "complete" },
          addResult: vi.fn(),
          resume: vi.fn(),
          respondToApproval: vi.fn(),
        } satisfies ToolCallMessagePartProps)}
      />,
    );

    expect(screen.getByText("修改文件")).toBeInTheDocument();
    expect(screen.queryByText(/note_one\.txt/)).not.toBeInTheDocument();
    expect(screen.queryByText(/note_two\.txt/)).not.toBeInTheDocument();
    expect(screen.queryByText("+2")).not.toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("工具过程及普通运行状态均不额外展示飞速运转和图标", () => {
    messageState.current = {
      role: "assistant",
      id: "message-running-process",
      metadata: { custom: {} },
      status: { type: "running" },
      content: [toolPart("command", "running")],
    };
    const view = render(<MessageRunStatus />);

    expect(screen.queryByText("飞速运转")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    messageState.current = {
      ...messageState.current,
      content: [toolPart("command", "completed"), processPart("继续处理")],
    };
    view.rerender(<MessageRunStatus />);

    expect(screen.queryByText("飞速运转")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("完成后展示耗时入口并提供 Token 用量详情", () => {
    render(
      <TooltipProvider delayDuration={0}>
        <RunMetricsBadge
          metrics={{
            durationMs: 17_700,
            inputTokens: 234,
            outputTokens: 1482,
            cacheReadTokens: 40_448,
          }}
        />
      </TooltipProvider>,
    );

    expect(
      screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
    ).toHaveTextContent("17.7s");
    expect(
      screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
    ).not.toHaveClass("ml-auto");
  });

  it.each(["last_request", "query_pipeline", "missing"] as const)(
    "聊天仅展示已知用量，不展示覆盖缺口说明：%s",
    async (coverage) => {
      render(
        <TooltipProvider delayDuration={0}>
          <RunMetricsBadge
            metrics={{
              durationMs: 4600,
              tokenUsage: {
                complete: false,
                coverage,
                missingUsageInvocations: coverage === "missing" ? 1 : 0,
                knownTokens:
                  coverage === "missing" ? {} : { input: 26451, output: 32 },
              },
            }}
          />
        </TooltipProvider>,
      );

      fireEvent.focus(
        screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
      );
      const tooltip = await screen.findByRole("tooltip");
      expect(tooltip).toHaveTextContent("4.6s");
      if (coverage !== "missing") expect(tooltip).toHaveTextContent("32");
      expect(tooltip).not.toHaveTextContent("仅覆盖最后一次请求");
      expect(tooltip).not.toHaveTextContent("用量不完整");
      expect(tooltip).not.toHaveTextContent("未收到用量");
      expect(tooltip).not.toHaveTextContent("Token:");
    },
  );

  it("原生输入无法拆分缓存时仍展示数值，不替代普通输入", async () => {
    const tokenUsage = {
      complete: false,
      knownTokens: { reportedInput: 1200, output: 20, cacheRead: 500 },
    };
    render(
      <TooltipProvider delayDuration={0}>
        <RunMetricsBadge metrics={{ durationMs: 4600, tokenUsage }} />
      </TooltipProvider>,
    );
    fireEvent.focus(
      screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
    );
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("原生输入");
    expect(tooltip).toHaveTextContent("1,200");
    expect(tooltip).toHaveTextContent("500");
    expect(tooltip).not.toHaveTextContent("用量不完整");
  });

  it("末次模型请求的样本单独展示", async () => {
    render(
      <TooltipProvider delayDuration={0}>
        <RunMetricsBadge
          metrics={{ durationMs: 5800, latestRequestOutputTokens: 743 }}
        />
      </TooltipProvider>,
    );

    fireEvent.focus(
      screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
    );
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("末次请求输出");
    expect(tooltip).toHaveTextContent("743");
  });

  it("完成消息读取运行指标时保持外部 store snapshot 稳定", () => {
    messageState.current = {
      role: "assistant",
      id: "message-1",
      createdAt: new Date("2026-08-02T07:00:00.000Z"),
      status: { type: "complete", reason: "stop" },
      content: [],
      metadata: {
        custom: {
          runMetrics: { durationMs: 17_700, outputTokens: 1482 },
        },
      },
    };

    expect(() =>
      render(
        <TooltipProvider delayDuration={0}>
          <MessageRunStatus />
        </TooltipProvider>,
      ),
    ).not.toThrow();
    expect(screen.getByText("17.7s")).toBeInTheDocument();
  });

  it("团队主会话隐藏上下文和消息运行用量，成员会话仍保留", () => {
    messageState.current = {
      role: "assistant",
      id: "member-message",
      createdAt: new Date("2026-09-02T08:24:00.000Z"),
      status: { type: "complete", reason: "stop" },
      content: [],
      metadata: {
        custom: {
          runMetrics: { durationMs: 17_700, outputTokens: 1482 },
        },
      },
    };
    const { rerender } = render(
      <TooltipProvider delayDuration={0}>
        <AssistantMessage name="项目经理" showUsageMetrics={false} />
        <ComposerContextUsage
          metrics={{ contextUsedTokens: 500, contextWindowTokens: 1_000 }}
          visible={false}
        />
      </TooltipProvider>,
    );

    expect(
      screen.queryByRole("button", { name: "查看执行耗时与 Token 用量" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("17.7s")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "查看上下文占用" }),
    ).not.toBeInTheDocument();

    rerender(
      <TooltipProvider delayDuration={0}>
        <AssistantMessage name="项目经理" />
        <ComposerContextUsage
          metrics={{ contextUsedTokens: 500, contextWindowTokens: 1_000 }}
          visible
        />
      </TooltipProvider>,
    );
    expect(
      screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "查看上下文占用" }),
    ).toBeVisible();
  });
});

describe("execution file changes", () => {
  it.each([
    { name: "默认提供入口", showFileChanges: undefined, visible: true },
    { name: "按入口关闭", showFileChanges: false, visible: false },
  ])("$name且只在点击后展示文件变更", ({ showFileChanges, visible }) => {
    const view = new ChatMessageProjection().update([
      {
        id: "file-result",
        role: "assistant",
        status: "completed",
        displayParts: [{ id: "body", type: "text", text: "已交付业务报告" }],
        fileChangeRecord: {
          recordId: "snapshot",
          version: 2,
          state: "ready",
          totalFiles: 1,
          complete: true,
        },
      },
    ])[0];
    if (!view) throw new Error("missing test message");
    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <PureAssistantMessage
          view={view}
          name="测试伙伴"
          showFileChanges={showFileChanges}
        />
      </QueryClientProvider>,
    );
    expect(screen.getByText("已交付业务报告")).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "已编辑 1 个文件" }),
    ).toBeNull();
    const action = screen.queryByRole("button", { name: "文件变更" });
    if (visible) {
      const footer = container.querySelector("footer");
      expect(footer).toContainElement(action);
      expect(footer).toHaveClass(
        "opacity-0",
        "pointer-events-none",
        "group-hover/reply:opacity-100",
        "group-focus-within/reply:opacity-100",
        "[@media(hover:none)]:opacity-100",
      );
      if (!action) throw new Error("missing file changes action");
      fireEvent.click(action);
      expect(
        screen.getByRole("region", { name: "已编辑 1 个文件" }),
      ).toBeVisible();
    } else {
      expect(action).toBeNull();
    }
  });

  // 快照成果卡片的终态、展开与按需 Diff 由 execution-file-changes-card.test.tsx 验证。
  it("旧工具摘要不产生文件变更入口", () => {
    messageState.current = {
      role: "assistant",
      id: "message-toggle-file-changes",
      createdAt: new Date("2026-08-25T07:30:00.000Z"),
      status: { type: "complete", reason: "stop" },
      metadata: {
        custom: {
          isTerminal: true,
          fileChanges: [{ path: "README.md", addedLines: 1 }],
        },
      },
      content: [],
    };

    render(<AssistantMessage name="cc" />);

    expect(screen.queryByText("已编辑 1 个文件")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "文件变更" })).toBeNull();
    expect(screen.getByRole("button", { name: "复制" })).toBeVisible();
  });

  it("团队聚合消息按消息标识展示实际执行成员", () => {
    messageState.current = {
      role: "assistant",
      id: "member-message",
      createdAt: new Date("2026-09-02T08:24:00.000Z"),
      metadata: { custom: {} },
      status: { type: "complete", reason: "stop" },
      content: [],
    };

    render(
      <AssistantMessage
        name="负责人"
        messageAuthors={{
          "member-message": { name: "项目经理" },
        }}
      />,
    );

    expect(
      screen.getByRole("button", { name: "与 项目经理 互动" }),
    ).toBeVisible();
    expect(screen.getByText("项目经理")).toBeVisible();
    expect(screen.queryByText("负责人")).not.toBeInTheDocument();
  });
  it.each(["completed", "failed"] as const)(
    "最新 %s 团队回复悬停也能切换表情，移开恢复原状态",
    (status) => {
      const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
      onTestFinished(() => random.mockRestore());
      const view = new ChatMessageProjection().update([
        {
          id: "latest-team-reply",
          role: "assistant",
          status,
          displayParts: [{ type: "text", text: "团队结果" }],
        },
      ])[0];
      if (!view) throw new Error("missing team fixture");
      render(
        <PureAssistantMessage
          view={view}
          name="团队负责人"
          avatar="bloub:plush:cercle:neutre:ef69bc"
          latest
          last
          messageAuthors={{}}
        />,
      );
      const button = screen.getByRole("button", { name: "与 团队负责人 互动" });
      const avatar = button.querySelector("svg[data-companion-avatar]");
      const resting = avatar?.getAttribute("data-expression");
      fireEvent.pointerEnter(button);
      expect(avatar?.getAttribute("data-expression")).not.toBe(resting);
      expect(avatar).toHaveAttribute("data-activity", status);
      fireEvent.pointerLeave(button);
      expect(avatar).toHaveAttribute("data-expression", resting);
      expect(avatar).toHaveAttribute("data-activity", status);
    },
  );
  it("团队历史回复保留头像与名称，不随最新回复切换而消失", () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    const view = new ChatMessageProjection().update([
      {
        id: "older-team-reply",
        role: "assistant",
        status: "completed",
        displayParts: [{ type: "text", text: "较早的团队回复" }],
      },
    ])[0];
    if (!view) throw new Error("missing team fixture");
    const { container } = render(
      <PureAssistantMessage
        view={view}
        name="团队负责人"
        messageAuthors={{}}
      />,
    );
    expect(screen.getByText("团队负责人")).toBeVisible();
    expect(
      container.querySelector("svg[data-companion-avatar]"),
    ).toBeInTheDocument();
    expect(screen.getByText("较早的团队回复")).toBeVisible();
    const avatarButton = screen.getByRole("button", {
      name: "与 团队负责人 互动",
    });
    const avatar = avatarButton.querySelector("svg[data-companion-avatar]");
    const resting = avatar?.getAttribute("data-expression");
    fireEvent.pointerEnter(avatarButton);
    expect(avatar?.getAttribute("data-expression")).not.toBe(resting);
    fireEvent.pointerLeave(avatarButton);
    expect(avatar).toHaveAttribute("data-expression", resting);
  });
});

describe("assistant message shell", () => {
  it("续接回复只在最后一段显示头像，历史段不保留静态头像", () => {
    const messages: BotChatMessage[] = [
      { id: "previous-segment", role: "assistant", status: "canceled" },
      { id: "latest-segment", role: "assistant", status: "completed" },
    ];
    const views = new ChatMessageProjection().update(messages);
    const { container } = render(
      <TooltipProvider>
        {views.map((view, index) => (
          <PureAssistantMessage
            key={view.key}
            view={view}
            latest={index === views.length - 1}
            name="伙伴"
            continuation={{
              continued: index === 0,
              continuation: index === 1,
              footerMessage: view.message,
            }}
          />
        ))}
      </TooltipProvider>,
    );
    expect(
      container.querySelectorAll("svg[data-companion-avatar]"),
    ).toHaveLength(1);
    expect(
      container.querySelector(
        '[data-message-id="previous-segment"] svg[data-companion-avatar]',
      ),
    ).toBeNull();
    expect(
      container.querySelector(
        '[data-message-id="latest-segment"] svg[data-companion-avatar]',
      ),
    ).not.toBeNull();
  });
  it("完成后用一个入口收起交错的中间文字与工具，保留最后的正文", () => {
    const message: BotChatMessage = {
      id: "multiple-processes",
      role: "assistant",
      status: "running",
      runMetrics: { durationMs: 32300 },
      displayParts: [
        { id: "text-1", type: "text", text: "先检查插件" },
        {
          id: "tool-1",
          type: "tool",
          toolCategory: "file_read",
          toolStatus: "completed",
        },
        { id: "text-2", type: "text", text: "插件已就绪" },
        { id: "reason", type: "reasoning", text: "再确认使用方式" },
        {
          id: "tool-2",
          type: "tool",
          toolCategory: "network",
          toolStatus: "completed",
        },
        { id: "final-1", type: "text", text: "可以直接使用" },
        { id: "final-2", type: "text", text: "以下是配置说明" },
        {
          id: "progress",
          type: "process_commentary",
          text: "正在记录执行情况",
        },
      ],
    };
    const projection = new ChatMessageProjection();
    const reply = (message: BotChatMessage) => {
      const view = projection.update([message])[0];
      if (!view) throw new Error("missing test message");
      return (
        <TooltipProvider>
          <PureAssistantMessage name="伙伴" view={view} />
        </TooltipProvider>
      );
    };
    const { rerender } = render(reply(message));
    expect(screen.getByText("先检查插件")).toBeVisible();
    screen.getByRole("button", { name: "已访问网络" }).focus();

    rerender(reply({ ...message, status: "completed" }));
    const toggle = screen.getByRole("button", { name: "用时 32.3s" });
    expect(toggle).toHaveFocus();
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: "查看过程" })).toBeNull();
    expect(screen.queryByText("先检查插件")).toBeNull();
    expect(screen.queryByText("插件已就绪")).toBeNull();
    expect(screen.queryByText("正在记录执行情况")).toBeNull();
    expect(screen.getByText("可以直接使用")).toBeVisible();
    expect(screen.getByText("以下是配置说明")).toBeVisible();

    fireEvent.click(toggle);
    expect(screen.getByText("先检查插件")).toBeVisible();
    expect(screen.getByRole("button", { name: "已读取文件" })).toBeVisible();
    expect(screen.getByText("插件已就绪")).toBeVisible();
    expect(screen.getByText("再确认使用方式")).toBeVisible();
    expect(screen.getByRole("button", { name: "已访问网络" })).toBeVisible();
    expect(screen.getByText("正在记录执行情况")).toBeVisible();

    fireEvent.click(toggle);
    expect(screen.queryByText("插件已就绪")).toBeNull();
    expect(screen.getByText("可以直接使用")).toBeVisible();
  });
  it("首发立即显示建立会话文案，接纳后原位收起文字并保留头像和三点", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-06T03:00:00Z"));
    const pending: BotChatMessage & { preparingSession: boolean } = {
      id: "preparing",
      role: "assistant",
      status: "pending",
      createdAt: Date.now(),
      optimistic: true,
      preparingSession: true,
    };
    const projection = new ChatMessageProjection();
    const preparing = projection.update([pending])[0];
    if (!preparing) throw new Error("missing preparing message");
    const view = render(
      <PureAssistantMessage view={preparing} latest name="nova" />,
    );
    const avatar = view.container.querySelector("[data-companion-avatar]");
    expect(avatar).toHaveAttribute("data-animation", "working");
    expect(avatar?.querySelector("[data-body]")).toBeInTheDocument();
    const establishing = screen.getByText("正在建立会话");
    expect(establishing).toBeVisible();
    expect(establishing).toHaveAttribute("data-running-shimmer", "true");
    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(screen.getByRole("status")).toHaveTextContent("正在建立会话");
    const dots = avatar?.querySelectorAll(
      '[data-native-attention="working"] > circle',
    );
    expect(dots).toHaveLength(3);
    expect(
      view.container.querySelector("[data-chat-message-card]"),
    ).not.toBeInTheDocument();
    const running = projection.update([
      {
        ...pending,
        status: "running",
        optimistic: false,
        preparingSession: false,
      },
    ])[0];
    if (!running) throw new Error("missing running message");
    view.rerender(<PureAssistantMessage view={running} latest name="nova" />);
    expect(establishing.closest('[aria-hidden="true"][inert]')).toHaveAttribute(
      "data-state",
      "closed",
    );
    expect(screen.getByRole("status")).toHaveTextContent("正在等待回复");
    expect(view.container.querySelector("[data-companion-avatar]")).toBe(
      avatar,
    );
    const acceptedDots = avatar?.querySelectorAll(
      '[data-native-attention="working"] > circle',
    );
    dots?.forEach((dot, index) => expect(acceptedDots?.[index]).toBe(dot));
    act(() => {
      vi.advanceTimersByTime(140);
    });
    expect(screen.queryByText("正在建立会话")).not.toBeInTheDocument();
    expect(view.container.querySelector("[data-running-shimmer]")).toBeNull();
    expect(avatar).toHaveAttribute("data-animation", "working");
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("续发等待使用头像和三点，出现正文后三点收起", () => {
    const projection = new ChatMessageProjection();
    const pending = projection.update([
      {
        id: "follow-up",
        role: "assistant",
        status: "pending",
        optimistic: true,
      },
    ])[0];
    if (!pending) throw new Error("missing follow-up message");
    const { container, rerender } = render(
      <PureAssistantMessage view={pending} latest name="nova" />,
    );
    expect(screen.queryByText("正在建立会话")).not.toBeInTheDocument();
    expect(container.querySelector("[data-companion-avatar]")).toHaveAttribute(
      "data-animation",
      "working",
    );

    const responding = projection.update([
      {
        ...pending.message,
        status: "running",
        displayParts: [{ id: "reply", type: "text", text: "第一段回复" }],
      },
    ])[0];
    if (!responding) throw new Error("missing reply");
    rerender(<PureAssistantMessage view={responding} latest name="nova" />);
    expect(screen.getByText("第一段回复")).toBeVisible();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(
      container.querySelector('[data-native-attention="working"]'),
    ).not.toBeInTheDocument();
  });
  it("最新助手消息变成历史消息后移除头像，名称不重复展示且时间位于底部", () => {
    messageState.current = {
      role: "assistant",
      id: "assistant-avatar-state",
      createdAt: new Date("2026-09-21T06:00:00.000Z"),
      isLast: true,
      metadata: { custom: {} },
      status: { type: "running" },
      content: [toolPart("file_read", "running")],
    };
    const view = render(<AssistantMessage name="测试伙伴" />);
    expect(
      view.container.querySelector("[data-companion-avatar]"),
    ).toHaveAttribute("data-animation", "idle");
    expect(
      view.container.querySelector('[data-native-attention="working"]'),
    ).not.toBeInTheDocument();
    messageState.current = { ...messageState.current, isLast: false };
    messageState.messages = [
      { id: messageState.current.id, role: "assistant" },
      { id: "new-assistant", role: "assistant" },
    ];
    const raf = vi.spyOn(window, "requestAnimationFrame");
    view.rerender(<AssistantMessage name="测试伙伴" />);
    expect(raf).not.toHaveBeenCalled();
    raf.mockRestore();
    expect(
      view.container.querySelector("svg[data-companion-avatar]"),
    ).toBeNull();
    expect(view.container.querySelector("img")).toBeNull();
    expect(screen.queryByText("测试伙伴")).not.toBeInTheDocument();
    const timestamp = view.container.querySelector("time");
    expect(timestamp).toBeInTheDocument();
    expect(timestamp?.closest("footer")).not.toBeNull();
  });
  it("末尾是用户消息时，之前的助手消息不再保留头像", () => {
    messageState.current = {
      role: "assistant",
      id: "last-assistant",
      metadata: { custom: {} },
      isLast: false,
      createdAt: new Date("2026-10-30T00:00:00Z"),
      status: { type: "complete", reason: "stop" },
      content: [],
    };
    messageState.messages = [
      { id: "last-assistant", role: "assistant" },
      { id: "new-user", role: "user" },
    ];
    const { container } = render(<AssistantMessage name="测试伙伴" />);
    expect(container.querySelector("svg[data-companion-avatar]")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
  });
  it("尚无输出时保留伙伴头像，隐藏空卡片与底部占位", () => {
    messageState.current = {
      role: "assistant",
      createdAt: new Date("2026-09-05T01:58:48.000Z"),
      id: "assistant-pending-output",
      metadata: { custom: {} },
      status: { type: "running" },
      content: [],
    };

    const { container } = render(<AssistantMessage name="测试伙伴" />);

    expect(screen.queryByText("测试伙伴")).not.toBeInTheDocument();
    expect(container.querySelector("time")).toBeNull();
    expect(container.querySelector("footer")).toBeNull();
    expect(container.querySelector("[data-activity]")).not.toBeNull();
    expect(screen.queryByText("飞速运转")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("正在等待回复");
    expect(container.querySelector("[data-chat-message-card]")).toBeNull();
  });
});

describe("steer 连续工作展示", () => {
  const projection = new ChatMessageProjection();
  const message: BotChatMessage = {
    id: "steered",
    role: "assistant",
    status: "canceled",
    executionId: "run-steered",
    displayParts: [{ id: "body", type: "text", text: "继续之前的工作" }],
    runMetrics: { durationMs: 9000 },
  };
  it("中间段保留正文并隐藏取消提示、耗时、复制及分叉操作", () => {
    const view = projection.update([message])[0];
    if (!view) throw new Error("missing test message");
    render(
      <TooltipProvider>
        <PureAssistantMessage
          name="伙伴"
          view={view}
          continuation={{
            continued: true,
            continuation: false,
            footerMessage: message,
          }}
        />
      </TooltipProvider>,
    );
    expect(screen.getByText("继续之前的工作")).toBeVisible();
    expect(screen.queryByText("执行已取消")).not.toBeInTheDocument();
    expect(screen.queryByText("9s")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "复制" }),
    ).not.toBeInTheDocument();
  });
  it("普通停止仍显示取消提示与复制操作", () => {
    const view = projection.update([message])[0];
    if (!view) throw new Error("missing test message");
    render(
      <TooltipProvider>
        <PureAssistantMessage name="伙伴" view={view} />
      </TooltipProvider>,
    );
    expect(screen.getByText("执行已取消")).toBeVisible();
    expect(screen.getByRole("button", { name: "复制" })).toBeVisible();
  });
});

describe("团队成员消息折叠", () => {
  const authors = {
    "report-a": {
      name: "A",
      sourceChatId: "chat-a",
      displayAsParticipant: true,
      kind: "member-report" as const,
    },
    "report-a2": {
      name: "A",
      sourceChatId: "chat-a",
      displayAsParticipant: true,
      kind: "member-report" as const,
    },
    "report-b": {
      name: "B",
      sourceChatId: "chat-b",
      displayAsParticipant: true,
      kind: "member-report" as const,
    },
  };
  const projection = new ChatMessageProjection();
  const report = (id: string, content: string): BotChatMessage => ({
    id,
    role: "user",
    actor: "team_member",
    content,
  });
  const leader = (id = "leader"): BotChatMessage => ({
    id,
    role: "assistant",
    status: "completed",
    displayParts: [{ type: "text", text: `负责人结果 ${id}` }],
  });

  it("展开报告与普通伙伴回复共用悬停底栏并保留后续消息的间距", () => {
    const { container } = render(
      <ChatMessageList
        views={projection.update([
          { ...report("report-a", "展开的报告"), createdAt: 1000 },
          { ...leader(), createdAt: 2000 },
          { id: "next-input", role: "user", content: "继续" },
        ])}
        name="伙伴"
        messageAuthors={authors}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "展开 A 的消息" }));
    const reportRow = container.querySelector('[data-message-id="report-a"]');
    const ordinaryRow = container.querySelector('[data-message-id="leader"]');
    const reportFooter = reportRow?.querySelector("footer");
    const ordinaryFooter = ordinaryRow?.querySelector("footer");
    expect(reportFooter).toHaveClass(
      "opacity-0",
      "pointer-events-none",
      "group-hover/reply:opacity-100",
      "group-focus-within/reply:opacity-100",
      "[@media(hover:none)]:opacity-100",
    );
    expect(reportFooter?.className).toBe(ordinaryFooter?.className);
    for (const row of [reportRow, ordinaryRow]) {
      expect(row).not.toHaveClass("pb-4");
      expect(row?.querySelector("footer")?.parentElement).toHaveClass(
        "group/reply",
      );
      expect(row?.querySelector("time")?.closest("footer")).not.toBeNull();
      expect(row?.querySelectorAll("footer")).toHaveLength(1);
    }
    expect(
      reportRow?.querySelector("[data-chat-message-card]")?.className,
    ).toBe(ordinaryRow?.querySelector("[data-chat-message-card]")?.className);
  });

  it("默认只显示负责人结果，按伙伴独立展开并复用普通消息卡片", () => {
    const views = projection.update([
      report("report-a", "A 的消息"),
      report("report-b", "B 的消息"),
      report("report-a2", "A 的补充"),
      leader(),
    ]);
    const { container, rerender } = render(
      <ChatMessageList views={views} name="负责人" messageAuthors={authors} />,
    );
    expect(screen.getByText("负责人结果 leader")).toBeVisible();
    expect(screen.queryByText("A 的消息")).not.toBeInTheDocument();
    expect(screen.queryByText("B 的消息")).not.toBeInTheDocument();
    expect(screen.queryByText("成员报告")).not.toBeInTheDocument();
    expect(screen.getByText(/收到来自.*的消息/)).toBeVisible();
    const toggleA = screen.getByRole("button", { name: "展开 A 的消息" });
    expect(toggleA).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggleA);
    expect(screen.getByText("A 的消息")).toBeVisible();
    expect(screen.getByText("A 的补充")).toBeVisible();
    expect(screen.queryByText("B 的消息")).not.toBeInTheDocument();
    const card = screen
      .getByText("A 的消息")
      .closest("[data-chat-message-card]");
    expect(card).toHaveClass("bg-message-card-background");
    expect(card).not.toHaveClass("border-l-2");
    fireEvent.click(screen.getByRole("button", { name: "展开 B 的消息" }));
    expect(screen.getByText("A 的消息")).toBeVisible();
    expect(screen.getByText("B 的消息")).toBeVisible();
    rerender(
      <ChatMessageList
        views={[...views]}
        name="负责人"
        messageAuthors={{ ...authors }}
      />,
    );
    expect(screen.getByText("A 的消息")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "收起 A 的消息" }));
    expect(screen.queryByText("A 的消息")).not.toBeInTheDocument();
    expect(screen.getByText("B 的消息")).toBeVisible();
    const marker = screen.getByText(/收到来自.*的消息/);
    const leadCard = container.querySelector('[data-message-id="leader"]');
    if (!leadCard) throw new Error("missing leader message");
    expect(
      marker.compareDocumentPosition(leadCard) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("按回复轮次分别折叠，同名伙伴按原身份区分，直接交付不折叠", () => {
    const views = projection.update([
      report("report-a", "第一轮报告"),
      leader("one"),
      { id: "input", role: "user", content: "下一轮" },
      report("report-b", "第二轮报告"),
      leader("two"),
      report("direct", "直接交付结果"),
    ]);
    render(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={{
          ...authors,
          "report-b": { ...authors["report-b"], name: "A" },
          direct: { name: "C", displayAsParticipant: true },
        }}
      />,
    );
    const toggles = screen.getAllByRole("button", { name: "展开 A 的消息" });
    expect(toggles).toHaveLength(2);
    const secondToggle = toggles[1];
    if (!secondToggle) throw new Error("missing second round toggle");
    fireEvent.click(secondToggle);
    expect(screen.queryByText("第一轮报告")).not.toBeInTheDocument();
    expect(screen.getByText("第二轮报告")).toBeVisible();
    expect(screen.getByText("直接交付结果")).toBeVisible();
  });

  it("消息深链展开对应伙伴，保留截断提示与原委派定位", () => {
    const views = projection.update([
      { ...report("report-a", "定位到 A 的报告"), createdAt: 100 },
      { ...report("report-b", "B 的报告"), createdAt: 110 },
      { ...leader(), createdAt: 200 },
    ]);
    const { rerender } = render(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={{
          ...authors,
          "report-a": { ...authors["report-a"], truncated: true },
        }}
        timelineEvents={[
          {
            id: "before-report",
            createdAt: 90,
            beforeMessageId: "report-a",
            content: <span>原委派</span>,
          },
        ]}
      />,
    );
    expect(screen.getByText("原委派")).toBeVisible();
    rerender(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={{
          ...authors,
          "report-a": { ...authors["report-a"], truncated: true },
        }}
        requestedMessageId="report-a"
      />,
    );
    expect(screen.getByText("定位到 A 的报告")).toBeVisible();
    expect(screen.getByText("内容已截断，不可作为完整依据")).toBeVisible();
    expect(screen.queryByText("B 的报告")).not.toBeInTheDocument();
  });

  it("前插同轮历史后保留已展开伙伴，并区分同名伙伴的真实身份", () => {
    const metadata = {
      ...authors,
      "report-a": {
        ...authors["report-a"],
        name: "同名",
        wakerId: "a",
        sourceChatId: "shared",
      },
      "report-b": {
        ...authors["report-b"],
        name: "同名",
        wakerId: "b",
        sourceChatId: "shared",
      },
    };
    const { rerender } = render(
      <ChatMessageList
        views={projection.update([report("report-b", "B 原报告"), leader()])}
        name="负责人"
        messageAuthors={metadata}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "展开 同名 的消息" }));
    rerender(
      <ChatMessageList
        views={projection.update([
          report("report-a", "A 更早报告"),
          report("report-b", "B 原报告"),
          leader(),
        ])}
        name="负责人"
        messageAuthors={metadata}
      />,
    );
    expect(screen.getByText("B 原报告")).toBeVisible();
    expect(screen.queryByText("A 更早报告")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "展开 同名 的消息" }));
    expect(screen.getByText("A 更早报告")).toBeVisible();
    expect(screen.getByText("B 原报告")).toBeVisible();
  });

  it("手动收起后再次导航相同消息仍能定位，点击展开后保持焦点", () => {
    const views = projection.update([
      report("report-a", "再次定位的报告"),
      leader(),
    ]);
    const { rerender } = render(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={authors}
        requestedMessageId="report-a"
        requestedMessageKey="first"
      />,
    );
    expect(screen.getByText("再次定位的报告")).toBeVisible();
    const toggle = screen.getByRole("button", { name: "收起 A 的消息" });
    toggle.focus();
    fireEvent.click(toggle);
    expect(screen.queryByText("再次定位的报告")).not.toBeInTheDocument();
    expect(toggle).toHaveFocus();
    rerender(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={authors}
        requestedMessageId="report-a"
        requestedMessageKey="second"
      />,
    );
    expect(screen.getByText("再次定位的报告")).toBeVisible();
    expect(toggle).toHaveFocus();
    rerender(
      <ChatMessageList views={views} name="负责人" messageAuthors={authors} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "收起 A 的消息" }));
    rerender(
      <ChatMessageList
        views={views}
        name="负责人"
        messageAuthors={authors}
        requestedMessageId="report-a"
        requestedMessageKey="second"
      />,
    );
    expect(screen.getByText("再次定位的报告")).toBeVisible();
  });

  it("伙伴名字沿用委派的头像色 hover 和临时表情，点击仍展开消息", () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    render(
      <ChatMessageList
        views={projection.update([report("report-a", "伙伴消息"), leader()])}
        name="负责人"
        messageAuthors={{
          ...authors,
          "report-a": {
            ...authors["report-a"],
            avatar: "bloub:plush:cercle:neutre:ef69bc",
          },
        }}
      />,
    );
    const button = screen.getByRole("button", { name: "展开 A 的消息" });
    const avatar = button.querySelector("svg[data-companion-avatar]");
    expect(avatar).toHaveAttribute("data-expression", "neutre");
    expect(
      button.style.getPropertyValue("--companion-hover-surface"),
    ).toContain("#ef69bc");
    fireEvent.pointerEnter(button);
    expect(avatar?.getAttribute("data-expression")).not.toBe("neutre");
    fireEvent.pointerLeave(button);
    expect(avatar).toHaveAttribute("data-expression", "neutre");
    fireEvent.click(button);
    expect(screen.getByText("伙伴消息")).toBeVisible();
    expect(button.querySelector("button")).toBeNull();
  });

  it("负责人尚未回复时可以查看报告，回复到达后保留展开状态", () => {
    const views = projection.update([report("report-a", "已收到的报告")]);
    const { rerender } = render(
      <ChatMessageList views={views} name="负责人" messageAuthors={authors} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "展开 A 的消息" }));
    rerender(
      <ChatMessageList
        views={projection.update([
          report("report-a", "已收到的报告"),
          leader(),
        ])}
        name="负责人"
        messageAuthors={authors}
      />,
    );
    expect(screen.getByText("已收到的报告")).toBeVisible();
    expect(screen.getByText("负责人结果 leader")).toBeVisible();
  });
});
