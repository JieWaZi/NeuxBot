import { TextMessagePartProvider } from "@assistant-ui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type * as DesktopModule from "@/shared/platform";
import type { FileReferenceRequest } from "@/shared/platform";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { WorkbenchProvider, useWorkbench } from "../workbench/workbench-store";
import { AssistantText } from "@/test/chat-markdown-context";
import { useChatWorkspaceBindings } from "./use-chat-workspace-bindings";
import { workbenchDocumentBuffers } from "../workbench/document-buffers";

vi.mock("@/shared/platform", async (importOriginal) => {
  const { fakeFileReference } = await import("@/test/file-reference-fake");
  return {
    ...(await importOriginal<typeof DesktopModule>()),
    resolveFileReferencePath: async (request: FileReferenceRequest) =>
      fakeFileReference(request),
  };
});

function ProjectLinkHarness({
  workspaceRoot,
  text,
}: {
  workspaceRoot: string;
  text: string;
}) {
  const scopeKey = "project-links";
  const workbench = useWorkbench(scopeKey);
  const roots = ["/Users/example/Desktop/share"];
  const { openBrowserLink, openAttachmentFile } = useChatWorkspaceBindings({
    additionalWorkingDirectories: roots,
    fileSource: "project",
    query: "",
    updateViewState: () => {},
    workspaceDirectory: workspaceRoot,
    workspaceFiles: [],
    workspacePanelOpen: false,
    workspaceScopeKey: scopeKey,
  });
  return (
    <>
      <FileReferenceProvider
        context={{
          baseDirectory: workspaceRoot,
          workspaceRoot,
          sourceRoots: roots,
        }}
        onOpenURL={openBrowserLink}
        onOpenFile={(file) => openAttachmentFile({ ...file, reference: file })}
      >
        <TextMessagePartProvider text={text} isRunning={false}>
          <AssistantText />
        </TextMessagePartProvider>
      </FileReferenceProvider>
      <output data-testid="workbench-tabs">
        {JSON.stringify(workbench.snapshot.tabs)}
      </output>
    </>
  );
}

function renderLinks(workspaceRoot: string, text: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <WorkspaceProvider>
        <WorkbenchProvider>
          <ProjectLinkHarness workspaceRoot={workspaceRoot} text={text} />
        </WorkbenchProvider>
      </WorkspaceProvider>
    </QueryClientProvider>,
  );
}

function workbenchTabs(): Array<{
  kind: string;
  path?: string;
  rootPath?: string;
}> {
  return JSON.parse(screen.getByTestId("workbench-tabs").textContent ?? "[]");
}

describe("project link navigation", () => {
  it("目录级操作识别最深授权根的已打开文件和未保存草稿", async () => {
    const user = userEvent.setup();
    const scopeKey = "nested-file-operations";
    let binding: ReturnType<typeof useChatWorkspaceBindings> | undefined;
    let board: ReturnType<typeof useWorkbench> | undefined;
    function Harness() {
      const workbench = useWorkbench(scopeKey);
      const workspaceBinding = useChatWorkspaceBindings({
        activeChatId: "chat-1",
        additionalWorkingDirectories: ["/work", "/work/nested"],
        fileSource: "project",
        query: "",
        updateViewState: () => {},
        workspaceDirectory: "/repo",
        workspaceFiles: [],
        workspacePanelOpen: false,
        workspaceScopeKey: scopeKey,
      });
      useEffect(() => {
        board = workbench;
        binding = workspaceBinding;
      });
      return (
        <button
          type="button"
          onClick={() =>
            workbench.dispatch({
              type: "open-file",
              title: "a.md",
              rootPath: "/work/nested",
              path: "a.md",
              canonicalPath: "/work/nested/a.md",
            })
          }
        >
          打开嵌套根文件
        </button>
      );
    }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider>
          <WorkbenchProvider>
            <Harness />
          </WorkbenchProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "打开嵌套根文件" }));
    const tab = board?.snapshot.tabs[0];
    if (!tab || !binding) throw new Error("文件工作台未就绪");
    const draft = `${scopeKey}:${tab.id}`;
    workbenchDocumentBuffers.begin(draft, "old");
    workbenchDocumentBuffers.change(draft, "changed");
    expect(
      binding.fileWorkspaceProps.isPathDirty?.("nested", undefined, "/work"),
    ).toBe(true);
    expect(
      binding.fileWorkspaceProps.isPathDirty?.(
        "nested-backup",
        undefined,
        "/work",
      ),
    ).toBe(false);
    workbenchDocumentBuffers.discardPrefix(scopeKey);
    act(() =>
      binding?.fileWorkspaceProps.onPathRenamed?.(
        "nested",
        "renamed",
        "renamed",
        undefined,
        true,
        "/work",
      ),
    );
    expect(board?.snapshot.tabs[0]).toMatchObject({
      rootPath: "/work",
      path: "renamed/a.md",
      canonicalPath: "/work/renamed/a.md",
    });
    act(() =>
      binding?.fileWorkspaceProps.onPathDeleted?.(
        "renamed",
        undefined,
        true,
        "/work",
      ),
    );
    expect(board?.snapshot.tabs).toEqual([]);
  });
  beforeEach(() => {
    const preferences = new Map<string, string>();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => preferences.get(key) ?? null,
        setItem: (key: string, value: string) => preferences.set(key, value),
        removeItem: (key: string) => preferences.delete(key),
      },
    });
  });
  it("真实项目绝对路径与文件链接复用一个右侧文件标签", async () => {
    const user = userEvent.setup();
    const root = "/managed/Workspaces/thread/leader";
    const managedPath = "file:///Users/example/Desktop/share/a.txt";
    const projectPath = "/Users/example/Desktop/share/a.txt";
    renderLinks(
      root,
      `已创建 [${managedPath}](<${managedPath}>)（即 [${projectPath}](${projectPath})）。`,
    );
    const links = await screen.findAllByRole("link", { name: /\/a\.txt$/ });
    for (const link of links) await user.click(link);
    await waitFor(() => {
      expect(workbenchTabs()).toEqual([
        expect.objectContaining({
          kind: "file",
          path: "a.txt",
          rootPath: "/Users/example/Desktop/share",
        }),
      ]);
    });
  });

  it.each([
    "/managed/Agents/waker",
    "/managed/Workspaces/thread/leader",
    "/managed/Workspaces/thread/members/member-1",
  ])("项目文件链接打开右侧标签：%s", async (workspaceRoot) => {
    const user = userEvent.setup();
    renderLinks(
      workspaceRoot,
      "[index.html](/Users/example/Desktop/share/index.html) [说明](file:///Users/example/Desktop/share/readme.md) [重复入口](/Users/example/Desktop/share/index.html)",
    );
    await user.click(await screen.findByRole("link", { name: "index.html" }));
    await user.click(screen.getByRole("link", { name: "重复入口" }));
    await waitFor(() => {
      expect(workbenchTabs()).toEqual([
        expect.objectContaining({
          kind: "file",
          path: "index.html",
          rootPath: "/Users/example/Desktop/share",
        }),
      ]);
    });
    await user.click(screen.getByRole("link", { name: "说明" }));
    await waitFor(() => {
      expect(workbenchTabs()).toHaveLength(2);
      expect(workbenchTabs()[1]).toMatchObject({
        kind: "file",
        path: "readme.md",
        rootPath: "/Users/example/Desktop/share",
      });
    });
  });
});
