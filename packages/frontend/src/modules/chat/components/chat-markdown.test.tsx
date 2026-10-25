import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { emptyFileReferenceContext } from "@/shared/workspace/file-reference";
import { resolveFileReferencePath } from "@/shared/platform";
import { TextMessagePartProvider } from "@assistant-ui/react";
import { mermaid } from "@streamdown/mermaid";
import {
  act,
  render as renderUI,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import themeStyles from "@/shared/styles/index.css?raw";

import { AssistantText, UserText } from "./chat-message-content";
import { ChatMermaid } from "./chat-mermaid";

const { saveBlob, themeState } = vi.hoisted(() => ({
  saveBlob: vi.fn<(blob: Blob, fileName: string) => Promise<boolean>>(
    async () => true,
  ),
  themeState: { resolvedTheme: "light" as "dark" | "light" },
}));

vi.mock("@/shared/platform", async () => {
  const { fakeFileReference } = await import("@/test/file-reference-fake");
  return {
    saveBlob,
    resolveFileReferencePath: vi.fn((request) =>
      Promise.resolve(
        fakeFileReference(request, {
          "/workspace/projects/share": "/Users/example/Desktop/share",
          "/workspace/leader/projects/share": "/Users/example/Desktop/share",
        }),
      ),
    ),
  };
});

function render(ui: Parameters<typeof renderUI>[0]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    client,
    ...renderUI(ui, {
      wrapper: ({ children }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    }),
  };
}
vi.mock("@/shared/theme", () => ({ useTheme: () => themeState }));

describe("chat markdown", () => {
  beforeEach(() => {
    themeState.resolvedTheme = "light";
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        disconnect() {}
        observe() {}
        takeRecords() {
          return [];
        }
        unobserve() {}
      },
    );
    Object.defineProperty(SVGElement.prototype, "getBBox", {
      configurable: true,
      value: () => ({ height: 1, width: 1, x: 0, y: 0 }),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    saveBlob.mockClear();
    delete document.documentElement.dataset.theme;
    delete (SVGElement.prototype as SVGElement & { getBBox?: unknown }).getBBox;
  });

  it("renders Mermaid fences as diagrams through Streamdown", async () => {
    const { container } = render(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-streamdown="mermaid-block"]'),
      ).not.toBeNull(),
    );
  });

  it("暗色主题下使用 Mermaid 暗色配色", async () => {
    document.documentElement.dataset.theme = "dark";
    themeState.resolvedTheme = "dark";
    const getMermaid = vi.spyOn(mermaid, "getMermaid").mockReturnValue({
      initialize: vi.fn(),
      render: vi.fn(async () => ({ svg: "<svg></svg>" })),
    });

    render(<ChatMermaid code={"flowchart TD\n  A --> B"} />);

    await waitFor(() => expect(getMermaid).toHaveBeenCalled());
    expect(getMermaid).toHaveBeenLastCalledWith({ theme: "dark" });
  });

  it("waits for the assistant message to finish before rendering Mermaid", async () => {
    const { container, rerender } = render(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning
      >
        <AssistantText isTerminal={false} />
      </TextMessagePartProvider>,
    );

    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 300));
    });
    expect(
      container.querySelector('[data-streamdown="mermaid-block"]'),
    ).toBeNull();

    rerender(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-streamdown="mermaid-block"]'),
      ).not.toBeNull(),
    );
  });

  it("does not restart a pending Mermaid render when the parent rerenders", async () => {
    let finishRender: ((result: { svg: string }) => void) | undefined;
    const renderMermaid = vi.fn(
      () =>
        new Promise<{ svg: string }>((resolve) => {
          finishRender = resolve;
        }),
    );
    vi.spyOn(mermaid, "getMermaid").mockReturnValue({
      initialize: vi.fn(),
      render: renderMermaid,
    });

    const { rerender } = render(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() => expect(renderMermaid).toHaveBeenCalledTimes(1));

    rerender(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await act(async () => {
      await Promise.resolve();
    });
    expect(renderMermaid).toHaveBeenCalledTimes(1);

    finishRender?.({ svg: "<svg></svg>" });
    expect(
      await screen.findByRole("img", { name: "Mermaid diagram" }),
    ).toBeInTheDocument();
  });

  it("keeps the previous diagram visible while a new Mermaid snapshot renders", async () => {
    const renderMermaid = vi.fn(async (_id: string, source: string) => ({
      svg: `<svg data-source="${source}"></svg>`,
    }));
    vi.spyOn(mermaid, "getMermaid").mockReturnValue({
      initialize: vi.fn(),
      render: renderMermaid,
    });

    const firstCode = "flowchart TD\n  A --> B";
    const nextCode = "flowchart TD\n  A --> C";
    const { rerender } = render(<ChatMermaid code={firstCode} />);
    const diagram = await screen.findByRole("img", {
      name: "Mermaid diagram",
    });
    expect(diagram.querySelector("svg")).toHaveAttribute(
      "data-source",
      firstCode,
    );

    rerender(<ChatMermaid code={nextCode} />);

    expect(
      screen.getByRole("img", { name: "Mermaid diagram" }).querySelector("svg"),
    ).toHaveAttribute("data-source", firstCode);
  });

  it("暗色主题下使用可读的代码高亮前景色", async () => {
    document.documentElement.dataset.theme = "dark";
    const { container } = render(
      <TextMessagePartProvider
        text={"```html\nEnable JavaScript and cookies to continue\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() => {
      expect(
        container.querySelector('[data-streamdown="code-block"]'),
      ).not.toBeNull();
      expect(
        container.querySelector('[data-streamdown="code-block-body"]'),
      ).not.toBeNull();
    });

    const token = await waitFor(() => {
      const element = container.querySelector<HTMLElement>(
        '[data-streamdown="code-block-body"] span[style*="--shiki-dark"]',
      );
      if (!element) throw new Error("代码高亮未生成暗色主题 Token");
      return element;
    });

    const darkTextClass = [...token.classList].find((className) =>
      className.startsWith("dark:text-"),
    );
    if (!darkTextClass) throw new Error("代码高亮未生成暗色前景工具类");

    const darkVariant = themeStyles.match(/@custom-variant dark .+;/)?.[0];
    if (!darkVariant) throw new Error("全局样式未声明暗色主题变体");
    const { compile } = await import("tailwindcss");
    const compiler = await compile(`${darkVariant}\n@tailwind utilities;`);
    const compiledCSS = compiler.build([darkTextClass]);

    expect(compiledCSS).toContain(
      ':where([data-theme="dark"], [data-theme="dark"] *)',
    );
    expect(compiledCSS).toContain("color: var(--shiki-dark");
  });

  it("将 htm 代码围栏按 HTML 高亮且不输出 Shiki 错误", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const { container } = render(
      <TextMessagePartProvider
        text={"```htm\n<p>hello</p>\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-streamdown="code-block"]'),
      ).not.toBeNull(),
    );
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 50));
    });

    expect(consoleError.mock.calls.flat().join(" ")).not.toContain(
      "Language `htm` is not included in this bundle",
    );
    consoleError.mockRestore();
  });

  it("为普通代码围栏提供复制入口", async () => {
    const user = userEvent.setup();
    const writeSpy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    render(
      <TextMessagePartProvider
        text={"```text\ncurl: (6) Could not resolve host: chatgpt.com\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    const copyButton = await screen.findByRole("button", { name: "复制代码" });
    const markdownContent = copyButton.closest(".chat-markdown");

    expect(copyButton).toHaveClass("size-control-sm", "p-0");
    expect(markdownContent).toHaveClass(
      "[&_[data-streamdown=code-block-actions]]:border-0",
      "[&_[data-streamdown=code-block-actions]]:bg-transparent",
      "[&_[data-streamdown=code-block-actions]]:p-0",
    );

    await user.click(copyButton);

    expect(writeSpy).toHaveBeenCalledWith(
      "curl: (6) Could not resolve host: chatgpt.com\n",
    );
  });

  it("switches a Mermaid block between diagram and source", async () => {
    const user = userEvent.setup();
    render(
      <TextMessagePartProvider
        text={"```mermaid\nflowchart TD\n  A --> B\n```"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await user.click(await screen.findByRole("radio", { name: "源码" }));

    expect(screen.getByText(/flowchart TD/)).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "源码" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("网页链接不弹确认框并直接交给 NeuxBot 浏览器处理", async () => {
    const user = userEvent.setup();
    const onOpenLink = vi.fn(() => true);
    render(
      <FileReferenceProvider
        context={emptyFileReferenceContext}
        onOpenURL={onOpenLink}
      >
        <TextMessagePartProvider
          text={"[SSO document](https://example.com/sso.md)"}
          isRunning={false}
        >
          <AssistantText />
        </TextMessagePartProvider>
      </FileReferenceProvider>,
    );

    await user.click(screen.getByRole("link", { name: "SSO document" }));

    expect(onOpenLink).toHaveBeenCalledWith("https://example.com/sso.md");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it.each([
    "`/workspace/projects/share/a.txt`",
    "`/Users/example/Desktop/share/a.txt`",
    "`~/Desktop/share/a.txt`",
    "`./projects/share/a.txt`",
    "[a.txt](file:///Users/example/Desktop/share/a.txt)",
    "[a.txt](/workspace/projects/share/a.txt)",
    "[a.txt](./projects/share/a.txt)",
  ])("文件引用的所有语法共用真实路径、显示、复制与打开：%s", async (text) => {
    const user = userEvent.setup();
    const clipboard = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    const open = vi.fn();
    render(
      <FileReferenceProvider
        context={{
          baseDirectory: "/workspace/leader",
          workspaceRoot: "/workspace/leader",
          sourceRoots: ["/Users/example/Desktop/share"],
        }}
        onOpenFile={open}
      >
        <TextMessagePartProvider text={text} isRunning={false}>
          <AssistantText />
        </TextMessagePartProvider>
      </FileReferenceProvider>,
    );
    const file = await screen.findByRole("link", { name: "a.txt" });
    await waitFor(() =>
      expect(file).toHaveAttribute(
        "data-file-path",
        "/Users/example/Desktop/share/a.txt",
      ),
    );
    expect(file).toHaveAttribute("title", "/Users/example/Desktop/share/a.txt");
    await user.click(
      screen.getByRole("button", { name: "复制完整路径：a.txt" }),
    );
    expect(clipboard).toHaveBeenCalledWith(
      "/Users/example/Desktop/share/a.txt",
    );
    expect(open).not.toHaveBeenCalled();
    await user.click(file);
    expect(open).toHaveBeenCalledWith(
      expect.objectContaining({
        canonicalPath: "/Users/example/Desktop/share/a.txt",
        displayPath: "a.txt",
      }),
    );
  });

  it.each([
    "projects/share/",
    "projects/share",
    "projects/share/folder.with.dots",
  ])("真实目录按目录展示并复制软链接目标：%s", async (path) => {
    const user = userEvent.setup();
    const clipboard = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    const canonicalPath = path.endsWith("folder.with.dots")
      ? "/Users/example/Desktop/share/folder.with.dots"
      : "/Users/example/Desktop/share";
    vi.mocked(resolveFileReferencePath).mockResolvedValueOnce({
      status: "directory",
      canonicalPath,
      baseDirectory: "/workspace/leader",
      workspaceRoot: "/workspace/leader",
      sourceRoots: ["/Users/example/Desktop/share"],
    });
    const open = vi.fn();
    render(
      <FileReferenceProvider
        context={{
          baseDirectory: "/workspace/leader",
          workspaceRoot: "/workspace/leader",
          sourceRoots: ["/Users/example/Desktop/share"],
        }}
        onOpenFile={open}
      >
        <TextMessagePartProvider
          text={`在默认项目目录 \`${path}\``}
          isRunning={false}
        >
          <AssistantText />
        </TextMessagePartProvider>
      </FileReferenceProvider>,
    );
    const directory = await screen.findByTitle(canonicalPath);
    expect(directory).toHaveAttribute("data-chat-link", "directory");
    expect(directory).toHaveTextContent(
      path.endsWith("folder.with.dots") ? "folder.with.dots/" : "share/",
    );
    expect(directory.querySelector(".lucide-folder")).not.toBeNull();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.queryByText("文件不存在")).not.toBeInTheDocument();
    await user.click(directory);
    expect(open).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: /复制完整路径/ }));
    expect(clipboard).toHaveBeenCalledWith(canonicalPath);
  });

  it.each([
    "missing",
    "outside_scope",
    "ambiguous",
    "unavailable",
    "error",
  ] as const)(
    "无法定位的引用保留原文且不显示失败文件卡片：%s",
    async (status) => {
      if (status === "error")
        vi.mocked(resolveFileReferencePath).mockRejectedValueOnce(
          new Error("bridge unavailable"),
        );
      else
        vi.mocked(resolveFileReferencePath).mockResolvedValueOnce({ status });
      const ui = (
        <FileReferenceProvider
          context={{
            baseDirectory: "/workspace",
            workspaceRoot: "/workspace",
            sourceRoots: [],
          }}
        >
          <TextMessagePartProvider
            text={
              "`./missing.custom`，[查看目录](./missing.custom)，`a`，`python3 -m http.server 8000`"
            }
            isRunning={false}
          >
            <AssistantText />
          </TextMessagePartProvider>
        </FileReferenceProvider>
      );
      const { client, rerender } = render(ui);
      // 等查询完成后再重渲染，确保断言覆盖真实失败结果，而非仅覆盖加载时原文。
      await waitFor(() =>
        expect(
          client
            .getQueryCache()
            .getAll()
            .every((query) => query.state.status !== "pending"),
        ).toBe(true),
      );
      rerender(ui);
      expect(screen.queryByRole("link")).not.toBeInTheDocument();
      expect(screen.getByText("./missing.custom").tagName).toBe("CODE");
      expect(screen.getByText("查看目录")).toBeInTheDocument();
      expect(screen.queryByText("文件不存在")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /复制完整路径/ }),
      ).not.toBeInTheDocument();
      expect(screen.getByText("a").tagName).toBe("CODE");
      expect(screen.getByText("python3 -m http.server 8000").tagName).toBe(
        "CODE",
      );
    },
  );

  it("链接内的代码标题不嵌套链接，围栏代码内的 URL 不变成交互入口", () => {
    const { container } = render(
      <FileReferenceProvider
        context={emptyFileReferenceContext}
        onOpenURL={() => true}
      >
        <TextMessagePartProvider
          text={
            "[`http://localhost:8000`](http://localhost:8000)\n\n```text\nhttp://localhost:8000\n```"
          }
          isRunning={false}
        >
          <AssistantText />
        </TextMessagePartProvider>
      </FileReferenceProvider>,
    );
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(container.querySelector("a a")).toBeNull();
    expect(container.querySelector("pre a")).toBeNull();
  });

  it("uses a semantic horizontal rule only when Markdown requests one", async () => {
    const { container } = render(
      <TextMessagePartProvider
        text={"第一段\n\n---\n\n第二段"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() => {
      const markdown = container.querySelector(
        '[data-chat-markdown="assistant"]',
      );
      expect(markdown).not.toBeNull();
      expect(markdown?.querySelectorAll("hr")).toHaveLength(1);
    });
  });

  it("renders user message Markdown with the same typography pipeline", async () => {
    const { container } = render(
      <TextMessagePartProvider text={"**重点** 与 `file.ts`"} isRunning={false}>
        <UserText />
      </TextMessagePartProvider>,
    );

    await waitFor(() => {
      expect(
        container.querySelector('[data-streamdown="strong"]'),
      ).not.toBeNull();
      expect(container.querySelector("code")).not.toBeNull();
    });
  });

  it("downloads and copies Mermaid source through the desktop export helpers", async () => {
    const user = userEvent.setup();
    const writeSpy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    render(<ChatMermaid code={"flowchart TD\n  A --> B"} />);

    await user.click(screen.getByRole("button", { name: "下载 Mermaid 源码" }));
    expect(saveBlob).toHaveBeenCalledWith(expect.any(Blob), "diagram.mmd");
    const blob = saveBlob.mock.calls[0]?.[0];
    await expect(blob?.text()).resolves.toBe("flowchart TD\n  A --> B");

    await user.click(screen.getByRole("button", { name: "复制 Mermaid 源码" }));
    expect(writeSpy).toHaveBeenCalledWith("flowchart TD\n  A --> B");
  });

  it("opens Mermaid fullscreen in the shared preview dialog", async () => {
    const user = userEvent.setup();
    const getMermaid = vi.spyOn(mermaid, "getMermaid").mockReturnValue({
      initialize: vi.fn(),
      render: vi.fn(async () => ({ svg: "<svg></svg>" })),
    });
    render(<ChatMermaid code={"flowchart TD\n  A --> B"} />);

    await user.click(await screen.findByRole("button", { name: "全屏查看" }));

    const dialog = screen.getByRole("dialog", { name: "图形" });
    expect(dialog).toBeInTheDocument();
    expect(
      within(dialog).getByRole("img", { name: "Mermaid diagram" }),
    ).toBeInTheDocument();
    getMermaid.mockRestore();
  });

  it("copies, downloads and previews Markdown tables in the shared dialog", async () => {
    const user = userEvent.setup();
    const writeSpy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    const { container } = render(
      <TextMessagePartProvider
        text={"| 步骤 | 动作 |\n| --- | --- |\n| 1 | 访问应用 |"}
        isRunning={false}
      >
        <AssistantText />
      </TextMessagePartProvider>,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-streamdown="table-wrapper"]'),
      ).not.toBeNull(),
    );

    await user.click(screen.getByRole("button", { name: "复制表格" }));
    await user.click(screen.getByRole("menuitem", { name: "Markdown" }));
    expect(writeSpy).toHaveBeenCalledWith(
      expect.stringContaining("| 步骤 | 动作 |"),
    );

    await user.click(screen.getByRole("button", { name: "下载表格" }));
    await user.click(screen.getByRole("menuitem", { name: "CSV" }));
    expect(saveBlob).toHaveBeenCalledWith(expect.any(Blob), "table.csv");

    await user.click(screen.getByRole("button", { name: "全屏查看" }));
    expect(screen.getByRole("dialog", { name: "表格" })).toBeInTheDocument();
  });
});
