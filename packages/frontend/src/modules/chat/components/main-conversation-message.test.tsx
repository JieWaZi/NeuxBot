import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import type * as Platform from "@/shared/platform";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type { PublicMessage } from "../api/main-conversation";
import { PublicConversationMessage } from "./main-conversation-message";

const calls = vi.hoisted(() => ({
  source: vi.fn(),
  resolve: vi.fn<typeof Platform.resolveWorkspaceResourceReference>(),
  reveal: vi.fn<typeof Platform.revealLocalPath>(),
  open: vi.fn<typeof Platform.openWorkspaceResourceScope>(),
  stat: vi.fn<typeof Platform.statWorkspaceResource>(),
  read: vi.fn<typeof Platform.readWorkspaceResource>(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  getMainConversationSource: calls.source,
}));
vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof Platform>()),
  resolveWorkspaceResourceReference: calls.resolve,
  revealLocalPath: calls.reveal,
  openWorkspaceResourceScope: calls.open,
  statWorkspaceResource: calls.stat,
  readWorkspaceResource: calls.read,
}));

const subject = { subjectType: "waker", subjectId: "partner" } as const;
const row = { id: "public", sourceChatId: "work-a" };

beforeEach(() => {
  calls.source.mockResolvedValue({
    data: { chat: { id: "work-a", workspaceDirectory: "/work/a" } },
  });
});
afterEach(() => {
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

function show(message: PublicMessage) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const openFile = vi.fn();
  const openCodeReference = vi.fn();
  const openRecord = vi.fn();
  const select = vi.fn();
  const tree = (active: boolean) => (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <PublicConversationMessage
            subject={subject}
            row={message}
            inTimeline
            active={active}
            select={select}
            openSource={() => {}}
            openRecord={openRecord}
            openFile={openFile}
            openCodeReference={openCodeReference}
          />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  const view = render(tree(true));
  return {
    openFile,
    openCodeReference,
    openRecord,
    select,
    hide: () => view.rerender(tree(false)),
  };
}

it("shows one persisted task card with the updated run time and detail link", () => {
  const taskReceipt = {
    receiptId: "update-receipt",
    taskId: "saved-task",
    name: "Review schedule",
    wakerName: "Planner",
    instructionPreview: "Review the report",
    schedule: { kind: "once" as const, timezone: "Asia/Shanghai" },
    nextRunAt: Date.parse("2026-11-29T01:00:00Z"),
    updated: true,
    enabled: true,
  };
  const message = {
    ...row,
    role: "assistant",
    content: "",
    readable: true,
    sequence: 4,
    automaticTaskReceipts: [taskReceipt],
  };
  show(message);
  expect(screen.getByText("Review schedule")).toBeInTheDocument();
  expect(screen.getByText("Review the report")).toBeInTheDocument();
  expect(screen.getAllByRole("link")).toHaveLength(1);
  expect(screen.getByRole("link")).toHaveAttribute(
    "href",
    "/automatic-tasks/saved-task",
  );
  expect(screen.getByRole("article").querySelector("time")).toHaveAttribute(
    "datetime",
    "2026-11-29T01:00:00.000Z",
  );
});

it.each(["user", "assistant"])(
  "从主会话的 %s 消息打开确切的工作流运行",
  (role) => {
    show({
      ...row,
      role,
      content: "审核流程",
      workflowRun: { definitionId: "definition", runId: "accepted-run" },
    });
    expect(screen.getByRole("link", { name: "查看运行" })).toHaveAttribute(
      "href",
      "/workflows/definition/runs/accepted-run",
    );
  },
);

it.each([undefined, { definitionId: "definition", runId: "scheduled-run" }])(
  "定时通知使用静态头像并保留运行链接 %#",
  (workflowRun) => {
    show({
      id: "scheduled-notice",
      role: "assistant",
      sourceChatId: "",
      content: "发现一项需要关注的变更。",
      workflowRun,
    });
    expect(screen.getByText("发现一项需要关注的变更。")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "打开工作会话" }),
    ).not.toBeInTheDocument();
    if (workflowRun) {
      expect(screen.getByRole("link", { name: "查看运行" })).toHaveAttribute(
        "href",
        "/workflows/definition/runs/scheduled-run",
      );
    }
  },
);

it("没有来源会话的直接答复仍可打开原问答记录", async () => {
  const { openRecord } = show({
    id: "direct-answer",
    role: "assistant",
    sourceChatId: "",
    replyToMessageId: "original-question",
    content: "这里是直接答复。",
  });
  await userEvent.click(screen.getByRole("button", { name: "打开默认会话" }));
  expect(openRecord).toHaveBeenCalledOnce();
});

it("主会话用户消息可用键盘直接回复，复制收在更多菜单", async () => {
  const user = userEvent.setup();
  const message = { ...row, role: "user", content: "收到", sequence: 3 };
  const { select } = show(message);
  const menu = screen.getByRole("button", { name: "更多操作" });
  const reply = screen.getByRole("button", { name: "回复" });
  await user.tab();
  expect(reply).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(select).toHaveBeenCalledExactlyOnceWith({
    messageId: message.id,
    role: "user",
    preview: message.content,
  });
  await user.tab();
  expect(menu).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(screen.queryByRole("menuitem", { name: "打开工作会话" })).toBeNull();
  expect(screen.queryByRole("menuitem", { name: "回复" })).toBeNull();
  await user.keyboard("{End}");
  expect(screen.getByRole("menuitem", { name: "复制消息" })).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(await navigator.clipboard.readText()).toBe(message.content);
  await waitFor(() => expect(menu).toHaveFocus());
});

it("公开答复用一张带小左上角的气泡呈现完整正文和独立回复操作", async () => {
  const text = "第一段正文\n\n第二段正文";
  const { select } = show({
    ...row,
    role: "assistant",
    content: text,
    replyToMessageId: "原交办",
  });
  const bubble = screen
    .getByText("第一段正文")
    .closest("[data-chat-message-bubble]");
  expect(bubble).toHaveClass("rounded-ss-xs");
  expect(bubble).toContainElement(screen.getByText("第二段正文"));
  expect(bubble?.querySelector("[aria-label*='原消息']")).toBeNull();
  const reply = screen.getByRole("button", { name: "回复" });
  expect(bubble).not.toContainElement(reply);
  await userEvent.click(reply);
  expect(select).toHaveBeenCalledExactlyOnceWith({
    messageId: row.id,
    role: "assistant",
    preview: text,
  });
});

it.each(["user", "assistant"])(
  "%s 的公开消息沿用普通附件入口和实际工作授权",
  async (role) => {
    const message = {
      ...row,
      role,
      content: "这是一张截图",
      attachments: [{ name: "截图.png", path: "attachments/sent/截图.png" }],
    };
    const { openFile } = show(message);
    fireEvent.click(await screen.findByRole("button", { name: "截图.png" }));
    expect(openFile).toHaveBeenCalledExactlyOnceWith({
      name: "截图.png",
      path: "attachments/sent/截图.png",
      fileSource: "chat",
      sourceChatId: "work-a",
    });
  },
);

it("未通过主会话来源核验时保留文件描述，关闭文件打开入口", async () => {
  calls.source.mockResolvedValue({
    data: { chat: { id: "different", workspaceDirectory: "/work/other" } },
  });
  const message = {
    ...row,
    role: "user",
    content: "[相对文件](./report.md)",
    attachments: [{ name: "截图原件.png", path: "attachments/截图.png" }],
  };
  const { openFile } = show(message);
  const file = await screen.findByText("截图原件.png");
  expect(file.closest("button")).toBeNull();
  const link = screen.getByRole("link", { name: "相对文件" });
  expect(link).toHaveAttribute("aria-disabled", "true");
  fireEvent.click(link);
  expect(calls.resolve).not.toHaveBeenCalled();
  expect(calls.reveal).not.toHaveBeenCalled();
  expect(openFile).not.toHaveBeenCalled();
});

it("公开代码引用保留原行号，并使用发言的工作目录", async () => {
  const reference = { path: "./src/index.ts", startLine: 3, endLine: 5 };
  const { openCodeReference } = show({
    ...row,
    role: "user",
    content: "index.ts:3-5",
    inputContent: "检查这段实现",
    codeReferences: [reference],
  });
  const chip = screen.getByRole("button", { name: "./src/index.ts (3-5)" });
  await waitFor(() => expect(chip).toBeEnabled());
  fireEvent.click(chip);
  expect(openCodeReference).toHaveBeenCalledExactlyOnceWith(reference, {
    sourceChatId: "work-a",
    rootPath: "/work/a",
  });
  expect(screen.getByText("检查这段实现")).toBeVisible();
  expect(screen.queryByText("index.ts:3-5")).not.toBeInTheDocument();
});

it("公开助手图片复用授权预览，离开主时间线时释放图片资源", async () => {
  vi.stubGlobal("IntersectionObserver", undefined);
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => "blob:public-chart");
      static revokeObjectURL = revoke;
    },
  );
  const info = {
    relativePath: "chart.png",
    size: 4,
    version: "1",
    modifiedAt: 1,
    mediaType: "image/png",
  };
  calls.resolve.mockResolvedValue({
    status: "resolved",
    canonicalPath: "/work/a/chart.png",
    baseDirectory: "/work/a",
    workspaceRoot: "/work/a",
    sourceRoots: [],
  });
  calls.open.mockResolvedValue({ id: "file-scope", root: "/work/a" });
  calls.stat.mockResolvedValue(info);
  calls.read.mockResolvedValue({ ...info, bytes: new Uint8Array(4) });
  const view = show({
    ...row,
    role: "assistant",
    content: "![报告图](./chart.png)",
  });

  fireEvent.click(await screen.findByRole("button", { name: "报告图" }));
  const preview = await screen.findByRole("dialog", { name: "报告图" });
  expect(within(preview).getByRole("img", { name: "报告图" })).toHaveAttribute(
    "src",
    "blob:public-chart",
  );
  expect(calls.resolve).toHaveBeenCalledWith(
    "work-a",
    "./chart.png",
    "/work/a",
  );
  expect(calls.open).toHaveBeenCalledWith("work-a");
  expect(calls.read).toHaveBeenCalledTimes(1);
  view.hide();
  await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:public-chart"));
  expect(preview).not.toBeInTheDocument();
});

it("公开本机绝对路径沿用显式点击的系统文件管理器定位", async () => {
  calls.resolve.mockResolvedValue({ status: "outside_scope" });
  calls.reveal.mockResolvedValue(true);
  const { openFile } = show({
    ...row,
    role: "assistant",
    content: "[打开本机日志](/Users/ryan/Documents/result.txt)",
  });
  const link = await screen.findByRole("link", { name: "打开本机日志" });
  await waitFor(() =>
    expect(link).not.toHaveAttribute("aria-disabled", "true"),
  );
  fireEvent.click(link);
  await waitFor(() =>
    expect(calls.reveal).toHaveBeenCalledExactlyOnceWith(
      "/Users/ryan/Documents/result.txt",
    ),
  );
  expect(openFile).not.toHaveBeenCalled();
});
