import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type * as Desktop from "@/shared/platform";

import "@/shared/i18n";
import { WorkspaceMarkdownPreview } from "./workspace-markdown-preview";

const fileBridge = vi.hoisted(() => ({
  resolve: vi.fn(async () => ({
    status: "resolved" as const,
    canonicalPath: "/project/docs/图 片.png",
    baseDirectory: "/project/docs",
    workspaceRoot: "/project",
    sourceRoots: [] as string[],
  })),
  open: vi.fn(async () => ({ id: "scope", root: "/project" })),
  stat: vi.fn(async () => ({
    relativePath: "docs/图 片.png",
    size: 4,
    version: "1",
    modifiedAt: 1,
    mediaType: "image/png",
  })),
  read: vi.fn(async () => ({
    relativePath: "docs/图 片.png",
    size: 4,
    version: "1",
    modifiedAt: 1,
    mediaType: "image/png",
    bytes: new Uint8Array([137, 80, 78, 71]),
  })),
}));

vi.mock("@/shared/platform", async (importOriginal) => ({
  ...(await importOriginal<typeof Desktop>()),
  isDesktopRuntime: () => true,
  hasPlatformService: () => true,
  resolveWorkspaceResourceReference: fileBridge.resolve,
  openWorkspaceResourceScope: fileBridge.open,
  statWorkspaceResource: fileBridge.stat,
  readWorkspaceResource: fileBridge.read,
  observeWorkspaceResource: async () => async () => {},
}));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it("loads a local Markdown image through its Chat file scope and revokes the Blob URL", async () => {
  const client = new QueryClient();
  const createObjectURL = vi.fn(() => "blob:ally-image");
  const revokeObjectURL = vi.fn();
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = createObjectURL;
      static revokeObjectURL = revokeObjectURL;
    },
  );

  const view = render(
    <QueryClientProvider client={client}>
      <WorkspaceMarkdownPreview
        source="![本地图](./图%20片.png)"
        chatId="chat-1"
        rootPath="/project"
        path="docs/readme.md"
      />
    </QueryClientProvider>,
  );

  await waitFor(() =>
    expect(screen.getByRole("img", { name: "本地图" })).toHaveAttribute(
      "src",
      "blob:ally-image",
    ),
  );
  expect(fileBridge.resolve).toHaveBeenCalledWith(
    "chat-1",
    "./图 片.png",
    "/project/docs",
  );
  expect(fileBridge.read).toHaveBeenCalledWith(
    "scope",
    "docs/图 片.png",
    expect.any(AbortSignal),
    "/project",
  );
  view.unmount();
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:ally-image");
  client.clear();
});

it("原始 HTML 作为文字回退，预览不插入 HTML 或脚本节点", () => {
  const source =
    "<details><summary>说明</summary><p>正文</p></details>\n<script>alert(1)</script>";
  const { container } = render(
    <WorkspaceMarkdownPreview
      source={source}
      rootPath="/project"
      path="readme.md"
    />,
  );
  expect(container.querySelector("details, summary, script")).toBeNull();
  expect(container.textContent).toContain(source);
});
