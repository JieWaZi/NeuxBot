import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
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

const { resolveFileReference } = vi.hoisted(() => ({
  resolveFileReference: vi.fn(async (candidate: { path: string }) => {
    const name = candidate.path.split("/").at(-1) ?? candidate.path;
    return {
      status: "resolved" as const,
      file: {
        name,
        path: candidate.path,
        canonicalPath: `/repo/${name}`,
        rootPath: "/repo",
        displayPath: name,
      },
    };
  }),
}));

vi.mock("@/shared/workspace/file-reference", async (importOriginal) => ({
  ...(await importOriginal<typeof FileReferenceModule>()),
  resolveFileReference,
}));

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
 hasPlatformService: () => false,
  importDroppedWorkspaceFiles: vi.fn(),
}));

vi.mock("../../components/workspace-file-browser", () => ({
  WorkspaceFileBrowser: ({
    onOpenFile,
    query,
  }: {
    onOpenFile: (file: { name: string; path: string }) => void;
    query: string;
  }) => (
    <div data-testid="project-tree" data-query={query}>
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
  canonicalPath: "/repo/one.md",
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
  resolveFileReference.mockClear();
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
      expect.objectContaining({ canonicalPath: "/repo/two.md" }),
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
