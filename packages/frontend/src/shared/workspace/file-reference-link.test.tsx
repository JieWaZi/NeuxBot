import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FileReferenceResolution } from "@neuxbot/platform-contracts/resources";
import {
  emptyFileReferenceContext,
  localFileURL,
  type FileReferenceCandidate,
  type FileReferenceContext,
  type ResolvedFileReference,
} from "./file-reference";
import { FileReferenceProvider, ReferenceLink } from "./file-reference-link";

const { openExternalURL, resolveFile, resolveResource, showError } = vi.hoisted(
  () => ({
    openExternalURL: vi.fn(async () => undefined),
    resolveFile: vi.fn<() => Promise<FileReferenceResolution>>(),
    resolveResource: vi.fn<() => Promise<FileReferenceResolution>>(),
    showError: vi.fn(),
  }),
);
vi.mock("@/shared/platform", () => ({
  openExternalURL,
  resolveFileReferencePath: resolveFile,
  resolveWorkspaceResourceReference: resolveResource,
}));
vi.mock("@/shared/ui/sonner", () => ({ toast: { error: showError } }));

const clients: QueryClient[] = [];
function renderReference(
  children: ReactNode,
  options: {
    context?: FileReferenceContext;
    onOpenFile?: (file: ResolvedFileReference) => void;
    onOpenURL?: (url: string) => unknown;
  } = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <FileReferenceProvider
        context={options.context ?? emptyFileReferenceContext}
        onOpenFile={options.onOpenFile}
        onOpenURL={options.onOpenURL}
      >
        {children}
      </FileReferenceProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => {
  for (const client of clients.splice(0)) client.clear();
});

it.each([
  ["/repo/report.md", "blue", "markdown"],
  ["/repo/note.txt", "blue", "document"],
  ["/repo/index.js", "amber", "javascript"],
  ["/repo/index.ts", "blue", "typescript"],
  ["/repo/view.jsx", "cyan", "react"],
  ["/repo/view.tsx", "blue", "react_ts"],
  ["/repo/app.vue", "green", "vue"],
  ["/repo/index.html", "orange", "html"],
  ["/repo/app.css", "purple", "css"],
  ["/repo/main.go", "cyan", "go"],
  ["/repo/main.py", "blue", "python"],
  ["/repo/Main.java", "red", "java"],
  ["/repo/main.rb", "red", "ruby"],
  ["/repo/run.sh", "orange", "console"],
  ["/repo/data.json", "amber", "json"],
  ["/repo/config.yaml", "red", "yaml"],
  ["/repo/config.toml", "red", "toml"],
  ["/repo/.env.local", "amber", "tune"],
  ["/repo/.gitignore", "orange", "git"],
  ["/repo/Dockerfile", "blue", "docker"],
  ["/repo/package.json", "green", "nodejs"],
  ["/repo/photo.png", "teal", "image"],
  ["/repo/report.pdf", "red", "pdf"],
  ["/repo/report.docx", "blue", "word"],
  ["/repo/data.xlsx", "green", "table"],
  ["/repo/deck.pptx", "orange", "powerpoint"],
  ["/repo/data.zip", "lime", "zip"],
  ["/repo/main.rs", "orange", "rust"],
  ["/repo/logo.svg", "amber", "svg"],
  ["/repo/output.log", "lime", "log"],
  ["/repo/tsconfig.app.json", "blue", "tsconfig"],
  ["/repo/unknown.xyz", "neutral", "file"],
])(
  "文件引用 %s 的文字颜色与类型图标一致且保留可访问名称",
  (path, tone, icon) => {
    renderReference(
      <ReferenceLink reference={{ kind: "file", path, raw: path }}>
        查看文件
      </ReferenceLink>,
      { onOpenFile: vi.fn() },
    );
    const link = screen.getByRole("link", { name: "查看文件" });
    expect(link).toHaveAttribute("data-file-tone", tone);
    expect(link.querySelector("img")?.getAttribute("src")).toContain(
      `/material-icon-theme/${icon}.svg`,
    );
    expect(resolveFile).not.toHaveBeenCalled();
  },
);

describe("网站图标", () => {
  it.each(["light", "dark"])(
    "%s 下成功加载同源 favicon，失败恢复链条且不改变链接名称",
    (theme) => {
      const view = renderReference(
        <div data-theme={theme}>
          <ReferenceLink
            reference={{
              kind: "web",
              url: "https://example.com/news?q=secret#item",
            }}
          >
            新闻来源
          </ReferenceLink>
        </div>,
      );
      const link = screen.getByRole("link", { name: "新闻来源" });
      const image = link.querySelector("img");
      if (!image) throw new Error("缺少网站图标");
      expect(image).toHaveAttribute("src", "https://example.com/favicon.ico");
      expect(image).toHaveAttribute("referrerpolicy", "no-referrer");
      expect(link.querySelector("svg")).not.toBeNull();
      fireEvent.load(image);
      expect(link.querySelector("svg")).toBeNull();
      fireEvent.error(image);
      expect(link.querySelector("img")).toBeNull();
      expect(link.querySelector("svg")).not.toBeNull();
      view.unmount();
    },
  );
  it("切换目标网站后重新加载图标，不沿用上个网站的失败状态", () => {
    const view = render(
      <ReferenceLink
        reference={{ kind: "web", url: "https://old.example/news" }}
      >
        来源
      </ReferenceLink>,
    );
    const image = view.container.querySelector("img");
    if (!image) throw new Error("缺少网站图标");
    fireEvent.error(image);
    view.rerender(
      <ReferenceLink
        reference={{ kind: "web", url: "https://new.example/news" }}
      >
        来源
      </ReferenceLink>,
    );
    expect(view.container.querySelector("img")).toHaveAttribute(
      "src",
      "https://new.example/favicon.ico",
    );
  });
});

describe("reference link activation", () => {
  it("delegates ordinary HTTP(S) clicks and preserves native modified and auxiliary gestures", () => {
    const open = vi.fn();
    const url = "https://example.com/report?q=完整目标#section";
    renderReference(
      <ReferenceLink reference={{ kind: "web", url }} title="作者说明">
        <strong>报告标签</strong>
      </ReferenceLink>,
      { onOpenURL: open },
    );
    const link = screen.getByRole("link", { name: "报告标签" });
    expect(link).toHaveAttribute("href", url);
    expect(link).toHaveAttribute("title", "作者说明");
    expect(fireEvent.click(link)).toBe(false);
    expect(open).toHaveBeenCalledExactlyOnceWith(url);
    for (const gesture of [
      { metaKey: true },
      { ctrlKey: true },
      { shiftKey: true },
      { altKey: true },
      { button: 1 },
      { button: 2 },
    ])
      expect(fireEvent.click(link, gesture)).toBe(true);
    expect(
      fireEvent(
        link,
        new MouseEvent("auxclick", {
          button: 1,
          bubbles: true,
          cancelable: true,
        }),
      ),
    ).toBe(true);
    expect(fireEvent.contextMenu(link)).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
    expect(openExternalURL).not.toHaveBeenCalled();
    expect(resolveFile).not.toHaveBeenCalled();
    expect(resolveResource).not.toHaveBeenCalled();
  });

  it("uses the platform delegate when no owner callback exists and respects prevented activation", () => {
    const url = "http://example.com/plain";
    renderReference(
      <>
        <ReferenceLink reference={{ kind: "web", url }} title="">
          普通网页
        </ReferenceLink>
        <div onClickCapture={(event) => event.preventDefault()}>
          <ReferenceLink reference={{ kind: "web", url }}>
            已处理网页
          </ReferenceLink>
        </div>
      </>,
    );
    expect(screen.getByRole("link", { name: "普通网页" })).toHaveAttribute(
      "title",
      "",
    );
    expect(
      fireEvent.click(screen.getByRole("link", { name: "普通网页" })),
    ).toBe(false);
    expect(openExternalURL).toHaveBeenCalledExactlyOnceWith(url);
    expect(
      fireEvent.click(screen.getByRole("link", { name: "已处理网页" })),
    ).toBe(false);
    expect(openExternalURL).toHaveBeenCalledTimes(1);
  });

  it("resolves local targets only on activation and preserves the author label, title and full path", async () => {
    const user = userEvent.setup();
    const clipboard = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    const path = `/authorized/${"完整目录/".repeat(30)}report with spaces.md`;
    resolveResource.mockResolvedValueOnce({
      status: "resolved",
      canonicalPath: path,
      baseDirectory: "/workspace",
      workspaceRoot: "/workspace",
      sourceRoots: ["/authorized"],
    });
    const open = vi.fn();
    const reference: FileReferenceCandidate = {
      kind: "file",
      path: "projects/report with spaces.md",
      raw: "projects/report with spaces.md#L17-L19",
      line: 17,
      endLine: 19,
    };
    renderReference(
      <ReferenceLink reference={reference} title="作者的文件说明">
        <strong>保留标签</strong>
      </ReferenceLink>,
      {
        context: {
          chatId: "chat-1",
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: ["/authorized"],
        },
        onOpenFile: open,
      },
    );
    const link = screen.getByRole("link", { name: "保留标签" });
    expect(resolveResource).not.toHaveBeenCalled();
    await user.click(link);
    await waitFor(() =>
      expect(open).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ canonicalPath: path, line: 17, endLine: 19 }),
      ),
    );
    expect(resolveResource).toHaveBeenCalledExactlyOnceWith(
      "chat-1",
      reference.path,
      "/workspace",
    );
    expect(link).toHaveAttribute("href", localFileURL(path));
    expect(link).toHaveAttribute("data-file-path", path);
    expect(link).toHaveAttribute("title", "作者的文件说明");
    expect(link.textContent).toBe("保留标签");
    expect(link.querySelector("strong")).toHaveTextContent("保留标签");
    await user.click(screen.getByRole("button", { name: /复制完整路径/ }));
    expect(clipboard).toHaveBeenCalledExactlyOnceWith(path);
    expect(resolveFile).not.toHaveBeenCalled();
    clipboard.mockRestore();
  });

  it("blocks native local file gestures without bypassing authorization or resolving eagerly", () => {
    const open = vi.fn();
    renderReference(
      <ReferenceLink
        reference={{
          kind: "file",
          path: "/private/file.txt",
          raw: "/private/file.txt",
        }}
      >
        文件
      </ReferenceLink>,
      {
        context: {
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: [],
        },
        onOpenFile: open,
      },
    );
    const link = screen.getByRole("link", { name: "文件" });
    for (const gesture of [
      { metaKey: true },
      { ctrlKey: true },
      { shiftKey: true },
      { altKey: true },
    ])
      expect(fireEvent.click(link, gesture)).toBe(false);
    for (const button of [1, 2])
      expect(
        fireEvent(
          link,
          new MouseEvent("auxclick", {
            button,
            bubbles: true,
            cancelable: true,
          }),
        ),
      ).toBe(false);
    expect(fireEvent.contextMenu(link)).toBe(true);
    expect(resolveFile).not.toHaveBeenCalled();
    expect(resolveResource).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    expect(openExternalURL).not.toHaveBeenCalled();
  });

  it("keeps denied local references readable without opening a system URL", async () => {
    resolveFile.mockResolvedValueOnce({ status: "outside_scope" });
    const open = vi.fn();
    renderReference(
      <ReferenceLink
        reference={{
          kind: "file",
          path: "/outside/file",
          raw: "/outside/file",
        }}
      >
        原始标签
      </ReferenceLink>,
      {
        context: {
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: [],
        },
        onOpenFile: open,
      },
    );
    expect(
      fireEvent.click(screen.getByRole("link", { name: "原始标签" })),
    ).toBe(false);
    await waitFor(() => expect(showError).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("link", { name: "原始标签" })).toHaveTextContent(
      "原始标签",
    );
    expect(open).not.toHaveBeenCalled();
    expect(openExternalURL).not.toHaveBeenCalled();
  });
});
