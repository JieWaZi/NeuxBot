import { useLocalRuntime, type ChatModelAdapter } from "@assistant-ui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCallback, useRef, useState, type ComponentProps } from "react";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type {
  BotChatMessage,
  ChatReplyDraftEditor,
  ChatReplyReference,
} from "../runtime/chat-projection";
import { ChatThread } from "./chat-thread";

const model: ChatModelAdapter = { run: async () => ({ content: [] }) };
beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
afterAll(() => vi.unstubAllGlobals());
const userMessage = {
  id: "source",
  createdSequence: 2,
  role: "user",
  content: "原始需求",
} satisfies BotChatMessage;
const assistantMessage = {
  id: "answer",
  createdSequence: 3,
  role: "assistant",
  status: "completed",
  displayParts: [
    { type: "text", text: "早期输出" },
    { type: "process_commentary", text: "内部进度" },
    { type: "tool", toolStatus: "completed" },
    { type: "text", text: "最终答复" },
  ],
} satisfies BotChatMessage;
const quotedMessage: BotChatMessage = {
  ...userMessage,
  id: "reply",
  createdSequence: 4,
  content: "补充说明",
  replyTo: { messageId: "source", role: "user", preview: "原始需求" },
};

type ThreadProps = ComponentProps<typeof ChatThread>;

function renderThread(
  props: Pick<ThreadProps, "messages"> &
    Partial<
      Pick<
        ThreadProps,
        "sendFailure" | "messageAuthors" | "teamReplyAuthorNames"
      >
    > & {
      replyEnabled?: boolean;
      draftEditor?: (editor: ChatReplyDraftEditor) => void;
      loadEarlier?: () => Promise<readonly BotChatMessage[]>;
    },
) {
  function Harness() {
    const runtime = useLocalRuntime(model);
    const [replyTo, setReplyTo] = useState<ChatReplyReference>();
    const [clearRevision, setClearRevision] = useState(0);
    const [history, setHistory] = useState<readonly BotChatMessage[]>();
    const replyDraftRef = useRef<ChatReplyDraftEditor>(null);
    const registerReplyDraftEditor = useCallback(
      (editor: ChatReplyDraftEditor) => {
        replyDraftRef.current = editor;
        return () => {
          if (replyDraftRef.current === editor) replyDraftRef.current = null;
        };
      },
      [],
    );
    return (
      <>
        <ChatThread
          messages={history ? [...history, ...props.messages] : props.messages}
          runtime={runtime}
          emptyState={null}
          wakerName="NeuxBot"
          messageAuthors={props.messageAuthors}
          teamReplyAuthorNames={props.teamReplyAuthorNames}
          sendFailure={props.sendFailure ?? { kind: "none" }}
          hasEarlierMessages={Boolean(props.loadEarlier && !history)}
          isLoadingEarlier={false}
          onLoadEarlier={async () => {
            if (props.loadEarlier) setHistory(await props.loadEarlier());
          }}
          replyTo={replyTo}
          onReplyToChange={
            props.replyEnabled === false ? undefined : setReplyTo
          }
          registerReplyDraftEditor={
            props.draftEditor ? registerReplyDraftEditor : undefined
          }
          composerClearRevision={clearRevision}
        />
        {props.draftEditor && (
          <button
            type="button"
            onClick={() => {
              if (replyDraftRef.current)
                props.draftEditor?.(replyDraftRef.current);
              setClearRevision((revision) => revision + 1);
            }}
          >
            Capture draft
          </button>
        )}
      </>
    );
  }
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <Harness />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("only gives durable text messages in this chat a reply action", () => {
  renderThread({
    messages: [
      userMessage,
      assistantMessage,
      { ...userMessage, id: "local", createdSequence: undefined },
      { ...userMessage, id: "optimistic", optimistic: true },
      {
        ...userMessage,
        id: "attachment",
        content: "",
        attachments: [{ name: "notes.md" }],
      },
      { ...userMessage, id: "other-chat", sourceChatId: "external" },
      { ...userMessage, id: "internal", actor: "lead" },
      {
        ...userMessage,
        id: "inherited",
        inherited: true,
        sourceChatId: "origin",
      },
      { ...assistantMessage, id: "working", status: "running" },
      {
        ...assistantMessage,
        id: "failed",
        status: "failed",
        displayParts: [],
        errorCode: "failure",
      },
      {
        ...assistantMessage,
        id: "process",
        status: "completed",
        displayParts: assistantMessage.displayParts?.slice(1, 3),
      },
      {
        ...assistantMessage,
        id: "delivery",
        displayParts: [
          { id: "delivery:delivery", type: "text", text: "送达摘要" },
        ],
      },
    ],
  });
  const replies = screen.getAllByRole("button", { name: "回复" });
  expect(
    replies.map((button) =>
      button.closest("[data-message-id]")?.getAttribute("data-message-id"),
    ),
  ).toEqual(["source", "answer", "inherited"]);
});

it("excludes an attachment-only prompt while allowing identical text written by the user", () => {
  const typed = {
    ...userMessage,
    id: "typed",
    content: "请查看附件。",
    attachments: [{ name: "notes.md" }],
  };
  const attachmentOnly = { ...typed, id: "attached", attachmentOnly: true };
  renderThread({ messages: [attachmentOnly, typed] });
  expect(
    screen.getByRole("button", { name: "回复" }).closest("[data-message-id]"),
  ).toHaveAttribute("data-message-id", "typed");
});

it("keeps an earlier visible terminal reply addressable after steering continues it", () => {
  renderThread({
    messages: [
      {
        ...assistantMessage,
        id: "steered",
        executionId: "original-execution",
        status: "canceled",
        displayParts: [{ type: "text", text: "被引导前的答复" }],
      },
      { ...userMessage, id: "prompt", content: "请再补充" },
      {
        ...assistantMessage,
        id: "continued",
        steerExecutionId: "original-execution",
      },
    ],
  });
  const previous = screen
    .getByText("被引导前的答复")
    .closest("[data-message-id]") as HTMLElement;
  expect(
    within(previous).getByRole("button", { name: "回复" }),
  ).toBeInTheDocument();
  expect(
    within(previous).queryByRole("button", { name: "复制" }),
  ).not.toBeInTheDocument();
});

it("opens a single reply preview, preserves the editor and keeps an existing error above it", async () => {
  const user = userEvent.setup();
  renderThread({
    messages: [userMessage, assistantMessage],
    sendFailure: { kind: "active-execution" },
  });
  const editor = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(editor, "继续编辑的草稿");

  const answer = screen
    .getByText("最终答复")
    .closest("[data-message-id]") as HTMLElement;
  await user.click(within(answer).getByRole("button", { name: "回复" }));
  const preview = screen.getByText("回复：最终答复");
  const composerSurface = editor.closest(
    "[data-chat-composer-surface]",
  ) as HTMLElement;
  expect(
    composerSurface.closest("[data-chat-composer-frame]"),
  ).toContainElement(preview);
  expect(composerSurface).not.toContainElement(preview);
  expect(
    screen.getByRole("alert").compareDocumentPosition(preview) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(editor).toHaveFocus();
  expect(editor).toHaveTextContent("继续编辑的草稿");

  const source = screen
    .getByText("原始需求")
    .closest("[data-message-id]") as HTMLElement;
  await user.click(within(source).getByRole("button", { name: "回复" }));
  expect(screen.getByText("回复：原始需求")).toBeInTheDocument();
  expect(screen.queryByText("回复：最终答复")).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "移除回复" }));
  expect(screen.queryByText("回复：原始需求")).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(editor);
  expect(editor).toHaveFocus();
  expect(editor).toHaveTextContent("继续编辑的草稿");
});

it.each([
  { source: userMessage, preview: "原始需求", author: "你" },
  { source: assistantMessage, preview: "最终答复", author: undefined },
])(
  "jumps to the quoted $source.role message and highlights only its bubble",
  async ({ source, preview, author }) => {
    const user = userEvent.setup();
    const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    try {
      renderThread({
        messages: [
          source,
          {
            ...quotedMessage,
            replyTo: { messageId: source.id, role: source.role, preview },
          },
        ],
        replyEnabled: false,
      });
      expect(
        screen.queryByRole("button", { name: "回复" }),
      ).not.toBeInTheDocument();
      const quote = screen.getByRole("button", {
        name: `定位原消息：${preview}`,
      });
      expect(quote).toHaveAccessibleDescription(author ?? preview);
      if (author) expect(within(quote).getByText(author)).toBeInTheDocument();
      const original = screen.getByText(preview, { selector: "p" });
      const target = original.closest("[data-message-id]");
      const bubble = original.closest("[data-chat-message-bubble]");
      await user.click(quote);
      await waitFor(() =>
        expect(
          jump.mock.contexts.filter((element) => element === target),
        ).toHaveLength(1),
      );
      expect(bubble).toHaveAttribute("data-chat-message-located");
      expect(target).not.toHaveAttribute("data-chat-message-located");
      expect(quote).toHaveFocus();
      expect(jump).toHaveBeenCalledWith({ block: "center" });
      await user.keyboard("{Enter}");
      await waitFor(() =>
        expect(
          jump.mock.contexts.filter((element) => element === target),
        ).toHaveLength(2),
      );
      expect(bubble).toHaveFocus();
    } finally {
      jump.mockRestore();
    }
  },
);

it("does not page when a loaded quote jumps to the top, but still pages on manual scroll", async () => {
  const user = userEvent.setup();
  const loadEarlier = vi.fn(async () => []);
  const { container } = renderThread({
    messages: [userMessage, quotedMessage],
    loadEarlier,
  });
  const viewport = container.querySelector<HTMLElement>(
    "[data-chat-scroll-viewport]",
  );
  if (!viewport) throw new Error("Missing chat viewport");
  Object.defineProperties(viewport, {
    scrollHeight: { value: 2400 },
    clientHeight: { value: 500 },
  });
  viewport.scrollTop = 240;
  fireEvent.scroll(viewport);
  const jump = vi
    .spyOn(HTMLElement.prototype, "scrollIntoView")
    .mockImplementation(() => {
      viewport.scrollTop = 12;
    });
  try {
    await user.click(
      screen.getByRole("button", { name: "定位原消息：原始需求" }),
    );
    fireEvent.scroll(viewport);
    await act(async () => vi.dynamicImportSettled());
    expect(loadEarlier).not.toHaveBeenCalled();
    viewport.scrollTop = 4;
    fireEvent.scroll(viewport);
    await waitFor(() => expect(loadEarlier).toHaveBeenCalledOnce());
  } finally {
    jump.mockRestore();
  }
});

it("uses the persisted paragraph separation in the reply draft preview", async () => {
  const user = userEvent.setup();
  const { container } = renderThread({
    messages: [
      {
        ...assistantMessage,
        displayParts: [
          { id: "first", type: "text", text: "第一段" },
          { id: "second", type: "text", text: "第二段" },
        ],
      },
    ],
  });
  await user.click(screen.getByRole("button", { name: "回复" }));
  expect(
    container.querySelector("[data-chat-composer-reply] span")?.textContent,
  ).toBe("回复：第一段\n\n第二段");
});

it("can quote typed text even when it matches a generated code reference prompt", () => {
  const content = "请查看引用的代码片段。";
  renderThread({
    messages: [
      { ...userMessage, id: "generated", content, codeReferenceOnly: true },
      { ...userMessage, id: "typed", content },
    ],
  });
  const actions = screen.getAllByRole("button", { name: "回复" });
  expect(
    actions.map((action) =>
      action.closest("[data-message-id]")?.getAttribute("data-message-id"),
    ),
  ).toEqual(["typed"]);
});

it("shows a compact partner excerpt without an author label before the original is loaded", () => {
  const replyTo = {
    messageId: "earlier",
    role: "assistant" as const,
    preview: "第一行\n第二行",
  };
  renderThread({ messages: [{ ...quotedMessage, replyTo }] });
  const quote = screen.getByRole("button", { name: /定位原消息/ });
  expect(quote.textContent).toBe(replyTo.preview);
  expect(quote).not.toHaveClass("w-full");
  expect(quote).not.toHaveClass("min-h-control-sm");
  const excerpt = within(quote).getByText("第一行 第二行");
  expect(excerpt.textContent).toBe(replyTo.preview);
  expect(excerpt).toHaveClass("line-clamp-3", "whitespace-pre-line");
});

it("uses the team roster to show the author before a team's original message is loaded", () => {
  const replyTo = {
    messageId: "earlier",
    role: "assistant" as const,
    sourceWakerId: "member",
    preview: "较早的成员报告",
  };
  renderThread({
    messages: [{ ...quotedMessage, replyTo }],
    teamReplyAuthorNames: { member: "lumi" },
  });
  const quote = screen.getByRole("button", { name: /定位原消息/ });
  expect(quote).toHaveAccessibleDescription("lumi");
  expect(within(quote).getByText("lumi")).toBeVisible();
});

it("reveals and quotes the team's committed report using its main timeline message", async () => {
  const user = userEvent.setup();
  const report = {
    ...userMessage,
    id: "report",
    role: "user",
    actor: "team_member",
    sourceChatId: "member-chat",
    sourceWakerId: "member",
    content: "成员报告正文",
  } satisfies BotChatMessage;
  renderThread({
    messages: [
      report,
      {
        ...quotedMessage,
        replyTo: {
          messageId: report.id,
          role: "assistant",
          preview: report.content,
        },
      },
    ],
    messageAuthors: {
      report: {
        name: "lumi",
        wakerId: "member",
        displayAsParticipant: true,
        kind: "member-report",
      },
    },
    teamReplyAuthorNames: { member: "lumi" },
  });
  expect(screen.queryByText(report.content, { selector: "p" })).toBeNull();
  await user.click(
    screen.getByRole("button", { name: `定位原消息：${report.content}` }),
  );
  const original = await screen.findByText(report.content, { selector: "p" });
  const row = original.closest<HTMLElement>("[data-message-id]");
  if (!row) throw new Error("缺少成员报告消息");
  await user.click(within(row).getByRole("button", { name: "回复" }));
  expect(screen.getByText(`回复：${report.content}`)).toBeVisible();
});

it("loads an earlier history window before jumping to its quoted message", async () => {
  const user = userEvent.setup();
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    renderThread({
      messages: [quotedMessage],
      loadEarlier: async () => [userMessage],
    });
    await user.click(
      screen.getByRole("button", { name: "定位原消息：原始需求" }),
    );
    const target = (
      await screen.findByText("原始需求", { selector: "p" })
    ).closest("[data-message-id]");
    await waitFor(() => expect(jump.mock.contexts).toContain(target));
  } finally {
    jump.mockRestore();
  }
});

it("does not jump when earlier history arrives after canceling the location", async () => {
  const user = userEvent.setup();
  let resolveHistory: (messages: BotChatMessage[]) => void = () => {};
  const history = new Promise<BotChatMessage[]>((resolve) => {
    resolveHistory = resolve;
  });
  renderThread({
    messages: [quotedMessage],
    loadEarlier: () => history,
  });
  await user.click(
    screen.getByRole("button", { name: "定位原消息：原始需求" }),
  );
  await user.click(await screen.findByRole("button", { name: "取消" }));
  await act(async () => resolveHistory([userMessage]));
  const target = (
    await screen.findByText("原始需求", { selector: "p" })
  ).closest("[data-message-id]");
  expect(target?.querySelector("[data-chat-message-located]")).toBeNull();
  expect(screen.queryByText("正在定位原消息…")).not.toBeInTheDocument();
});

it("explains when a quoted source is unavailable", async () => {
  const user = userEvent.setup();
  renderThread({
    messages: [
      {
        ...userMessage,
        id: "reply",
        createdSequence: 4,
        content: "补充说明",
        replyTo: {
          messageId: "missing",
          role: "assistant",
          preview: "历史原文",
        },
      },
    ],
  });
  await user.click(
    screen.getByRole("button", { name: "定位原消息：历史原文" }),
  );
  expect(await screen.findByText("原消息不可用")).toBeInTheDocument();
});

it("restores each captured editor document independently of later clears", async () => {
  const user = userEvent.setup();
  const restores: (() => void)[] = [];
  renderThread({
    messages: [],
    draftEditor: (editor) => {
      expect(editor.isEmpty()).toBe(false);
      restores.push(editor.capture());
    },
  });
  const editor = screen.getByRole("textbox", { name: "输入消息" });
  const capture = screen.getByRole("button", { name: "Capture draft" });
  await user.type(editor, "第一份草稿");
  await user.click(capture);
  await user.type(editor, "第二份草稿");
  await user.click(capture);
  expect(editor).toHaveTextContent("");
  act(() => restores[0]?.());
  expect(editor).toHaveTextContent("第一份草稿");
  act(() => restores[1]?.());
  expect(editor).toHaveTextContent("第二份草稿");
});
