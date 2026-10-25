import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ChatWorkspaceEntry } from "./chat-page";
import { RetainedChatPageContent } from "./retained-chat-page";

const lifecycle = vi.hoisted(() => ({ open: vi.fn(), close: vi.fn() }));
beforeEach(() => vi.clearAllMocks());
// 会话探针模拟持有原生工具和输入的共用 Chat；验证业务入口的实例与活跃边界。
vi.mock("./chat-page", () => ({
  ChatPageContent: function ChatProbe({
    entry,
  }: {
    entry: ChatWorkspaceEntry;
  }) {
    const [input, setInput] = useState("");
    useEffect(() => {
      lifecycle.open(entry.scopeKey);
      return () => {
        lifecycle.close(entry.scopeKey);
      };
    }, [entry.scopeKey]);
    return (
      <section hidden={!entry.active}>
        <input
          aria-label={entry.scopeKey}
          value={input}
          onChange={(event) => setInput(event.target.value)}
        />
        <output>{entry.active ? "活跃" : "暂停"}</output>
        {typeof entry.mainContent?.content === "function"
          ? entry.mainContent?.content(Boolean(entry.active), {
              scopeKey: entry.scopeKey,
            })
          : entry.mainContent?.content}
      </section>
    );
  },
}));

it("首次接纳保留草稿输入实例，普通历史切换创建隔离实例", async () => {
  const entry: ChatWorkspaceEntry = {
    scopeKey: "draft:workflow:first",
    wakerId: "ally",
    directoryLabel: "默认",
    mainContent: { contentId: "canvas", title: "画布", content: null },
    onSend: vi.fn(),
    onAccepted: vi.fn(),
    onRefresh: vi.fn(),
    onNewChat: vi.fn(),
  };
  const view = render(<RetainedChatPageContent entry={entry} />);
  const input = screen.getByRole("textbox");
  await userEvent.setup().type(input, "后续输入");
  view.rerender(
    <RetainedChatPageContent
      chatId="accepted"
      draftBinding={{ scopeKey: entry.scopeKey, chatId: "accepted" }}
      entry={{ ...entry, scopeKey: "chat:accepted" }}
    />,
  );
  expect(screen.getByRole("textbox")).toBe(input);
  expect(input).toHaveValue("后续输入");
  view.rerender(
    <RetainedChatPageContent
      chatId="history"
      entry={{ ...entry, scopeKey: "chat:history" }}
    />,
  );
  expect(screen.getByRole("textbox")).not.toBe(input);
  expect(screen.getByRole("textbox")).toHaveValue("");
  view.rerender(
    <RetainedChatPageContent
      chatId="accepted"
      entry={{ ...entry, scopeKey: "chat:accepted" }}
    />,
  );
  expect(screen.getByRole("textbox")).toBe(input);
  view.rerender(
    <RetainedChatPageContent
      draftBinding={{ scopeKey: entry.scopeKey, chatId: "accepted" }}
      entry={{ ...entry, scopeKey: "draft:workflow:second" }}
    />,
  );
  const second = screen.getByRole("textbox");
  view.rerender(
    <RetainedChatPageContent
      chatId="accepted"
      draftBinding={{ scopeKey: entry.scopeKey, chatId: "accepted" }}
      entry={{ ...entry, scopeKey: "chat:accepted" }}
    />,
  );
  expect(screen.getByRole("textbox")).toBe(input);
  expect(screen.getByRole("textbox")).not.toBe(second);
});

it("会话切换保留输入与工具实例，隐藏会话暂停，删除和关闭释放实例", async () => {
  const props = (chatId: string) => ({
    chatId,
    entry: {
      scopeKey: `chat:${chatId}`,
      wakerId: "ally",
      directoryLabel: "默认",
      mainContent: {
        contentId: "canvas",
        title: "画布",
        content: <div>当前画布</div>,
      },
      onSend: vi.fn(),
      onAccepted: vi.fn(),
      onRefresh: vi.fn(),
      onNewChat: vi.fn(),
    },
  });
  const user = userEvent.setup();
  const view = render(<RetainedChatPageContent {...props("a")} />);
  const first = screen.getByRole("textbox", { name: "chat:a" });
  await user.type(first, "甲未发送");
  view.rerender(<RetainedChatPageContent {...props("b")} />);
  await user.type(screen.getByRole("textbox", { name: "chat:b" }), "乙未发送");
  expect(lifecycle.close).not.toHaveBeenCalled();
  expect(screen.getAllByText("当前画布")).toHaveLength(1);
  expect(screen.getAllByText("活跃")).toHaveLength(1);
  expect(screen.getAllByText("暂停")).toHaveLength(1);
  view.rerender(<RetainedChatPageContent {...props("a")} />);
  expect(screen.getByRole("textbox", { name: "chat:a" })).toBe(first);
  expect(first).toHaveValue("甲未发送");
  expect(lifecycle.open).toHaveBeenCalledTimes(2);
  view.rerender(
    <RetainedChatPageContent {...props("b")} closedChatIds={["a"]} />,
  );
  expect(screen.getByRole("textbox", { name: "chat:b" })).toHaveValue(
    "乙未发送",
  );
  expect(lifecycle.close).toHaveBeenCalledWith("chat:a");
  view.unmount();
  expect(lifecycle.close).toHaveBeenCalledTimes(2);
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
