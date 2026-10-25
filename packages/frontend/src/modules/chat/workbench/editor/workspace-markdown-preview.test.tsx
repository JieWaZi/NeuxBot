import { render, screen, waitFor } from "@testing-library/react";
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
}));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it("loads a local Markdown image through its Chat file scope and revokes the Blob URL", async () => {
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
    <WorkspaceMarkdownPreview
      source="![本地图](./图%20片.png)"
      chatId="chat-1"
      rootPath="/project"
      path="docs/readme.md"
    />,
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
});

it("renders safe Markdown HTML while stripping active content", async () => {
  const { container } = render(
    <WorkspaceMarkdownPreview
      source={
        "<details><summary>说明</summary><p>正文</p></details>\n<script>alert(1)</script>"
      }
      rootPath="/project"
      path="readme.md"
    />,
  );
  await waitFor(() =>
    expect(container.querySelector("details")).not.toBeNull(),
  );
  expect(container.querySelector("summary")).toHaveTextContent("说明");
  expect(container.querySelector("script")).toBeNull();
});
