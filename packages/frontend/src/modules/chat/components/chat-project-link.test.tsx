import { TextMessagePartProvider } from "@assistant-ui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type * as DesktopModule from "@/shared/platform";
import type { FileReferenceRequest } from "@/shared/platform";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { WorkbenchProvider, useWorkbench } from "../workbench/workbench-store";
import { AssistantText } from "./chat-message-content";
import { useChatWorkspaceBindings } from "./use-chat-workspace-bindings";

vi.mock("@/shared/platform", async (importOriginal) => {
  const { fakeFileReference } = await import("@/test/file-reference-fake");
  return {
    ...(await importOriginal<typeof DesktopModule>()),
    resolveFileReferencePath: async (request: FileReferenceRequest) =>
      fakeFileReference(request, {
        [`${request.workspaceRoot}/projects/share`]:
          "/Users/example/Desktop/share",
        [request.workspaceRoot.replace(/\/(?:leader|members\/[^/]+)$/, "") +
        "/projects/share"]: "/Users/example/Desktop/share",
      }),
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
  beforeEach(() => localStorage.clear());
  it("软链接与真实路径复用一个右侧文件标签", async () => {
    const user = userEvent.setup();
    const root =
      "/Users/example/Library/Application Support/com.neuxally.desktop/team/7481f05f7a4a4d3ab5eb0714a9fe1465/workspace/leader";
    renderLinks(
      root,
      "已创建 `/Users/example/Library/Application Support/com.neuxally.desktop/team/7481f05f7a4a4d3ab5eb0714a9fe1465/workspace/projects/share/a.txt`（即 `/Users/example/Desktop/share/a.txt`）。",
    );
    const links = await screen.findAllByRole("link", { name: "a.txt" });
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
    "/Users/example/Library/Application Support/com.neuxally.desktop/chat/chat-1/workspace",
    "/Users/example/Library/Application Support/com.neuxally.desktop/team/thread/workspace/leader",
    "/Users/example/Library/Application Support/com.neuxally.desktop/team/thread/workspace/members/member-1",
  ])("项目文件链接打开右侧标签：%s", async (workspaceRoot) => {
    const user = userEvent.setup();
    renderLinks(
      workspaceRoot,
      "[index.html](/Users/example/Desktop/share/index.html) [说明](file:///Users/example/Desktop/share/readme.md) [相对入口](projects/share/index.html)",
    );
    await user.click(await screen.findByRole("link", { name: "index.html" }));
    await user.click(screen.getByRole("link", { name: "相对入口" }));
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
