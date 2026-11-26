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
  it("浏览器显示项目目录并将选中的文件交给工作台打开", async () => {
    const user = userEvent.setup();
    projectTree();
    const onSelectFile = vi.fn();
    mount({
      rootPath: "/work/project",
      onSelectFile,
    });
    await user.click(await screen.findByRole("button", { name: "docs" }));
    await user.click(await screen.findByRole("treeitem", { name: "plan.md" }));
    expect(onSelectFile).toHaveBeenCalledWith(
      { name: "plan.md", path: "docs/plan.md" },
      "/work/project",
    );
    expect(screen.queryByRole("separator")).toBeNull();
  });

  it("文件标签跟随当前文件展开工作目录，隐藏树保留预览和草稿", async () => {
    const user = userEvent.setup();
    projectTree();
    mount({
      file: selected,
      sourceRoots: ["/work/project", "/work/other"],
    });
    expect(
      screen.getByRole("navigation", { name: "文件路径" }),
    ).toHaveTextContent("docs / plan.md");
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

  it("展开和折叠目录不会替换当前文件，刷新重新读取目录", async () => {
    const user = userEvent.setup();
    const list = projectTree();
    mount({ rootPath: "/work/project", file: selected });
    expect(
      await screen.findByRole("treeitem", { name: "plan.md" }),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "折叠全部目录" }));
    expect(screen.queryByRole("treeitem", { name: "plan.md" })).toBeNull();
    expect(screen.getByRole("textbox", { name: "文件正文" })).toHaveValue(
      "草稿",
    );

    await user.click(screen.getByRole("button", { name: "展开全部目录" }));
    expect(
      await screen.findByRole("treeitem", { name: "plan.md" }),
    ).toBeVisible();

    list.mockClear();
    await user.click(screen.getByRole("button", { name: "刷新文件目录" }));
    await waitFor(() => expect(list).toHaveBeenCalledWith("/work/project", ""));
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
