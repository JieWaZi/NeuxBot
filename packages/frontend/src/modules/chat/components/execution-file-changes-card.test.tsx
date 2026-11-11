import { ContentVisibility } from "@/shared/lib/content-visibility";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { ExecutionFileChangesCard } from "./execution-file-changes-card";
import type { AllyChatMessage } from "../runtime/chat-projection";

const { readSummary } = vi.hoisted(() => ({ readSummary: vi.fn() }));
vi.mock("../api/chat-queries", () => ({
  getChatMessageFileChangesOptions: (options: unknown) => ({
    queryKey: [{ _id: "getChatMessageFileChanges", ...(options as object) }],
    queryFn: () => readSummary(options),
  }),
}));
const message: AllyChatMessage = {
  id: "reply",
  role: "assistant",
  status: "completed",
  fileChangeRecord: {
    recordId: "snapshot",
    version: 2,
    state: "ready",
    totalFiles: 4,
    complete: true,
  },
};
function mount(value = message) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>
      <FileReferenceProvider
        context={{
          chatId: "chat",
          workspaceRoot: "/project",
          sourceRoots: ["/project"],
          baseDirectory: "/project",
        }}
      >
        {children}
      </FileReferenceProvider>
    </QueryClientProvider>
  );
  const onOpenFile = vi.fn();
  const view = render(
    <ExecutionFileChangesCard
      visible
      message={value}
      onOpenFile={onOpenFile}
    />,
    { wrapper },
  );
  return { ...view, onOpenFile, client };
}
afterEach(() => {
  vi.unstubAllGlobals();
});
beforeEach(() => {
  readSummary.mockReset();
});
describe("independent workspace outcome", () => {
  it("切换会话后读取已完成成果的明细时保留文件数，不显示重新采集", async () => {
    const summary = {
      state: "ready",
      totalFiles: 1,
      complete: true,
      files: [
        {
          rootId: "root",
          path: "report.md",
          absolutePath: "/project/report.md",
          content: "text",
        },
      ],
    };
    let resolveSummary!: (value: typeof summary) => void;
    readSummary.mockReturnValue(
      new Promise<typeof summary>((resolve) => {
        resolveSummary = resolve;
      }),
    );
    const completed = {
      ...message,
      fileChangeRecord: { ...message.fileChangeRecord, totalFiles: 1 },
    };
    const client = new QueryClient();
    const onOpenFile = vi.fn();
    const scene = (active: boolean) => (
      <QueryClientProvider client={client}>
        <ContentVisibility value={active}>
          <ExecutionFileChangesCard
            visible
            chatId="chat"
            message={completed}
            onOpenFile={onOpenFile}
          />
        </ContentVisibility>
      </QueryClientProvider>
    );
    const view = render(scene(true));
    await waitFor(() => expect(readSummary).toHaveBeenCalledTimes(1));
    expect(screen.getByText("已编辑 1 个文件")).toBeVisible();
    expect(screen.queryByText("正在采集文件改动…")).toBeNull();
    expect(screen.getByRole("button", { name: "查看变更" })).toBeDisabled();
    expect(view.container.querySelector("[data-work-result-body]")).toBeNull();

    view.rerender(scene(false));
    view.rerender(scene(true));
    expect(screen.getByText("已编辑 1 个文件")).toBeVisible();
    expect(screen.queryByText("正在采集文件改动…")).toBeNull();
    await act(async () => resolveSummary(summary));
    await screen.findByText("report.md");
    expect(screen.getByRole("button", { name: "查看变更" })).toBeEnabled();
    expect(
      view.container.querySelector("[data-work-result-body]"),
    ).not.toBeNull();
    expect(readSummary).toHaveBeenCalledTimes(1);
  });
  it("完整统计汇总到标题下，列表使用相对路径并保留真实打开身份", async () => {
    readSummary.mockResolvedValue({
      state: "ready",
      totalFiles: 2,
      complete: true,
      roots: [{ id: "root", path: "/project" }],
      files: [
        {
          rootId: "root",
          path: "src/main.ts",
          absolutePath: "/project/src/main.ts",
          content: "text",
          added: 4,
          deleted: 3,
        },
        {
          rootId: "root",
          path: "docs/guide.md",
          absolutePath: "/project/docs/guide.md",
          content: "text",
          added: 11,
          deleted: 7,
        },
      ],
    });
    const { onOpenFile, container } = mount();
    expect(await screen.findByText("+15")).toBeVisible();
    expect(screen.getByText("-10")).toBeVisible();
    expect(screen.getByTitle("/project/src/main.ts")).toHaveTextContent(
      "src/main.ts",
    );
    expect(screen.getByText("main.ts")).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "查看 /project/src/main.ts 的更改" }),
    );
    expect(onOpenFile).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "/project/src/main.ts",
        rootPath: "/project",
      }),
    );
    expect(screen.queryByRole("button", { name: "撤销" })).toBeNull();
    expect(screen.getByRole("button", { name: "查看变更" })).toBeEnabled();
    expect(container.querySelectorAll("li")).toHaveLength(2);
  });
  it("单文件查看变更直接打开该消息的差异", async () => {
    readSummary.mockResolvedValue({
      state: "ready",
      totalFiles: 1,
      complete: true,
      roots: [{ id: "root", path: "/project" }],
      files: [
        {
          rootId: "root",
          path: "src/main.ts",
          absolutePath: "/project/src/main.ts",
          content: "text",
        },
      ],
    });
    const { onOpenFile } = mount();
    await screen.findByText("main.ts");
    await userEvent.click(screen.getByRole("button", { name: "查看变更" }));
    expect(onOpenFile).toHaveBeenCalledExactlyOnceWith({
      messageId: "reply",
      path: "/project/src/main.ts",
      name: "main.ts",
      rootPath: "/project",
    });
  });
  it("精确旧成果保留原路径并且不把文件身份当作目录范围", async () => {
    const path = "/project/outputs/yesterday/report.txt";
    readSummary.mockResolvedValue({
      state: "ready",
      totalFiles: 1,
      complete: true,
      roots: [{ id: "selected", path, file: true }],
      files: [
        {
          rootId: "selected",
          path: "report.txt",
          absolutePath: path,
          content: "text",
        },
      ],
    });
    const { onOpenFile } = mount();
    await screen.findByText("report.txt");
    await userEvent.click(screen.getByRole("button", { name: "查看变更" }));
    expect(onOpenFile).toHaveBeenCalledExactlyOnceWith({
      messageId: "reply",
      path,
      name: "report.txt",
      rootPath: undefined,
    });
  });
  it("多文件入口可选择折叠行，沿用原消息和文件身份且不重复读取摘要", async () => {
    readSummary.mockResolvedValue({
      state: "ready",
      totalFiles: 4,
      complete: true,
      roots: [{ id: "root", path: "/project" }],
      files: ["a.ts", "b.ts", "c.ts", "d.ts"].map((path) => ({
        rootId: "root",
        path,
        absolutePath: `/project/${path}`,
        content: "text",
      })),
    });
    const { onOpenFile } = mount();
    await screen.findByText("a.ts");
    expect(screen.queryByText("d.ts")).toBeNull();
    const user = userEvent.setup();
    const action = screen.getByRole("button", { name: "查看变更" });
    action.focus();
    await user.keyboard("{Enter}");
    await screen.findByRole("menuitem", { name: "d.ts" });
    await user.keyboard("{End}{Enter}");
    expect(onOpenFile).toHaveBeenCalledExactlyOnceWith({
      messageId: "reply",
      path: "/project/d.ts",
      name: "d.ts",
      rootPath: "/project",
    });
    expect(readSummary).toHaveBeenCalledTimes(1);
  });
  it("摘要按需读取，原始路径与未知统计保留，展开及打开使用同一成果", async () => {
    const longPath = "/project/" + "deep/".repeat(30) + "new.txt";
    readSummary.mockResolvedValue({
      state: "ready",
      totalFiles: 4,
      complete: true,
      roots: [{ id: "root", path: "/project" }],
      files: [
        {
          rootId: "root",
          path: longPath.slice("/project/".length),
          absolutePath: longPath,
          status: "added",
          content: "text",
          added: 0,
          deleted: 0,
        },
        ...["b", "c", "d"].map((path) => ({
          rootId: "root",
          path,
          absolutePath: "/project/" + path,
          status: "modified",
          content: "binary",
        })),
      ],
    });
    const { onOpenFile } = mount();
    expect(await screen.findByTitle(longPath)).toHaveTextContent(
      longPath.slice("/project/".length),
    );
    expect(readSummary).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("d")).toBeNull();
    expect(screen.getByText("+0")).toBeTruthy();
    expect(screen.getAllByText("+0")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "再显示 1 个文件" }));
    expect(screen.getByText("d")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: `查看 ${longPath} 的更改` }),
    );
    expect(onOpenFile).toHaveBeenCalledWith(
      expect.objectContaining({
        messageId: "reply",
        path: longPath,
        rootPath: "/project",
      }),
    );
    expect(readSummary).toHaveBeenCalledTimes(1);
  });
  it.each(["pending", "ready", "partial", "unavailable", "incomplete"])(
    "%s 没有确认的文件变更时不展示卡片也不读取摘要",
    (state) => {
      const view = mount({
        ...message,
        fileChangeRecord: {
          ...message.fileChangeRecord,
          state,
          totalFiles: 0,
          complete: state === "ready",
        },
      });
      expect(view.container).toBeEmptyDOMElement();
      expect(readSummary).not.toHaveBeenCalled();
    },
  );
  it("pending不读半成品，晚到真实变更主动读新成果且失败可见", async () => {
    readSummary.mockResolvedValue({
      state: "partial",
      complete: false,
      totalFiles: 1,
      files: [{ rootId: "r", path: "changed.txt", content: "text" }],
    });
    const pending = {
      ...message,
      fileChangeRecord: {
        ...message.fileChangeRecord,
        state: "pending",
        version: 1,
        totalFiles: 0,
      },
    };
    const view = mount(pending);
    expect(view.container).toBeEmptyDOMElement();
    expect(readSummary).not.toHaveBeenCalled();
    view.rerender(<ExecutionFileChangesCard visible message={message} />);
    await screen.findByText("部分范围未覆盖");
    expect(readSummary).toHaveBeenCalledTimes(1);
    readSummary.mockImplementation(async () => {
      throw new Error("unavailable");
    });
    view.rerender(
      <ExecutionFileChangesCard
        visible
        message={{
          ...message,
          fileChangeRecord: { ...message.fileChangeRecord, version: 3 },
        }}
      />,
    );
    await waitFor(() =>
      expect(screen.getByText("文件改动暂时无法读取")).toBeTruthy(),
    );
    expect(view.container.querySelector("[data-work-result-body]")).toBeNull();
    readSummary.mockResolvedValue({
      state: "ready",
      complete: true,
      totalFiles: 1,
      files: [
        {
          rootId: "r",
          path: "retry",
          absolutePath: "/project/retry",
          status: "modified",
          content: "text",
        },
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await screen.findByText("retry");
  });
  it("旧工具摘要不产生文件成果卡", () => {
    mount({
      ...message,
      fileChangeRecord: undefined,
      fileChanges: [{ path: "invented.txt", addedLines: 10 }],
    } as AllyChatMessage);
    expect(screen.queryByRole("region")).toBeNull();
    expect(readSummary).not.toHaveBeenCalled();
  });
});

it("历史摘要首次进入视区才读取，隐藏Scene和零改动不读", async () => {
  const activations: {
    callback: IntersectionObserverCallback;
    element?: Element;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      item: (typeof activations)[number];
      constructor(callback: IntersectionObserverCallback) {
        this.item = { callback };
        activations.push(this.item);
      }
      observe(element: Element) {
        this.item.element = element;
      }
      unobserve() {
        this.item.element = undefined;
      }
      disconnect() {
        this.item.element = undefined;
      }
    },
  );
  readSummary.mockResolvedValue({
    state: "ready",
    totalFiles: 1,
    complete: true,
    files: [
      {
        rootId: "root",
        path: "a",
        absolutePath: "/project/a",
        status: "modified",
        content: "text",
      },
    ],
  });
  const view = mount();
  expect(readSummary).not.toHaveBeenCalled();
  expect(view.container.querySelector("[data-work-result-body]")).toBeNull();
  const observer = activations.find((item) => item.element);
  await act(async () => {
    observer?.callback(
      [
        {
          target: observer.element,
          isIntersecting: true,
        } as IntersectionObserverEntry,
      ],
      {} as IntersectionObserver,
    );
    await Promise.resolve();
  });
  await screen.findByText("a");
  expect(readSummary).toHaveBeenCalledTimes(1);
  expect(
    view.container.querySelector(
      '[data-work-result-body][data-file-change-record-id="snapshot"][data-file-change-record-version="2"]',
    ),
  ).not.toBeNull();
  view.unmount();
  readSummary.mockClear();
  const hidden = render(
    <QueryClientProvider client={new QueryClient()}>
      <ContentVisibility value={false}>
        <ExecutionFileChangesCard visible chatId="chat" message={message} />
      </ContentVisibility>
    </QueryClientProvider>,
  );
  expect(readSummary).not.toHaveBeenCalled();
  hidden.unmount();
  const empty = mount({
    ...message,
    fileChangeRecord: {
      ...message.fileChangeRecord,
      state: "ready",
      totalFiles: 0,
    },
  });
  expect(empty.container).toBeEmptyDOMElement();
  expect(readSummary).not.toHaveBeenCalled();
  empty.unmount();
  vi.unstubAllGlobals();
});
