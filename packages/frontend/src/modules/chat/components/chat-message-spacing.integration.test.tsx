import { fireEvent, render, screen } from "@testing-library/react";
import { useMemo } from "react";
import { MemoryRouter } from "react-router-dom";
import { expect, it } from "vitest";

import { i18n } from "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type { AllyChatMessage } from "../runtime/chat-projection";
import { ChatMessageProjection } from "./chat-message-nodes";
import { AssistantMessage } from "./chat-thread";
import { ChatMessageContributionProvider } from "./chat-contributions";

function MessageHarness({
  message,
  showUsageMetrics = true,
}: {
  message: AllyChatMessage;
  showUsageMetrics?: boolean;
}) {
  const view = useMemo(
    () => new ChatMessageProjection().update([message])[0],
    [message],
  );
  if (!view) throw new Error("missing fixture message");
  return (
    <MemoryRouter>
      <TooltipProvider>
        <AssistantMessage
          view={view}
          name="私人助理"
          latest
          last
          showUsageMetrics={showUsageMetrics}
        />
      </TooltipProvider>
    </MemoryRouter>
  );
}

it("等待输出和纯空白不显示空卡，有实际工具过程或正文后再显示卡片", () => {
  const message: AllyChatMessage = {
    id: "empty-to-content",
    role: "assistant",
    status: "running",
  };
  const view = render(<MessageHarness message={message} />);
  expect(view.container.querySelector("[data-chat-message-card]")).toBeNull();
  expect(view.container.querySelector("footer")).toBeNull();
  expect(screen.queryByText("飞速运转")).toBeNull();
  view.rerender(
    <MessageHarness
      message={{ ...message, displayParts: [{ type: "text", text: " \n\t" }] }}
    />,
  );
  expect(view.container.querySelector("[data-chat-message-card]")).toBeNull();
  view.rerender(
    <MessageHarness
      message={{
        ...message,
        displayParts: [
          {
            type: "tool",
            toolCallId: "read-1",
            toolCategory: "file_read",
            toolAction: "read_file",
            toolStatus: "running",
          },
        ],
      }}
    />,
  );
  expect(
    view.container.querySelector("[data-chat-message-card]"),
  ).not.toBeNull();
  view.rerender(
    <MessageHarness
      message={{
        ...message,
        displayParts: [{ type: "text", text: "实际回复" }],
      }}
    />,
  );
  expect(
    screen.getByText("实际回复").closest("[data-chat-message-card]"),
  ).not.toBeNull();
  expect(screen.queryByText("飞速运转")).toBeNull();
});

it("仅有领域贡献成果时仍展示消息卡", () => {
  render(
    <ChatMessageContributionProvider value={() => <p>工作流成果</p>}>
      <MessageHarness
        message={{
          id: "contribution-only",
          role: "assistant",
          status: "running",
        }}
      />
    </ChatMessageContributionProvider>,
  );
  expect(
    screen.getByText("工作流成果").closest("[data-chat-message-card]"),
  ).not.toBeNull();
});

it("思考后的纯换行不占段落高度，也不把连续网络工具拆成多个活动组", () => {
  // 使用截图对应的实际片段顺序：思考、三个换行、工具、一个换行、工具。
  const message: AllyChatMessage = {
    id: "assistant-spacing",
    role: "assistant",
    status: "running",
    displayParts: [
      { type: "reasoning", text: "先收集今日 AI 新闻。" },
      { type: "process_commentary", text: "\n\n\n" },
      {
        type: "tool",
        toolCallId: "web-1",
        toolCategory: "network",
        toolAction: "search_web",
        toolStatus: "completed",
      },
      { type: "process_commentary", text: "\n" },
      { type: "reasoning", text: " \t\n" },
      { type: "text", text: "\r\n" },
      {
        type: "tool",
        toolCallId: "web-2",
        toolCategory: "network",
        toolAction: "search_web",
        toolStatus: "completed",
      },
    ],
  };
  const { container } = render(<MessageHarness message={message} />);

  expect(screen.getAllByRole("button", { name: "已访问网络" })).toHaveLength(1);
  const reasoning = screen.getByText("先收集今日 AI 新闻。");
  const parts = reasoning.closest(".space-y-4");
  expect(parts?.children).toHaveLength(2);
  expect(
    [...container.querySelectorAll("p")].filter(
      (paragraph) => paragraph.textContent && !paragraph.textContent.trim(),
    ),
  ).toHaveLength(0);
});

function resultMessage(
  status: "running" | "completed" | "failed" = "completed",
): AllyChatMessage {
  return {
    id: "assistant-result",
    role: "assistant",
    status,
    runMetrics: { durationMs: 22_000, outputTokens: 1482 },
    displayParts: [
      { type: "reasoning", text: "先检查配置来源。" },
      { type: "process_commentary", text: "正在核对项目配置。" },
      {
        type: "tool",
        toolCallId: "read-config",
        toolCategory: "file_read",
        toolAction: "read_file",
        toolStatus: "completed",
      },
      { type: "reasoning", text: "配置已经核对完成。" },
      { type: "text", text: "最终结果：配置正确。" },
    ],
  };
}

it("完成后用一个耗时摘要收起全部过程，最终正文始终可见", () => {
  render(<MessageHarness message={resultMessage()} />);
  const toggle = screen.getByRole("button", { name: "用时 22.0s" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(screen.getByRole("separator")).toBeVisible();
  expect(screen.getAllByText(/22\.0s/)).toHaveLength(1);
  expect(
    screen.queryByRole("button", { name: "查看执行耗时与 Token 用量" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("先检查配置来源。")).not.toBeInTheDocument();
  expect(screen.queryByText("正在核对项目配置。")).not.toBeInTheDocument();
  expect(screen.queryByText("配置已经核对完成。")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "已读取文件" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("最终结果：配置正确。")).toBeVisible();
  fireEvent.click(toggle);
  expect(screen.getByText("先检查配置来源。")).toBeVisible();
  expect(screen.getByText("正在核对项目配置。")).toBeVisible();
  expect(screen.getByText("配置已经核对完成。")).toBeVisible();
  expect(screen.getByRole("button", { name: "已读取文件" })).toBeVisible();
  fireEvent.click(toggle);
  expect(screen.queryByText("先检查配置来源。")).not.toBeInTheDocument();
  expect(screen.getByText("最终结果：配置正确。")).toBeVisible();
});

it("从执行中切换为完成时自动收起过程", async () => {
  const { rerender } = render(
    <MessageHarness message={resultMessage("running")} />,
  );
  const reasoning = screen.getByText("先检查配置来源。");
  expect(reasoning).toBeVisible();
  const process = reasoning.closest('[data-presence-layout="collapse"]');
  expect(process).toHaveAttribute("data-state", "open");
  rerender(<MessageHarness message={resultMessage()} />);
  expect(
    await screen.findByRole("button", { name: "用时 22.0s" }),
  ).toHaveAttribute("aria-expanded", "false");
  const toggle = screen.getByRole("button", { name: "用时 22.0s" });
  expect(
    document.getElementById(toggle.getAttribute("aria-controls") ?? ""),
  ).toBe(process);
  expect(process).toHaveAttribute("data-state", "closed");
  expect(screen.queryByText("先检查配置来源。")).not.toBeInTheDocument();
  expect(screen.getByText("最终结果：配置正确。")).toBeVisible();
});

it("失败时保留过程，纯正文完成消息不增加空的过程入口", async () => {
  const { rerender } = render(
    <MessageHarness message={resultMessage("failed")} />,
  );
  expect(screen.getByText("先检查配置来源。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "用时 22.0s" }),
  ).not.toBeInTheDocument();
  rerender(
    <MessageHarness
      message={{
        id: "text-only",
        role: "assistant",
        status: "completed",
        displayParts: [{ type: "text", text: "直接回复。" }],
      }}
    />,
  );
  expect(await screen.findByText("直接回复。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: /用时|查看过程/ }),
  ).not.toBeInTheDocument();
});

it("英文摘要与无耗时的历史过程入口使用对应翻译", async () => {
  await i18n.changeLanguage("en-US");
  try {
    const { rerender } = render(<MessageHarness message={resultMessage()} />);
    expect(screen.getByRole("button", { name: "Took 22.0s" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    rerender(
      <MessageHarness
        message={{
          id: "without-metrics",
          role: "assistant",
          status: "completed",
          displayParts: [
            { type: "reasoning", text: "历史思考过程。" },
            { type: "text", text: "历史最终回复。" },
          ],
        }}
      />,
    );
    const toggle = await screen.findByRole("button", { name: "View process" });
    expect(await screen.findByText("历史最终回复。")).toBeVisible();
    fireEvent.click(toggle);
    expect(screen.getByText("历史思考过程。")).toBeVisible();
  } finally {
    await i18n.changeLanguage("zh-CN");
  }
});

it("耗时与 Token 详情合并到过程入口，键盘聚焦可查看", async () => {
  render(<MessageHarness message={resultMessage()} />);
  const toggle = screen.getByRole("button", { name: "用时 22.0s" });
  fireEvent.focus(toggle);
  const details = await screen.findByRole("tooltip");
  expect(details).toHaveTextContent("耗时:");
  expect(details).toHaveTextContent("输出:");
  expect(details).toHaveTextContent("1,482");
  expect(toggle).toHaveAttribute("aria-expanded", "false");
});

it("纯正文完成消息保留底部用量入口", () => {
  render(
    <MessageHarness
      message={{
        id: "plain-with-metrics",
        role: "assistant",
        status: "completed",
        runMetrics: { durationMs: 22_000 },
        displayParts: [{ type: "text", text: "直接回复，无中间过程。" }],
      }}
    />,
  );
  expect(
    screen.getByRole("button", { name: "查看执行耗时与 Token 用量" }),
  ).toBeVisible();
  expect(screen.queryByRole("separator")).not.toBeInTheDocument();
});

it("关闭用量展示时仍可展开过程，且不暴露 Token 详情", async () => {
  render(<MessageHarness message={resultMessage()} showUsageMetrics={false} />);
  const toggle = screen.getByRole("button", { name: "用时 22.0s" });
  fireEvent.focus(toggle);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  expect(screen.getAllByText(/22\.0s/)).toHaveLength(1);
  fireEvent.click(toggle);
  expect(screen.getByText("先检查配置来源。")).toBeVisible();
});

it("完成时收起过程会把工具入口中的键盘焦点还给耗时摘要", () => {
  const view = render(<MessageHarness message={resultMessage("running")} />);
  const tool = screen.getByRole("button", { name: "已读取文件" });
  tool.focus();
  expect(document.activeElement).toBe(tool);
  view.rerender(<MessageHarness message={resultMessage()} />);
  const summary = screen.getByRole("button", { name: "用时 22.0s" });
  expect(document.activeElement).toBe(summary);
  expect(summary).toHaveAttribute("aria-expanded", "false");
});

it("截图固定语料保留13.5s、过程说明、网络组、具体搜索动作、收起和最终分隔", () => {
  render(
    <MessageHarness
      message={{
        id: "screenshot-equivalence",
        role: "assistant",
        status: "completed",
        runMetrics: { durationMs: 13_500 },
        displayParts: [
          {
            id: "commentary",
            type: "process_commentary",
            text: "我会先查阅官方资料。",
          },
          {
            id: "search",
            type: "tool",
            toolCategory: "network",
            toolAction: "search_web",
            toolStatus: "completed",
            toolLabel: "搜索 OpenAI 官方资料",
          },
          { id: "body", type: "text", text: "已经完成核对，以下是最终答复。" },
        ],
      }}
    />,
  );
  const duration = screen.getByRole("button", { name: "用时 13.5s" });
  expect(duration).toHaveAttribute("aria-expanded", "false");
  expect(screen.getByRole("separator")).toBeVisible();
  expect(screen.getByText("已经完成核对，以下是最终答复。")).toBeVisible();
  fireEvent.click(duration);
  expect(screen.getByText("我会先查阅官方资料。")).toBeVisible();
  const network = screen.getByRole("button", { name: "已访问网络" });
  expect(network).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(network);
  expect(screen.getByText("搜索 OpenAI 官方资料")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "收起" }));
  expect(network).toHaveAttribute("aria-expanded", "false");
  expect(screen.getByText("已经完成核对，以下是最终答复。")).toBeVisible();
});
