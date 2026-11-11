import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { WorkspaceFile } from "@/shared/workspace/types";
import type { WorkspaceFileView } from "@/shared/workspace/workspace-tab";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type * as FileReferenceModule from "@/shared/workspace/file-reference";
import { workbenchDocumentBuffers } from "../document-buffers";
import {
  discardFileWorkspaceDrafts,
  hasDirtyFileWorkspaceDrafts,
  WorkbenchFileWorkspace,
} from "./workbench-file-workspace";

const {
  resolveFileReference,
  hasPlatformService,
  importDroppedWorkspaceFiles,
} = vi.hoisted(() => ({
  hasPlatformService: vi.fn(() => false),
  importDroppedWorkspaceFiles: vi.fn(async () => ({
    imported: [],
    failed: [],
  })),
  resolveFileReference: vi.fn(
    async (candidate: { path: string }, context: { workspaceRoot: string }) => {
      const name = candidate.path.split("/").at(-1) ?? candidate.path;
      return {
        status: "resolved" as const,
        file: {
          name,
          path: context.workspaceRoot
            ? candidate.path.replace(`${context.workspaceRoot}/`, "")
            : candidate.path,
          canonicalPath: candidate.path.startsWith("/")
            ? candidate.path
            : `${context.workspaceRoot}/${candidate.path}`,
          rootPath: context.workspaceRoot,
          displayPath: name,
        },
      };
    },
  ),
}));

vi.mock("@/shared/workspace/file-reference", async (importOriginal) => ({
  ...(await importOriginal<typeof FileReferenceModule>()),
  resolveFileReference,
}));

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService,
  importDroppedWorkspaceFiles,
}));

vi.mock("../../components/workspace-file-browser", () => ({
  WorkspaceFileBrowser: ({
    activePath,
    onOpenFile,
    query,
    rootPath,
    onPathDeleted,
    onPathRenamed,
    isPathDirty,
  }: {
    activePath?: string;
    onOpenFile: (file: { name: string; path: string }) => void;
    query: string;
    rootPath: string;
    onPathDeleted: (
      path: string,
      file: undefined,
      directory: boolean,
      root: string,
    ) => void;
    onPathRenamed: (
      oldPath: string,
      newPath: string,
      name: string,
      file: undefined,
      directory: boolean,
      root: string,
    ) => void;
    isPathDirty: (path: string, file: undefined, root: string) => boolean;
  }) => (
    <div
      data-testid="project-tree"
      data-active-path={activePath}
      data-query={query}
      data-root={rootPath}
      data-dirty={isPathDirty("projects/one.md", undefined, rootPath)}
      data-directory-dirty={isPathDirty("nested", undefined, rootPath)}
    >
      <button
        type="button"
        onClick={() => onOpenFile({ name: "one.md", path: "projects/one.md" })}
      >
        one.md
      </button>
      <button
        type="button"
        onClick={() => onOpenFile({ name: "two.md", path: "projects/two.md" })}
      >
        two.md
      </button>
      <button
        type="button"
        onClick={() =>
          onPathDeleted("projects/one.md", undefined, false, rootPath)
        }
      >
        删除当前根文件
      </button>
      <button
        type="button"
        onClick={() =>
          onPathRenamed(
            "projects/one.md",
            "projects/renamed.md",
            "renamed.md",
            undefined,
            false,
            rootPath,
          )
        }
      >
        重命名当前根文件
      </button>
      <button
        type="button"
        onClick={() => onPathDeleted("nested", undefined, true, rootPath)}
      >
        删除嵌套目录
      </button>
      <button
        type="button"
        onClick={() =>
          onPathRenamed(
            "nested",
            "renamed",
            "renamed",
            undefined,
            true,
            rootPath,
          )
        }
      >
        重命名嵌套目录
      </button>
    </div>
  ),
}));

vi.mock("../../components/workspace-file-viewer", () => ({
  WorkspaceFileViewer: ({
    path,
    draftKey,
    toolbarAccessory,
    fileView,
    onFileViewChange,
  }: {
    path: string;
    draftKey: string;
    toolbarAccessory?: ReactNode;
    fileView?: WorkspaceFileView;
    onFileViewChange?: (view: WorkspaceFileView) => void;
  }) => (
    <div
      data-testid="file-viewer"
      data-path={path}
      data-draft-key={draftKey}
      data-view={fileView}
    >
      {workbenchDocumentBuffers.get(draftKey)?.text ?? path}
      {toolbarAccessory}
      <button type="button" onClick={() => onFileViewChange?.("code")}>
        代码视图
      </button>
    </div>
  ),
}));

const ownerKey = "test-chat:files-tab";
const firstFile = {
  name: "one.md",
  path: "projects/one.md",
  rootPath: "/repo",
  canonicalPath: "/repo/projects/one.md",
};

function renderWorkspace(
  props: Partial<Parameters<typeof WorkbenchFileWorkspace>[0]> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onSelectedFileChange = vi.fn();
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchFileWorkspace
        ownerKey={ownerKey}
        rootPath="/repo"
        scopePath="projects"
        chatFiles={[]}
        fileSource="project"
        onDownloadChatFile={vi.fn(async () => {})}
        onDeleteChatFile={vi.fn(async () => true)}
        onSelectedFileChange={onSelectedFileChange}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { onSelectedFileChange };
}

afterEach(() => {
  discardFileWorkspaceDrafts(ownerKey);
  resolveFileReference.mockReset();
  hasPlatformService.mockReturnValue(false);
  importDroppedWorkspaceFiles.mockClear();
});

it("文件页不显示来源切换，摘要打开的会话列表仍可浏览全部文件", () => {
  renderWorkspace({
    fileSource: "chat",
    chatFiles: [
      { name: "report.md", path: "attachments/report.md", source: "chat" },
    ],
  });
  expect(screen.queryByText("项目文件")).toBeNull();
  expect(screen.queryByText("会话文件")).toBeNull();
  expect(screen.getByText("report.md")).toBeVisible();
  expect(screen.queryByTestId("project-tree")).toBeNull();
});

describe("WorkbenchFileWorkspace", () => {
  it("会话附件有未保存的修改时阻止删除", async () => {
    const user = userEvent.setup();
    const onDeleteChatFile = vi.fn(async () => true);
    const attachment: WorkspaceFile = {
      name: "note.txt",
      path: "attachments/message/note.txt",
      source: "chat",
    };
    const reference = {
      name: attachment.name,
      path: "message/note.txt",
      rootPath: "/private/attachments",
      canonicalPath: "/private/attachments/message/note.txt",
      displayPath: attachment.name,
    };
    renderWorkspace({
      fileSource: "chat",
      scopeChatId: "chat-1",
      chatFiles: [attachment],
      initialFile: reference,
      onDeleteChatFile,
    });
    const draft = screen
      .getByTestId("file-viewer")
      .getAttribute("data-draft-key");
    if (!draft) throw new Error("缺少附件草稿键");
    workbenchDocumentBuffers.begin(draft, "original");
    workbenchDocumentBuffers.change(draft, "changed");
    resolveFileReference.mockResolvedValueOnce({
      status: "resolved",
      file: reference,
    });

    await user.click(
      screen.getByRole("button", { name: "note.txt 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除文件" }));
    await user.click(screen.getByRole("button", { name: "删除文件" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "请先保存或放弃此文件的修改。",
    );
    expect(onDeleteChatFile).not.toHaveBeenCalled();
    expect(workbenchDocumentBuffers.get(draft)?.text).toBe("changed");
  });

  it.each([false, true])(
    "删除会话附件只影响附件自身预览，当前预览属于附件：%s",
    async (previewAttachment) => {
      const user = userEvent.setup();
      const logical = "attachments/message/note.txt";
      const reference = {
        name: "note.txt",
        path: "message/note.txt",
        rootPath: "/private/attachments",
        canonicalPath: "/private/attachments/message/note.txt",
        displayPath: "note.txt",
      };
      renderWorkspace({
        fileSource: "chat",
        scopeChatId: "chat-1",
        chatFiles: [{ name: "note.txt", path: logical, source: "chat" }],
        initialFile: {
          name: "note.txt",
          path: previewAttachment ? reference.path : logical,
          rootPath: previewAttachment ? reference.rootPath : "/repo",
          canonicalPath: previewAttachment
            ? reference.canonicalPath
            : `/repo/${logical}`,
        },
      });
      resolveFileReference.mockResolvedValueOnce({
        status: "resolved",
        file: reference,
      });
      await user.click(
        screen.getByRole("button", { name: "note.txt 的文件操作" }),
      );
      await user.click(screen.getByRole("menuitem", { name: "删除文件" }));
      await user.click(screen.getByRole("button", { name: "删除文件" }));
      await waitFor(() =>
        expect(resolveFileReference).toHaveBeenCalledWith(
          expect.objectContaining({ path: logical }),
          expect.objectContaining({ baseDirectory: "", workspaceRoot: "" }),
        ),
      );
      if (previewAttachment)
        await waitFor(() =>
          expect(screen.queryByTestId("file-viewer")).toBeNull(),
        );
      else
        expect(screen.getByTestId("file-viewer")).toHaveAttribute(
          "data-path",
          logical,
        );
    },
  );

  it("外层工作树的目录操作按绝对范围识别内层根预览和草稿", async () => {
    const user = userEvent.setup();
    renderWorkspace({
      sourceRoots: ["/work", "/work/nested"],
      initialFile: {
        name: "a.md",
        path: "a.md",
        rootPath: "/work/nested",
        canonicalPath: "/work/nested/a.md",
      },
    });
    const draft = screen
      .getByTestId("file-viewer")
      .getAttribute("data-draft-key");
    if (!draft) throw new Error("缺少文件草稿键");
    workbenchDocumentBuffers.begin(draft, "original");
    workbenchDocumentBuffers.change(draft, "changed");
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    await user.click(screen.getByRole("button", { name: "work" }));
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-active-path",
      "/work/nested/a.md",
    );
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-directory-dirty",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "删除嵌套目录" }));
    expect(screen.queryByTestId("file-viewer")).toBeNull();
  });

  it("从外层根重命名包含内层根的目录不会重复拼接路径", async () => {
    const user = userEvent.setup();
    renderWorkspace({
      sourceRoots: ["/work", "/work/nested"],
      initialFile: {
        name: "a.md",
        path: "a.md",
        rootPath: "/work/nested",
        canonicalPath: "/work/nested/a.md",
      },
    });
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    await user.click(screen.getByRole("button", { name: "work" }));
    await user.click(screen.getByRole("button", { name: "重命名嵌套目录" }));
    await waitFor(() =>
      expect(resolveFileReference).toHaveBeenCalledWith(
        expect.objectContaining({ path: "/work/renamed/a.md" }),
        expect.objectContaining({ baseDirectory: "/work" }),
      ),
    );
  });
  it("工作目录的同名文件操作不会误命中空间目录的草稿和预览", async () => {
    const user = userEvent.setup();
    renderWorkspace({
      sourceRoots: ["/work"],
      initialFile: firstFile,
    });
    const firstDraftKey = screen
      .getByTestId("file-viewer")
      .getAttribute("data-draft-key");
    if (!firstDraftKey) throw new Error("缺少文件草稿键");
    workbenchDocumentBuffers.begin(firstDraftKey, "original");
    workbenchDocumentBuffers.change(firstDraftKey, "space draft");
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索文件" }), {
      target: { value: "one" },
    });
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-dirty",
      "true",
    );
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-dirty",
      "false",
    );
    await user.click(screen.getByRole("button", { name: "重命名当前根文件" }));
    expect(resolveFileReference).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "删除当前根文件" }));
    expect(screen.getByTestId("file-viewer")).toHaveAttribute(
      "data-draft-key",
      firstDraftKey,
    );
    expect(workbenchDocumentBuffers.get(firstDraftKey)?.text).toBe(
      "space draft",
    );
  });

  it("拖入工作目录的文件按实际工作根导入", async () => {
    const user = userEvent.setup();
    hasPlatformService.mockReturnValue(true);
    renderWorkspace({ scopeChatId: "chat-1", sourceRoots: ["/work"] });
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    const file = new File(["hello"], "hello.txt");
    const dropTarget = screen.getByTestId("project-tree").parentElement;
    if (!dropTarget) throw new Error("缺少文件拖放区域");
    fireEvent.drop(dropTarget, {
      dataTransfer: { types: ["Files"], files: [file] },
    });
    expect(importDroppedWorkspaceFiles).toHaveBeenCalledWith(
      "chat-1",
      "",
      [file],
      "/work",
    );
  });

  it("会话附件列表使用当前会话逻辑路径，不与空间同名文件混用", async () => {
    const user = userEvent.setup();
    renderWorkspace({
      fileSource: "chat",
      scopeChatId: "chat-1",
      chatFiles: [
        { name: "report.md", path: "attachments/report.md", source: "chat" },
      ],
    });
    await user.click(screen.getByRole("treeitem", { name: "report.md" }));
    await waitFor(() =>
      expect(resolveFileReference).toHaveBeenCalledWith(
        expect.objectContaining({ path: "attachments/report.md" }),
        expect.objectContaining({
          chatId: "chat-1",
          baseDirectory: "",
          workspaceRoot: "",
        }),
      ),
    );
  });
  it("没有实际工作目录时只展示空间树，不显示目录切换", () => {
    renderWorkspace({ sourceRoots: ["/repo/"] });
    expect(screen.queryByRole("radiogroup", { name: "目录" })).toBeNull();
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-root",
      "/repo",
    );
  });

  it("草稿没有空间根时直接浏览已选工作目录，不伪造空间入口", () => {
    renderWorkspace({
      rootPath: "",
      sourceRoots: ["/work"],
      spaceRootAvailable: false,
    });
    expect(screen.queryByRole("radiogroup", { name: "目录" })).toBeNull();
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-root",
      "/work",
    );
  });

  it("空间目录和工作目录切换使用各自真实根，同名文件草稿不会混用", async () => {
    const user = userEvent.setup();
    renderWorkspace({
      sourceRoots: ["/repo", "/work"],
      initialFile: firstFile,
    });
    const firstDraftKey = screen
      .getByTestId("file-viewer")
      .getAttribute("data-draft-key");
    if (!firstDraftKey) throw new Error("缺少文件草稿键");
    workbenchDocumentBuffers.begin(firstDraftKey, "original");
    workbenchDocumentBuffers.change(firstDraftKey, "space draft");

    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-root",
      "/work",
    );
    expect(screen.getByTestId("file-viewer")).toHaveAttribute(
      "data-draft-key",
      firstDraftKey,
    );
    await user.click(screen.getByRole("button", { name: "one.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).not.toHaveAttribute(
        "data-draft-key",
        firstDraftKey,
      ),
    );
    expect(resolveFileReference).toHaveBeenLastCalledWith(
      expect.objectContaining({ path: "/work/projects/one.md" }),
      expect.objectContaining({
        workspaceRoot: "/work",
        baseDirectory: "/work",
      }),
    );

    await user.click(screen.getByRole("radio", { name: "空间目录" }));
    await user.click(screen.getByRole("button", { name: "one.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-draft-key",
        firstDraftKey,
      ),
    );
    expect(screen.getByTestId("file-viewer")).toHaveTextContent("space draft");
  });

  it("多个工作目录按实际目录名展开，各树使用原目录", async () => {
    const user = userEvent.setup();
    renderWorkspace({ sourceRoots: ["/work/alpha", "/work/beta", "/repo"] });
    await user.click(screen.getByRole("radio", { name: "工作目录" }));
    expect(screen.queryByTestId("project-tree")).toBeNull();
    await user.click(screen.getByRole("button", { name: "beta" }));
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-root",
      "/work/beta",
    );
    await user.click(screen.getByRole("button", { name: "two.md" }));
    await waitFor(() =>
      expect(resolveFileReference).toHaveBeenLastCalledWith(
        expect.anything(),
        expect.objectContaining({ workspaceRoot: "/work/beta" }),
      ),
    );
  });

  it("项目树点击文件只替换同一文件 Tab 的主查看区", async () => {
    const user = userEvent.setup();
    const { onSelectedFileChange } = renderWorkspace({
      initialFile: firstFile,
    });
    expect(screen.getByTestId("file-viewer")).toHaveAttribute(
      "data-path",
      "projects/one.md",
    );

    await user.click(screen.getByRole("button", { name: "two.md" }));

    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-path",
        "projects/two.md",
      ),
    );
    expect(screen.getAllByTestId("file-viewer")).toHaveLength(1);
    expect(onSelectedFileChange).toHaveBeenCalledWith(
      expect.objectContaining({ canonicalPath: "/repo/projects/two.md" }),
    );
    expect(resolveFileReference).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ workspaceRoot: "/repo" }),
    );
  });

  it("树内回访文件时恢复该文件上次选择的查看方式", async () => {
    const user = userEvent.setup();
    renderWorkspace({ initialFile: firstFile });
    await user.click(screen.getByRole("button", { name: "代码视图" }));
    expect(screen.getByTestId("file-viewer")).toHaveAttribute(
      "data-view",
      "code",
    );
    await user.click(screen.getByRole("button", { name: "two.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-path",
        "projects/two.md",
      ),
    );
    expect(screen.getByTestId("file-viewer")).not.toHaveAttribute("data-view");
    await user.click(screen.getByRole("button", { name: "one.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-view",
        "code",
      ),
    );
  });

  it("目录按钮隐藏与恢复右侧筛选树，主查看区持续挂载", async () => {
    const user = userEvent.setup();
    renderWorkspace({ initialFile: firstFile });
    expect(
      screen.getByRole("complementary", { name: "文件目录" }),
    ).toBeVisible();
    expect(
      screen.getByRole("complementary", { name: "文件目录" }),
    ).not.toHaveClass("absolute");
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "隐藏文件目录" }));
    expect(
      screen.queryByRole("complementary", { name: "文件目录" }),
    ).toBeNull();
    expect(
      screen.queryByRole("separator", { name: "调整分屏比例" }),
    ).toBeNull();
    expect(screen.getByTestId("file-viewer")).toHaveAttribute(
      "data-path",
      "projects/one.md",
    );

    await user.click(screen.getByRole("button", { name: "显示文件目录" }));
    expect(
      screen.getByRole("complementary", { name: "文件目录" }),
    ).toBeVisible();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索文件" }), {
      target: { value: "two" },
    });
    expect(screen.getByTestId("project-tree")).toHaveAttribute(
      "data-query",
      "two",
    );
  });

  it("树内切换文件保留各自草稿，关闭时按 Tab 整组检查", async () => {
    const user = userEvent.setup();
    renderWorkspace({ initialFile: firstFile });
    const firstDraftKey = screen
      .getByTestId("file-viewer")
      .getAttribute("data-draft-key");
    if (!firstDraftKey) throw new Error("缺少文件草稿键");
    workbenchDocumentBuffers.begin(firstDraftKey, "original");
    workbenchDocumentBuffers.change(firstDraftKey, "unsaved draft");

    await user.click(screen.getByRole("button", { name: "two.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-path",
        "projects/two.md",
      ),
    );
    expect(screen.getByTestId("file-viewer")).not.toHaveAttribute(
      "data-draft-key",
      firstDraftKey,
    );
    expect(hasDirtyFileWorkspaceDrafts(ownerKey)).toBe(true);
    expect(workbenchDocumentBuffers.get(firstDraftKey)?.text).toBe(
      "unsaved draft",
    );

    await user.click(screen.getByRole("button", { name: "one.md" }));
    await waitFor(() =>
      expect(screen.getByTestId("file-viewer")).toHaveAttribute(
        "data-draft-key",
        firstDraftKey,
      ),
    );
    expect(screen.getByTestId("file-viewer")).toHaveTextContent(
      "unsaved draft",
    );

    discardFileWorkspaceDrafts(ownerKey);
    expect(hasDirtyFileWorkspaceDrafts(ownerKey)).toBe(false);
  });
});
