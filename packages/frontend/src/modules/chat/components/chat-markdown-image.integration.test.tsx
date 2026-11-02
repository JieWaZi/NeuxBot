import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type * as Platform from "@/shared/platform";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import "@/shared/i18n";
import { AssistantMarkdown } from "./chat-message-content";

const bridge = vi.hoisted(() => ({
  resolve: vi.fn<typeof Platform.resolveWorkspaceResourceReference>(),
  open: vi.fn<typeof Platform.openWorkspaceResourceScope>(),
  stat: vi.fn<typeof Platform.statWorkspaceResource>(),
  read: vi.fn<typeof Platform.readWorkspaceResource>(),
}));
vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof Platform>()),
  resolveWorkspaceResourceReference: bridge.resolve,
  openWorkspaceResourceScope: bridge.open,
  statWorkspaceResource: bridge.stat,
  readWorkspaceResource: bridge.read,
}));
const clients: QueryClient[] = [];
const metadata = {
  relativePath: "docs/chart.png",
  size: 4,
  version: "1",
  modifiedAt: 1,
  mediaType: "image/png",
};

beforeEach(() => {
  bridge.resolve.mockResolvedValue({
    status: "resolved",
    canonicalPath: "/project/docs/chart.png",
    baseDirectory: "/project/docs",
    workspaceRoot: "/project",
    sourceRoots: [],
  });
  bridge.open.mockResolvedValue({ id: "scope", root: "/project" });
  bridge.stat.mockResolvedValue(metadata);
  bridge.read.mockResolvedValue({
    ...metadata,
    bytes: new Uint8Array([137, 80, 78, 71]),
  });
  vi.stubGlobal("IntersectionObserver", undefined);
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => "blob:chat-chart");
      static revokeObjectURL = vi.fn();
    },
  );
});
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear());
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});
function show(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const tree = (body: ReactNode) => (
    <QueryClientProvider client={client}>
      <FileReferenceProvider
        context={{
          chatId: "chat",
          workspaceRoot: "/project",
          baseDirectory: "/project/docs",
          sourceRoots: [],
        }}
      >
        {body}
      </FileReferenceProvider>
    </QueryClientProvider>
  );
  const view = render(tree(children));
  return { ...view, rerender: (body: ReactNode) => view.rerender(tree(body)) };
}

it("真实聊天正文在结束态接入授权本地图片，并透传作者说明", async () => {
  const text = '结果如下：\n\n![本地图](./chart.png "作者的图像说明")';
  const view = show(<AssistantMarkdown text={text} isTerminal={false} />);
  await act(async () => {});
  expect(bridge.resolve).not.toHaveBeenCalled();
  expect(bridge.read).not.toHaveBeenCalled();
  view.rerender(<AssistantMarkdown text={text} />);
  await waitFor(() =>
    expect(screen.getByRole("img", { name: "本地图" })).toHaveAttribute(
      "src",
      "blob:chat-chart",
    ),
  );
  expect(screen.getByRole("img", { name: "本地图" })).toHaveAttribute(
    "title",
    "作者的图像说明",
  );
  expect(bridge.resolve).toHaveBeenCalledWith(
    "chat",
    "./chart.png",
    "/project/docs",
  );
  expect(bridge.read).toHaveBeenCalledTimes(1);
  expect(bridge.read).toHaveBeenCalledWith(
    "scope",
    "docs/chart.png",
    expect.any(AbortSignal),
    "/project",
  );
});

it("聊天远程图片仍保留说明且不创建图片请求，禁用文件引用时不启动本地读取", async () => {
  const view = show(
    <AssistantMarkdown text="![远程示意图](https://example.com/chart.png)" />,
  );
  await act(async () => {});
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.textContent).toContain("远程示意图");
  view.rerender(
    <AssistantMarkdown
      allowFileReferences={false}
      text="![本地图](./chart.png)"
    />,
  );
  await act(async () => {});
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.textContent).toContain("本地图");
  expect(bridge.resolve).not.toHaveBeenCalled();
  expect(bridge.read).not.toHaveBeenCalled();
});
