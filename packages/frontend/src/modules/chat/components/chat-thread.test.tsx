import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type * as ReactModule from "react";
import type * as AssistantUI from "@assistant-ui/react";
import type {
  ReasoningMessagePartProps,
  ThreadAssistantMessagePart,
  ToolCallMessagePart,
  ToolCallMessagePartProps,
} from "@assistant-ui/react";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  AgentRunningIndicator,
  AssistantMessage,
  ComposerContextUsage,
  ExecutionFileChangesCard,
  MessageRunStatus,
  ReasoningPart,
  RunMetricsBadge,
  ToolActivityRow,
  WorkProcess,
} from "./chat-thread";
import { CHAT_PROCESS_COMMENTARY_PART } from "../runtime/chat-projection";

type MockMessageState = {
  id: string;
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
    current: {
      id: "message-1",
      status: { type: "complete", reason: "stop" },
      content: [] as ThreadAssistantMessagePart[],
    } as MockMessageState,
  },
}));

vi.mock("@assistant-ui/react", async () => {
  const actual = await vi.importActual<typeof AssistantUI>(
    "@assistant-ui/react",
  );
  const React = await vi.importActual<typeof ReactModule>("react");
  const subscribe = () => () => undefined;
  const PassThrough = ({ children }: { children?: ReactModule.ReactNode }) => (
    <>{children}</>
  );
  return {
    ...actual,
    ActionBarPrimitive: {
      Root: PassThrough,
      Copy: PassThrough,
    },
    AuiIf: ({
      condition,
      children,
    }: {
      condition: boolean | ((state: { message: MockMessageState }) => boolean);
      children?: ReactModule.ReactNode;
    }) => (
      <>
        {(
          typeof condition === "function"
            ? condition({ message: messageState.current })
            : condition
        )
          ? children
          : null}
      </>
    ),
    MessagePrimitive: {
      Root: PassThrough,
      GroupedParts: () => null,
      Error: () => null,
    },
    useAuiState: <T,>(selector: (state: { message: MockMessageState }) => T) =>
      React.useSyncExternalStore(
        subscribe,
        () => selector({ message: messageState.current }),
        () => selector({ message: messageState.current }),
      ),
  };
});

afterEach(() => {
  vi.useRealTimers();
  messageState.current = {
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
      id: "failed-message",
      createdAt: new Date(0),
      isLast: true,
      content: [],
      status: { type: "incomplete", reason: "error" },
      metadata: {
        custom: {
          executionId: "failed-execution",
          failureMessage: "模型服务暂时不可用",
        },
      },
    };
    const { rerender, container } = render(<AssistantMessage name="NeuxBot" />);
    expect(screen.getByRole("alert")).toHaveTextContent("模型服务暂时不可用");
    messageState.current = { ...messageState.current, isLast: false };
    rerender(<AssistantMessage name="NeuxBot" />);
    expect(screen.queryByText("模型服务暂时不可用")).not.toBeInTheDocument();
    expect(container.querySelector("[data-work-result-body]")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "展开失败详情" }));
    expect(screen.getByText("模型服务暂时不可用")).toBeInTheDocument();
    expect(container.querySelector("[data-work-result-body]")).not.toBeNull();
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
  it("摘要按首次出现顺序合并工具类别，不展示计数和过程消息", () => {
    messageState.current = {
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

  it("终态文件改动 Card 展示在执行过程内容底部", async () => {
    const onOpenFile = vi.fn();
    messageState.current = {
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
    await screen.findByText("已编辑 2 个文件");

    const process = toggle.closest("section");
    expect(process).not.toBeNull();
    expect(
      within(process as HTMLElement).getByText("已编辑 2 个文件"),
    ).toBeInTheDocument();
    expect(screen.getByText("Desktop/note_two.txt")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "查看 Desktop/note_two.txt 的更改",
      }),
    );
    expect(onOpenFile).toHaveBeenCalledWith({
      messageId: "message-process-file-changes",
      name: "note_two.txt",
      path: "projects/Desktop/note_two.txt",
    });
  });

  it("多段工具活动只在最后一段展示文件改动汇总", async () => {
    messageState.current = {
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
    expect(await screen.findByText("已编辑 1 个文件")).toBeVisible();
    expect(screen.getAllByText("已编辑 1 个文件")).toHaveLength(1);
  });

  it("运行中手动展开后，完成时保持展开", () => {
    messageState.current = {
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

  it("运行中只展示网格动画和飞速运转，不展示计时", () => {
    render(<AgentRunningIndicator />);

    expect(screen.getByText("飞速运转")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveClass("agent-grid-spinner");
    expect(screen.queryByText(/\d+(?:\.\d+)?(?:ms|s)/)).not.toBeInTheDocument();
  });

  it("当前处于工具过程时隐藏飞速运转和图标，离开工具过程后恢复", () => {
    messageState.current = {
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

    expect(screen.getByText("飞速运转")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveClass("agent-grid-spinner");
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

  it("完成消息读取运行指标时保持外部 store snapshot 稳定", () => {
    messageState.current = {
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
  it("仅在终态展示本次执行文件，并支持展开和打开 Diff", () => {
    const onOpenFile = vi.fn();
    messageState.current = {
      id: "message-file-changes",
      status: { type: "complete", reason: "stop" },
      metadata: {
        custom: {
          isTerminal: true,
          fileChanges: [
            {
              path: "projects/Desktop/README.md",
              addedLines: 12,
              deletedLines: 3,
            },
            { path: "docs/a.md", addedLines: 1, deletedLines: 1 },
            { path: "docs/b.md", addedLines: 2, deletedLines: 0 },
            { path: "src/main.ts", addedLines: 4, deletedLines: 2 },
            { path: "src/main.test.ts", addedLines: 8, deletedLines: 0 },
          ],
        },
      },
      content: [],
    };

    render(<ExecutionFileChangesCard visible onOpenFile={onOpenFile} />);

    expect(screen.getByText("已编辑 5 个文件")).toBeInTheDocument();
    expect(screen.queryByText("查看更改")).not.toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "已编辑 5 个文件" }).firstElementChild,
    ).toHaveClass("min-h-row-default");
    expect(screen.getByText("Desktop/README.md")).toBeInTheDocument();
    expect(
      screen.queryByText("projects/Desktop/README.md"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("src/main.ts")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "再显示 2 个文件" }));
    expect(screen.getByText("src/main.ts")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "查看 src/main.ts 的更改" }),
    );
    expect(onOpenFile).toHaveBeenCalledWith({
      messageId: "message-file-changes",
      name: "main.ts",
      path: "src/main.ts",
    });
  });

  it("执行仍在运行时不提前展示文件变更卡片", () => {
    messageState.current = {
      id: "message-running-file-changes",
      status: { type: "running" },
      metadata: {
        custom: {
          isTerminal: false,
          fileChanges: [{ path: "README.md", addedLines: 1 }],
        },
      },
      content: [],
    };

    render(<ExecutionFileChangesCard visible />);

    expect(screen.queryByText("已编辑 1 个文件")).not.toBeInTheDocument();
  });

  it("有文件改动时不在复制按钮后展示独立入口", () => {
    messageState.current = {
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
    expect(screen.getByRole("button", { name: "复制" })).toBeVisible();
  });

  it("团队聚合消息按消息标识展示实际执行成员", () => {
    messageState.current = {
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

    expect(screen.getByText("项目经理")).toBeVisible();
    expect(screen.queryByText("负责人")).not.toBeInTheDocument();
  });
});

describe("assistant message shell", () => {
  it("最新助手消息变成历史消息后，头像退出工作状态", () => {
    messageState.current = {
      id: "assistant-avatar-state",
      createdAt: new Date("2026-09-21T06:00:00.000Z"),
      isLast: true,
      metadata: { custom: {} },
      status: { type: "running" },
      content: [toolPart("file_read", "running")],
    };
    const view = render(<AssistantMessage name="测试伙伴" />);
    expect(
      view.container.querySelector("[data-companion-activity]"),
    ).toHaveAttribute("data-companion-activity", "searching");
    messageState.current = { ...messageState.current, isLast: false };
    view.rerender(<AssistantMessage name="测试伙伴" />);
    expect(
      view.container.querySelector("[data-companion-activity]"),
    ).toHaveAttribute("data-companion-activity", "idle");
  });
  it("尚无输出时立即展示伙伴身份和原有飞速运转动画", () => {
    messageState.current = {
      createdAt: new Date("2026-09-05T01:58:48.000Z"),
      id: "assistant-pending-output",
      metadata: { custom: {} },
      status: { type: "running" },
      content: [],
    };

    const { container } = render(<AssistantMessage name="测试伙伴" />);

    expect(screen.getByText("测试伙伴")).toBeVisible();
    expect(container.querySelector("time")).toHaveAttribute(
      "datetime",
      "2026-09-05T01:58:48.000Z",
    );
    expect(screen.getByText("飞速运转")).toBeVisible();
    expect(screen.getByRole("status")).toHaveClass("agent-grid-spinner");
    expect(screen.getByRole("status").children).toHaveLength(9);
  });
});
