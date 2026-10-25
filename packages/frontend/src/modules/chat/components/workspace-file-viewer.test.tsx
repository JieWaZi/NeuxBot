import type { FileReferenceRequest } from "@/shared/platform";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceFileViewer } from "./workspace-file-viewer";

const nativeView = vi.hoisted(() => ({
  create: vi.fn().mockResolvedValue("html-preview"),
  close: vi.fn().mockResolvedValue(undefined),
}));
const scoped = vi.hoisted(() => ({
  open: vi.fn(async () => ({ id: "scope", root: "/tmp/ally" })),
  stat: vi.fn(async () => ({
    relativePath: "guide.md",
    size: 5,
    version: "version-1",
    modifiedAt: 1,
    mediaType: "text/markdown",
  })),
  read: vi.fn(async () => ({
    relativePath: "guide.md",
    size: 5,
    version: "version-1",
    modifiedAt: 1,
    mediaType: "text/markdown",
    bytes: new TextEncoder().encode("# 初版"),
  })),
  observe: vi.fn(async () => async () => {}),
  saveAs: vi.fn(async () => true),
  openExternal: vi.fn(async () => true),
  write: vi.fn(async () => ({
    resource: {
      relativePath: "guide.md",
      size: 5,
      version: "version-3",
      modifiedAt: 3,
      mediaType: "text/markdown",
    },
    saveRevision: 1,
  })),
}));

const { getChatMessageFileChange, readWorkspaceDiff, readWorkspaceFile } =
  vi.hoisted(() => ({
    getChatMessageFileChange: vi.fn(
      async (_chatId: string, _messageId: string, path: string) => ({
        path,
        oldContent: "const value = 1;\n",
        newContent: "const value = 2;\n",
      }),
    ),
    readWorkspaceDiff: vi.fn<
      (root: string, relativePath: string) => Promise<never>
    >(async () => {
      throw new Error("当前目录不是 Git 仓库");
    }),
    readWorkspaceFile: vi.fn<
      (
        root: string,
        relativePath: string,
      ) => Promise<{
        name: string;
        relativePath: string;
        kind: "text";
        mediaType: string;
        content: string;
        size: number;
      }>
    >(async () => ({
      name: "main.ts",
      relativePath: "src/main.ts",
      kind: "text",
      mediaType: "text/plain; charset=utf-8",
      content: "export const ally = true;",
      size: 25,
    })),
  }));

vi.mock("./use-browser-surface", async () => {
  const { useEffect, useRef } = await import("react");
  return {
    useBrowserSurface: (props: {
      url: string;
      workspaceRoot: string;
      chatId?: string;
    }) => {
      const hostRef = useRef<HTMLDivElement>(null);
      useEffect(() => {
        const host = hostRef.current;
        if (!host) throw new Error("测试预览容器尚未挂载");
        const rect = host.getBoundingClientRect();
        void nativeView.create(
          props.url,
          { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
          props.workspaceRoot,
          props.chatId,
        );
        return () => {
          void nativeView.close("html-preview");
        };
      }, [props.url, props.workspaceRoot, props.chatId]);
      return {
        hostRef,
        browserReady: true,
        error: "",
        runBrowserAction: () => {},
      };
    },
  };
});

vi.mock("@/shared/platform", async () => {
  const { fakeFileReference } = await import("@/test/file-reference-fake");
  return {
    resolveFileReferencePath: async (request: FileReferenceRequest) =>
      fakeFileReference(request),
    isDesktopRuntime: () => true,
    hasPlatformService: () => true,
    createBrowserView: nativeView.create,
    closeBrowserView: nativeView.close,
    setBrowserViewBounds: vi.fn().mockResolvedValue(undefined),
    setBrowserViewVisible: vi.fn().mockResolvedValue(undefined),
    navigateBrowserView: vi.fn().mockResolvedValue(undefined),
    controlBrowserView: vi.fn().mockResolvedValue(undefined),
    subscribeBrowserNavigation: () => () => {},
    readWorkspaceFile: (root: string, relativePath: string) =>
      readWorkspaceFile(root, relativePath),
    readWorkspaceDiff: (root: string, relativePath: string) =>
      readWorkspaceDiff(root, relativePath),
    openWorkspaceResourceScope: scoped.open,
    statWorkspaceResource: scoped.stat,
    readWorkspaceResource: scoped.read,
    observeWorkspaceResource: scoped.observe,
    saveWorkspaceResourceAs: scoped.saveAs,
    openWorkspaceResourceExternal: scoped.openExternal,
    writeWorkspaceResource: scoped.write,
  };
});

vi.mock("../api/chat-queries", () => ({
  getChatMessageFileChangeOptions: (options: {
    path: { chatId: string; messageId: string };
    query: { path: string };
  }) => ({
    queryKey: ["execution-file-change", options],
    queryFn: () =>
      getChatMessageFileChange(
        options.path.chatId,
        options.path.messageId,
        options.query.path,
      ),
  }),
}));

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

describe("WorkspaceFileViewer", () => {
  it("文件工作台将操作放在顶层工具栏且不重复显示文件名", async () => {
    const toolbarTarget = document.createElement("div");
    document.body.append(toolbarTarget);
    const view = render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer
            rootPath="/tmp/project"
            path="guide.md"
            toolbarTarget={toolbarTarget}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    try {
      await waitFor(() =>
        expect(
          toolbarTarget.querySelector("[data-workspace-file-header]"),
        ).not.toBeNull(),
      );
      const header = toolbarTarget.querySelector(
        "[data-workspace-file-header]",
      ) as HTMLElement;
      expect(header).not.toHaveTextContent("guide.md");
      expect(
        within(header).getByRole("button", { name: "编辑 Markdown" }),
      ).toBeVisible();
      expect(
        view.container.querySelector("[data-workspace-file-header]"),
      ).toBeNull();
    } finally {
      view.unmount();
      toolbarTarget.remove();
    }
  });

  it("Markdown 文档链接以文档目录解析并使用共享文件引用展示", async () => {
    const onOpenFile = vi.fn();
    readWorkspaceFile.mockResolvedValueOnce({
      name: "guide.md",
      relativePath: "docs/guide.md",
      kind: "text",
      mediaType: "text/markdown",
      content: "[a.txt](../a.txt)",
      size: 20,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer
            rootPath="/project"
            path="docs/guide.md"
            sourceRoots={["/project"]}
            onOpenFile={onOpenFile}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    const link = await screen.findByRole("link", { name: "a.txt" });
    await waitFor(() =>
      expect(link).toHaveAttribute("data-file-path", "/project/a.txt"),
    );
    await userEvent.setup().click(link);
    expect(onOpenFile).toHaveBeenCalledWith(
      expect.objectContaining({
        canonicalPath: "/project/a.txt",
        displayPath: "a.txt",
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("Markdown 在同一查看器切换代码与预览，代码查看不进入编辑状态", async () => {
    const user = userEvent.setup();
    readWorkspaceFile.mockResolvedValueOnce({
      name: "guide.md",
      relativePath: "guide.md",
      kind: "text",
      mediaType: "text/markdown",
      content: "# 使用指南",
      size: 16,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer rootPath="/tmp/project" path="guide.md" />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("heading", { name: "使用指南" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "代码" }));
    expect(
      screen.queryByRole("heading", { name: "使用指南" }),
    ).not.toBeInTheDocument();
    expect(
      document.querySelector("[data-selectable-code-viewer]"),
    ).toHaveTextContent("# 使用指南");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "预览" }));
    expect(
      await screen.findByRole("heading", { name: "使用指南" }),
    ).toBeInTheDocument();
  });

  it("只读产出保留代码与预览切换但没有编辑入口", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      name: "report.md",
      relativePath: "report.md",
      kind: "text",
      mediaType: "text/markdown",
      content: "# 只读报告",
      size: 16,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer
            rootPath="/tmp/project"
            path="report.md"
            readOnly
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "只读报告" });
    expect(
      screen.queryByRole("button", { name: "编辑 Markdown" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "代码" }));
    expect(
      document.querySelector("[data-selectable-code-viewer]"),
    ).toHaveTextContent("# 只读报告");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "预览" }));
    await screen.findByRole("heading", { name: "只读报告" });
  });

  it("编辑 Markdown 时预览当前草稿并可返回同一编辑器", async () => {
    const user = userEvent.setup();
    readWorkspaceFile.mockResolvedValueOnce({
      name: "guide.md",
      relativePath: "guide.md",
      kind: "text",
      mediaType: "text/markdown",
      content: "# 初版",
      size: 8,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer rootPath="/tmp/project" path="guide.md" />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "初版" });
    await user.click(screen.getByRole("button", { name: "编辑 Markdown" }));
    const editor = await screen.findByRole(
      "textbox",
      { name: "文件编辑器" },
      { timeout: 5_000 },
    );
    await user.clear(editor);
    await user.type(editor, "# 草稿版本");
    await user.click(screen.getByRole("button", { name: "预览" }));
    expect(
      await screen.findByRole("heading", { name: "草稿版本" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "代码" }));
    const resumed = await screen.findByRole("textbox", {
      name: "文件编辑器",
    });
    expect(resumed).toBe(editor);
    expect(resumed).toHaveValue("# 草稿版本");
  });

  it("HTML 默认代码，预览默认互动并在工具栏切换静态", async () => {
    const user = userEvent.setup();
    nativeView.create.mockClear();
    nativeView.close.mockClear();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(10, 20, 800, 400),
    );
    readWorkspaceFile.mockResolvedValueOnce({
      name: "index.html",
      relativePath: "index.html",
      kind: "text",
      mediaType: "text/html",
      content: "<h1>Hello</h1>",
      size: 14,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorkspaceFileViewer rootPath="/tmp/project" path="index.html" />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("button", { name: "预览" })).toBeEnabled();
    expect(nativeView.create).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "预览" }));
    await waitFor(() =>
      expect(nativeView.create).toHaveBeenCalledWith(
        "file:///tmp/project/index.html",
        { x: 10, y: 20, width: 800, height: 400 },
        "/tmp/project",
        undefined,
      ),
    );
    await user.click(screen.getByRole("button", { name: "静态预览" }));
    expect(await screen.findByTitle("静态预览")).toHaveAttribute("sandbox", "");
    await user.click(screen.getByRole("button", { name: "代码" }));
    await waitFor(() =>
      expect(nativeView.close).toHaveBeenCalledWith("html-preview"),
    );
    expect(
      document.querySelector("[data-selectable-code-viewer]"),
    ).toHaveTextContent("<h1>Hello</h1>");
  });
  it("执行文件改动从 Chat JSONL 接口读取且不依赖 Git", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            mode="diff"
            path="projects/Desktop/hello_world.html"
            rootPath="/tmp/chat"
            executionDiff={{
              chatId: "chat-1",
              messageId: "assistant-1",
            }}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(getChatMessageFileChange).toHaveBeenCalledWith(
        "chat-1",
        "assistant-1",
        "projects/Desktop/hello_world.html",
      ),
    );
    expect(readWorkspaceDiff).not.toHaveBeenCalled();
    expect(screen.queryByText("文件暂时无法读取")).not.toBeInTheDocument();
  });

  it("Markdown 渲染视图选中文字后显示添加到对话", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      name: "AGENTS.md",
      relativePath: "AGENTS.md",
      kind: "text",
      mediaType: "text/markdown; charset=utf-8",
      content: "# 仓库运行时入口\n\n关联 DevWiki",
      size: 30,
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            path="AGENTS.md"
            rootPath="/tmp/ally"
            onAddCodeReference={vi.fn()}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    const heading = await screen.findByText("仓库运行时入口");
    const textNode = heading.firstChild;
    if (!textNode) throw new Error("Markdown 标题缺少文本节点");
    const range = document.createRange();
    range.selectNodeContents(textNode);
    vi.spyOn(range, "getBoundingClientRect").mockReturnValue(
      new DOMRect(100, 100, 200, 20),
    );
    const selection = window.getSelection();
    selection?.removeAllRanges();
    fireEvent.mouseDown(heading);
    selection?.addRange(range);
    fireEvent.mouseUp(document);

    expect(
      await screen.findByRole("button", { name: "添加到对话" }),
    ).toBeInTheDocument();
    const action = screen.getByRole("button", { name: "添加到对话" });
    expect(action).toHaveClass("whitespace-nowrap");
    expect(
      action.closest("[data-selection-action-popover]")?.parentElement,
    ).toBe(document.body);
  });

  it("多行选区的添加按钮跟随最后一段文字而不是整块外接矩形", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      name: "AGENTS.md",
      relativePath: "AGENTS.md",
      kind: "text",
      mediaType: "text/markdown",
      content: "# 仓库运行时入口\n\n关联 DevWiki",
      size: 30,
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            path="AGENTS.md"
            rootPath="/tmp/ally"
            onAddCodeReference={vi.fn()}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    const heading = await screen.findByText("仓库运行时入口");
    const range = document.createRange();
    range.selectNodeContents(heading);
    vi.spyOn(range, "getBoundingClientRect").mockReturnValue(
      new DOMRect(200, 100, 800, 120),
    );
    vi.spyOn(range, "getClientRects").mockReturnValue([
      new DOMRect(200, 100, 100, 20),
      new DOMRect(220, 200, 60, 20),
    ] as unknown as DOMRectList);
    fireEvent.mouseDown(heading);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);
    fireEvent.mouseUp(document);
    const action = await screen.findByRole("button", { name: "添加到对话" });
    expect(
      action.closest<HTMLElement>("[data-selection-action-popover]"),
    ).toHaveStyle({
      left: "250px",
      top: "192px",
    });
  });

  it("非 Markdown 文本在 Light DOM 中选择后显示添加到对话", async () => {
    const onAddCodeReference = vi.fn();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            path="src/main.ts"
            rootPath="/tmp/ally"
            onAddCodeReference={onAddCodeReference}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    const textElement = await waitFor(() => {
      const line = document.querySelector<HTMLElement>("[data-code-line]");
      expect(line).toHaveTextContent("export const ally = true;");
      return line;
    });
    if (!textElement) throw new Error("代码行缺少 Light DOM 内容");
    const range = document.createRange();
    range.selectNodeContents(textElement);
    vi.spyOn(range, "getBoundingClientRect").mockReturnValue(
      new DOMRect(100, 100, 200, 20),
    );
    const selection = window.getSelection();
    selection?.removeAllRanges();
    fireEvent.mouseDown(textElement);
    selection?.addRange(range);
    fireEvent.mouseUp(document);

    expect(
      await screen.findByRole("button", { name: "添加到对话" }),
    ).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(
      screen.queryByRole("button", { name: "添加到对话" }),
    ).not.toBeInTheDocument();
    fireEvent(document, new Event("selectionchange"));
    await screen.findByRole("button", { name: "添加到对话" });
    fireEvent.scroll(textElement);
    expect(
      screen.queryByRole("button", { name: "添加到对话" }),
    ).not.toBeInTheDocument();
    fireEvent(document, new Event("selectionchange"));
    await screen.findByRole("button", { name: "添加到对话" });
    fireEvent.click(screen.getByRole("button", { name: "添加到对话" }));
    expect(onAddCodeReference).toHaveBeenCalledWith({
      id: expect.any(String),
      path: "src/main.ts",
      startLine: 1,
      endLine: 1,
      content: "export const ally = true;",
    });
    expect(window.getSelection()?.isCollapsed).toBe(true);
    expect(
      screen.queryByRole("button", { name: "添加到对话" }),
    ).not.toBeInTheDocument();
  });

  it("读取工作区文件并通过 Light DOM 渲染", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer path="src/main.ts" rootPath="/tmp/ally" />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(container.querySelector("[data-code-line]")).toHaveTextContent(
        "export const ally = true;",
      ),
    );
    const viewer = container.querySelector("[data-selectable-code-viewer]");
    expect(viewer).toHaveTextContent("export const ally = true;");
    expect(viewer?.shadowRoot).toBeNull();
    expect(viewer).toHaveAttribute("data-wrap", "scroll");
    expect(screen.getByRole("button", { name: "编辑文本" })).toBeEnabled();
    expect(readWorkspaceFile).toHaveBeenCalledWith("/tmp/ally", "src/main.ts");

    expect(container.querySelector("[data-workspace-file-header]")).toHaveClass(
      "h-row-navigation",
      "bg-surface",
    );
  });

  it("编辑期间外部版本变化时沿用开始编辑的版本并保留冲突草稿", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            path="guide.md"
            rootPath="/tmp/ally"
            scopeChatId={"a".repeat(32)}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "初版" });
    await user.click(screen.getByRole("button", { name: "编辑 Markdown" }));
    const editor = await screen.findByRole(
      "textbox",
      { name: "文件编辑器" },
      { timeout: 10_000 },
    );
    await user.clear(editor);
    await user.type(editor, "# 我的草稿");

    scoped.stat.mockResolvedValueOnce({
      relativePath: "guide.md",
      size: 5,
      version: "version-2",
      modifiedAt: 2,
      mediaType: "text/markdown",
    });
    scoped.read.mockResolvedValueOnce({
      relativePath: "guide.md",
      size: 5,
      version: "version-2",
      modifiedAt: 2,
      mediaType: "text/markdown",
      bytes: new TextEncoder().encode("# 外部修改"),
    });
    await queryClient.invalidateQueries({
      queryKey: ["workspace-file", "a".repeat(32), "/tmp/ally", "guide.md"],
    });
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("文件已在外部修改"),
    );
    scoped.write.mockRejectedValueOnce(new Error("conflict"));
    await user.click(screen.getByRole("button", { name: "保存文件" }));
    await waitFor(() =>
      expect(scoped.write).toHaveBeenCalledWith(
        "scope",
        "guide.md",
        "# 我的草稿",
        "version-1",
        expect.any(Number),
        "/tmp/ally",
      ),
    );
    expect(editor).toHaveTextContent("# 我的草稿");
    expect(
      screen.getByRole("button", { name: "保存失败，点击重试" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("conflict");
  });

  it("大文件另存由受限宿主入口处理", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider delayDuration={0}>
          <WorkspaceFileViewer
            path="guide.md"
            rootPath="/tmp/ally"
            scopeChatId={"a".repeat(32)}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "初版" });
    await user.click(screen.getByRole("button", { name: "下载文件" }));
    expect(scoped.saveAs).toHaveBeenCalledWith(
      "a".repeat(32),
      "guide.md",
      "/tmp/ally",
    );
    await user.click(
      screen.getByRole("button", { name: "使用系统默认应用打开" }),
    );
    expect(scoped.openExternal).toHaveBeenCalledWith(
      "a".repeat(32),
      "guide.md",
      "/tmp/ally",
    );
  });
});
