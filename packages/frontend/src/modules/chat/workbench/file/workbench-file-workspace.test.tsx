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
import * as platform from "@/shared/platform";
import {
  WorkbenchFileWorkspace,
  type WorkbenchFileWorkspaceProps,
} from "./workbench-file-workspace";

const selected = {
  path: "docs/plan.md",
  canonicalPath: "/work/project/docs/plan.md",
  rootPath: "/work/project",
  navigationRevision: 1,
  render: () => <textarea aria-label="文件正文" defaultValue="草稿" />,
};
const attachment = {
  name: "plan.md",
  path: "attachments/message/plan.md",
  source: "chat" as const,
};

function mount(props: Partial<WorkbenchFileWorkspaceProps> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchFileWorkspace
        rootPath="/space"
        scopePath=""
        chatFiles={[]}
        fileSource="project"
        onDownloadChatFile={async () => {}}
        onDeleteChatFile={async () => true}
        {...props}
      />
    </QueryClientProvider>,
  );
}

function projectTree() {
  return vi
    .spyOn(platform, "listWorkspaceDirectory")
    .mockImplementation(async (root, path) =>
      root !== "/work/project"
        ? []
        : !path
          ? [{ name: "docs", relativePath: "docs", isDirectory: true }]
          : path === "docs"
            ? [
                {
                  name: "plan.md",
                  relativePath: "docs/plan.md",
                  isDirectory: false,
                },
              ]
            : [],
    );
}

afterEach(() => vi.restoreAllMocks());

describe("WorkbenchFileWorkspace", () => {
  it("文件标签初次打开时左侧预览和路径留空，右侧目录可搜索和打开文件", async () => {
    const user = userEvent.setup();
    projectTree();
    const onSelectFile = vi.fn();
    mount({
      rootPath: "/work/project",
      onSelectFile,
    });
    expect(screen.getByText("打开文件", { exact: true })).toBeVisible();
    expect(screen.getByText("从右侧目录中选择文件")).toBeVisible();
    expect(screen.queryByRole("navigation", { name: "文件路径" })).toBeNull();
    expect(screen.getByRole("separator")).toBeVisible();
    expect(screen.getByText("打开文件").closest(".bg-surface")).not.toBeNull();
    expect(screen.getByRole("button", { name: "自动换行" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "隐藏文件目录" }));
    expect(
      screen.queryByRole("complementary", { name: "文件目录" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "显示文件目录" }));

    const tree = screen.getByRole("complementary", { name: "文件目录" });
    expect(
      within(tree).getByRole("searchbox", { name: "搜索文件" }),
    ).toBeVisible();
    await user.click(await within(tree).findByRole("button", { name: "docs" }));
    await user.click(
      await within(tree).findByRole("treeitem", { name: "plan.md" }),
    );
    expect(onSelectFile).toHaveBeenCalledWith(
      { name: "plan.md", path: "docs/plan.md" },
      "/work/project",
    );
  });

  it("文件标签跟随当前文件展开工作目录，隐藏树保留预览和草稿", async () => {
    const user = userEvent.setup();
    projectTree();
    mount({
      file: selected,
      sourceRoots: ["/work/project", "/work/other"],
    });
    const breadcrumb = screen.getByRole("navigation", { name: "文件路径" });
    expect(
      within(breadcrumb)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["project", "docs", "plan.md"]);
    expect(breadcrumb.querySelector('[aria-current="page"]')).toHaveTextContent(
      "plan.md",
    );
    expect(
      await screen.findByRole("treeitem", { name: "plan.md" }),
    ).toHaveAttribute("aria-current", "page");
    const preview = screen.getByRole("textbox", { name: "文件正文" });
    await user.type(preview, "未保存");
    expect(screen.getByRole("separator")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "隐藏文件目录" }));
    expect(
      screen.queryByRole("complementary", { name: "文件目录" }),
    ).toBeNull();
    expect(screen.getByRole("textbox", { name: "文件正文" })).toBe(preview);
    expect(preview).toHaveValue("草稿未保存");

    await user.click(screen.getByRole("button", { name: "显示文件目录" }));
    expect(screen.getByRole("treeitem", { name: "plan.md" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("面包屑以包含当前文件的最内层仓库为起点", async () => {
    projectTree();
    vi.spyOn(platform, "getScopedGitStatus").mockResolvedValue({
      isRepo: true,
      root: "/work/project",
      repositories: ["/work/project", "/work/project/subsystem"],
      entries: [],
    });
    mount({
      rootPath: "/work/project",
      file: {
        ...selected,
        path: "subsystem/src/plan.md",
        canonicalPath: "/work/project/subsystem/src/plan.md",
      },
    });
    const breadcrumb = screen.getByRole("navigation", { name: "文件路径" });
    await waitFor(() =>
      expect(
        within(breadcrumb)
          .getAllByRole("listitem")
          .map((item) => item.textContent),
      ).toEqual(["subsystem", "src", "plan.md"]),
    );
  });

  it("定位已打开的文件等待深层行出现，重复定位保持正文焦点", async () => {
    projectTree();
    const scroll = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    const renderWorkspace = (navigationRevision: number) => (
      <WorkbenchFileWorkspace
        rootPath="/work/project"
        scopePath=""
        file={{ ...selected, navigationRevision }}
        chatFiles={[]}
        fileSource="project"
        onDownloadChatFile={async () => {}}
        onDeleteChatFile={async () => true}
      />
    );
    const client = new QueryClient();
    const view = render(
      <QueryClientProvider client={client}>
        {renderWorkspace(1)}
      </QueryClientProvider>,
    );
    const editor = screen.getByRole("textbox", { name: "文件正文" });
    editor.focus();
    const row = await screen.findByRole("treeitem", { name: "plan.md" });
    await waitFor(() => expect(scroll.mock.contexts).toEqual([row]));
    expect(editor).toHaveFocus();

    view.rerender(
      <QueryClientProvider client={client}>
        {renderWorkspace(2)}
      </QueryClientProvider>,
    );
    await waitFor(() => expect(scroll.mock.contexts).toEqual([row, row]));
    expect(editor).toHaveFocus();
  });

  it("文件树保留搜索和单个目录开关，去除三项批量工具", async () => {
    const user = userEvent.setup();
    projectTree();
    mount({ rootPath: "/work/project", file: selected });
    const tree = screen.getByRole("complementary", { name: "文件目录" });
    expect(
      within(tree).getByRole("searchbox", { name: "搜索文件" }),
    ).toBeVisible();
    for (const name of ["刷新文件目录", "展开全部目录", "折叠全部目录"]) {
      expect(within(tree).queryByRole("button", { name })).toBeNull();
    }
    const toggle = screen.getByRole("button", { name: "隐藏文件目录" });
    expect(toggle.querySelector(".lucide-folder-tree")).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    const directory = await within(tree).findByRole("button", { name: "docs" });
    await user.click(directory);
    expect(directory).toHaveAttribute("aria-expanded", "false");
    await user.click(directory);
    expect(directory).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("textbox", { name: "文件正文" })).toHaveValue(
      "草稿",
    );
  });

  it("点击目录面包屑重新打开并定位树中的目录，保留当前文件", async () => {
    const user = userEvent.setup();
    projectTree();
    const scroll = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    const onSelectFile = vi.fn();
    mount({ rootPath: "/work/project", file: selected, onSelectFile });
    const tree = screen.getByRole("complementary", { name: "文件目录" });
    const directory = await within(tree).findByRole("button", { name: "docs" });
    await user.click(directory);
    expect(directory).toHaveAttribute("aria-expanded", "false");
    const breadcrumb = screen.getByRole("navigation", { name: "文件路径" });
    const crumb = within(breadcrumb).getByRole("button", { name: "docs" });

    await user.click(screen.getByRole("button", { name: "隐藏文件目录" }));
    expect(tree).not.toBeInTheDocument();
    await user.click(crumb);

    const reopenedTree = screen.getByRole("complementary", {
      name: "文件目录",
    });
    const revealed = await within(reopenedTree).findByRole("button", {
      name: "docs",
    });
    expect(revealed).toHaveAttribute("aria-expanded", "true");
    await waitFor(() => expect(scroll.mock.contexts).toContain(revealed));
    expect(crumb).toHaveFocus();
    expect(onSelectFile).not.toHaveBeenCalled();
    expect(
      await within(reopenedTree).findByRole("treeitem", { name: "plan.md" }),
    ).toHaveAttribute("aria-current", "page");
  });

  it.each([
    { fileSource: "chat" as const, rootPath: "/work/project" },
    { fileSource: "project" as const, rootPath: "/work/other" },
  ])("无法从 $fileSource 目录树定位时，面包屑仅显示路径", (source) => {
    projectTree();
    mount({ ...source, file: selected });
    const breadcrumb = screen.getByRole("navigation", { name: "文件路径" });
    expect(within(breadcrumb).getByText("docs")).toBeVisible();
    expect(within(breadcrumb).queryByRole("button")).toBeNull();
  });

  it("面包屑箭头按需浏览目录并用当前工作根打开文件", async () => {
    const user = userEvent.setup();
    vi.spyOn(platform, "hasPlatformService").mockReturnValue(true);
    vi.spyOn(platform, "openWorkspaceResourceScope").mockResolvedValue({
      id: "scope",
      root: "/work/project",
    });
    vi.spyOn(platform, "observeWorkspaceResource").mockResolvedValue(
      async () => {},
    );
    const list = vi
      .spyOn(platform, "listWorkspaceResourceDirectory")
      .mockImplementation(async (_chat, path) => {
        switch (path) {
          case "":
            return [{ name: "docs", relativePath: "docs", isDirectory: true }];
          case "docs":
            return [
              {
                name: "plan.md",
                relativePath: "docs/plan.md",
                isDirectory: false,
              },
              { name: "notes", relativePath: "docs/notes", isDirectory: true },
            ];
          case "docs/notes":
            return [
              {
                name: "next.md",
                relativePath: "docs/notes/next.md",
                isDirectory: false,
              },
            ];
          default:
            return [];
        }
      });
    const onSelectFile = vi.fn();
    mount({
      rootPath: "/work/project",
      scopeChatId: "chat-1",
      file: selected,
      onSelectFile,
    });
    await screen.findByRole("treeitem", { name: "plan.md" });
    expect(list).not.toHaveBeenCalledWith(
      "chat-1",
      "docs/notes",
      "/work/project",
    );

    const breadcrumb = screen.getByRole("navigation", { name: "文件路径" });
    await user.click(
      within(breadcrumb).getByRole("button", { name: "浏览 docs" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "plan.md" }),
    ).toHaveAttribute("aria-current", "page");
    await user.click(screen.getByRole("menuitem", { name: "notes" }));
    expect(
      await screen.findByRole("menuitem", { name: "next.md" }),
    ).toBeVisible();
    expect(list).toHaveBeenCalledWith("chat-1", "docs/notes", "/work/project");
    expect(onSelectFile).not.toHaveBeenCalled();
    await user.click(screen.getByRole("menuitem", { name: "返回 docs" }));
    await user.click(await screen.findByRole("menuitem", { name: "notes" }));
    await user.click(await screen.findByRole("menuitem", { name: "next.md" }));

    expect(onSelectFile).toHaveBeenCalledWith(
      { name: "next.md", path: "docs/notes/next.md" },
      "/work/project",
    );
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("草稿没有空间根时直接浏览已选工作目录", async () => {
    projectTree();
    mount({ rootPath: "/work/project", spaceRootAvailable: false });
    expect(await screen.findByRole("button", { name: "docs" })).toBeVisible();
    expect(screen.queryByRole("radio", { name: "空间目录" })).toBeNull();
  });

  it("拖入工作目录的文件按实际工作根导入", async () => {
    const user = userEvent.setup();
    vi.spyOn(platform, "hasPlatformService").mockReturnValue(true);
    vi.spyOn(platform, "openWorkspaceResourceScope").mockResolvedValue({
      id: "scope",
      root: "/space",
    });
    vi.spyOn(platform, "observeWorkspaceResource").mockResolvedValue(
      async () => {},
    );
    vi.spyOn(platform, "listWorkspaceResourceDirectory").mockResolvedValue([
      { name: "plan.md", relativePath: "plan.md", isDirectory: false },
    ]);
    const importFiles = vi
      .spyOn(platform, "importDroppedWorkspaceFiles")
      .mockResolvedValue({ imported: [], failed: [] });
    mount({ scopeChatId: "chat-1", sourceRoots: ["/work/project"] });
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    const file = new File(["content"], "example.txt");
    fireEvent.drop(await screen.findByRole("tree"), {
      dataTransfer: { types: ["Files"], files: [file] },
    });
    expect(importFiles).toHaveBeenCalledWith(
      "chat-1",
      "",
      [file],
      "/work/project",
    );
  });

  it("会话附件可按逻辑路径打开及下载，项目目录操作独立", async () => {
    const user = userEvent.setup();
    const onSelectFile = vi.fn();
    const onDownloadChatFile = vi.fn(async () => {});
    mount({
      fileSource: "chat",
      chatFiles: [attachment],
      onSelectFile,
      onDownloadChatFile,
    });
    const list = screen.getByRole("complementary", { name: "会话文件" });
    await user.click(within(list).getByRole("treeitem", { name: "plan.md" }));
    expect(onSelectFile).toHaveBeenCalledWith(attachment);
    await user.click(
      within(list).getByRole("button", { name: "plan.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "下载文件" }));
    expect(onDownloadChatFile).toHaveBeenCalledWith(attachment);
    expect(screen.queryByRole("button", { name: "刷新文件目录" })).toBeNull();
  });

  it("会话附件有未保存的修改时阻止删除", async () => {
    const user = userEvent.setup();
    vi.spyOn(platform, "resolveWorkspaceResourceReference").mockResolvedValue({
      status: "resolved",
      canonicalPath: "/private/attachments/message/plan.md",
      baseDirectory: "",
      workspaceRoot: "/private/attachments",
      sourceRoots: [],
    });
    const onDeleteChatFile = vi.fn(async () => true);
    mount({
      fileSource: "chat",
      scopeChatId: "chat-1",
      chatFiles: [attachment],
      onDeleteChatFile,
      isPathDirty: (_, reference) =>
        reference?.canonicalPath === "/private/attachments/message/plan.md",
    });
    await user.click(
      screen.getByRole("button", { name: "plan.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除文件" }));
    await user.click(screen.getByRole("button", { name: "删除文件" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "请先保存或放弃此文件的修改。",
    );
    expect(onDeleteChatFile).not.toHaveBeenCalled();
  });
});
