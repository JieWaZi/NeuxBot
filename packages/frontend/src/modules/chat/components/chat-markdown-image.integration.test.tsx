import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type * as Platform from "@/shared/platform";
import { MarkdownVisibility } from "@/shared/markdown/visibility";
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
function show(children: ReactNode, onOpenFile = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const tree = (body: ReactNode) => (
    <QueryClientProvider client={client}>
      <FileReferenceProvider
        onOpenFile={onOpenFile}
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

it.each([
  ["png", "image/png"],
  ["jpg", "image/jpeg"],
  ["jpeg", "image/jpeg"],
  ["gif", "image/gif"],
  ["webp", "image/webp"],
])(
  "独立的 %s 图片链接显示缩略图，点击后弹窗放大",
  async (extension, mediaType) => {
    const relativePath = `docs/chart.${extension}`;
    bridge.resolve.mockResolvedValue({
      status: "resolved",
      canonicalPath: `/project/${relativePath}`,
      baseDirectory: "/project/docs",
      workspaceRoot: "/project",
      sourceRoots: [],
    });
    bridge.stat.mockResolvedValue({ ...metadata, relativePath, mediaType });
    bridge.read.mockResolvedValue({
      ...metadata,
      relativePath,
      mediaType,
      bytes: new Uint8Array([1, 2, 3, 4]),
    });
    const onOpenFile = vi.fn();
    const text = `[查看故障复盘图](./chart.${extension})`;
    const view = show(
      <AssistantMarkdown text={text} isTerminal={false} />,
      onOpenFile,
    );
    await act(async () => {});
    expect(bridge.read).not.toHaveBeenCalled();
    view.rerender(<AssistantMarkdown text={text} />);
    const image = await screen.findByRole("img", { name: "查看故障复盘图" });
    expect(image).toHaveAttribute("src", "blob:chat-chart");
    expect(screen.queryByRole("button", { name: /复制完整路径/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "查看故障复盘图" }));
    const dialog = screen.getByRole("dialog", { name: "查看故障复盘图" });
    expect(
      within(dialog).getByRole("img", { name: "查看故障复盘图" }),
    ).toHaveAttribute("src", "blob:chat-chart");
    expect(onOpenFile).not.toHaveBeenCalled();
    expect(bridge.resolve).toHaveBeenCalledTimes(1);
    expect(bridge.read).toHaveBeenCalledTimes(1);
  },
);

it("内嵌图片支持键盘打开和关闭放大预览", async () => {
  const user = userEvent.setup();
  const onOpenFile = vi.fn();
  show(<AssistantMarkdown text="![故障复盘图](./chart.png)" />, onOpenFile);
  const preview = await screen.findByRole("button", { name: "故障复盘图" });
  await user.tab();
  expect(preview).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(
    screen.getByRole("dialog", { name: "故障复盘图" }),
  ).toBeInTheDocument();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(preview).toHaveFocus();
  expect(onOpenFile).not.toHaveBeenCalled();
});

it("图片链接保留同一个缩略图位置，切回会话时重新加载图片内容", async () => {
  const text = "之前的内容\n\n[故障复盘图](./chart.png)\n\n后续的回复";
  const body = (visible: boolean) => (
    <MarkdownVisibility value={visible}>
      <AssistantMarkdown text={text} />
    </MarkdownVisibility>
  );
  const view = show(body(false));
  const paragraph = screen.getByText("故障复盘图").closest("p");
  const frame = paragraph?.querySelector(".aspect-video");
  expect(frame).toHaveClass("w-64", "max-w-full");
  expect(bridge.read).not.toHaveBeenCalled();
  view.rerender(body(true));
  const thumbnail = await screen.findByRole("img", { name: "故障复盘图" });
  expect(frame).toContainElement(thumbnail);
  view.rerender(body(false));
  expect(paragraph?.querySelector(".aspect-video")).toBe(frame);
  expect(thumbnail).not.toBeInTheDocument();
  view.rerender(body(true));
  expect(frame).toContainElement(
    await screen.findByRole("img", { name: "故障复盘图" }),
  );
});

it("图片预览与流程图一样适配视口，可放大、缩小和重置", async () => {
  const user = userEvent.setup();
  const width = vi
    .spyOn(HTMLElement.prototype, "offsetWidth", "get")
    .mockReturnValue(600);
  const height = vi
    .spyOn(HTMLElement.prototype, "offsetHeight", "get")
    .mockReturnValue(500);
  try {
    show(<AssistantMarkdown text="![故障复盘图](./chart.png)" />);
    const thumbnail = await screen.findByRole("button", { name: "故障复盘图" });
    await user.click(thumbnail);
    const dialog = within(screen.getByRole("dialog", { name: "故障复盘图" }));
    const original = dialog.getByRole("img", { name: "故障复盘图" });
    Object.defineProperties(original, {
      naturalWidth: { value: 1200 },
      naturalHeight: { value: 600 },
    });
    fireEvent.load(original);

    const canvas = original.parentElement?.parentElement;
    expect(canvas).toHaveStyle({ width: "600px", height: "300px" });
    await user.click(dialog.getByRole("button", { name: "放大" }));
    expect(canvas).toHaveStyle({ width: "750px", height: "375px" });
    await user.click(dialog.getByRole("button", { name: "缩小" }));
    await user.click(dialog.getByRole("button", { name: "缩小" }));
    expect(canvas).toHaveStyle({ width: "450px", height: "225px" });
    await user.click(dialog.getByRole("button", { name: "重置缩放" }));
    expect(canvas).toHaveStyle({ width: "600px", height: "300px" });
    expect(bridge.read).toHaveBeenCalledTimes(1);
  } finally {
    width.mockRestore();
    height.mockRestore();
  }
});

it("已经包在链接里的内嵌图片保留该链接的操作", async () => {
  show(<AssistantMarkdown text="[![故障复盘图](./chart.png)](./report.md)" />);
  await screen.findByRole("img", { name: "故障复盘图" });
  const link = screen.getByRole("link", { name: "故障复盘图" });
  expect(link).toHaveAttribute("href", "file:///./report.md");
  expect(screen.queryByRole("button", { name: "故障复盘图" })).toBeNull();
});

it("正文中的图片文件链接保持原来的行内显示", async () => {
  show(<AssistantMarkdown text="可参照[故障复盘图](./chart.png)核对。" />);
  await act(async () => {});
  expect(screen.getByRole("link", { name: "故障复盘图" })).toBeInTheDocument();
  expect(screen.queryByRole("img", { name: "故障复盘图" })).toBeNull();
  expect(bridge.read).not.toHaveBeenCalled();
});

it("独立图片链接预览读取失败时仍可打开文件", async () => {
  bridge.read.mockRejectedValue(new Error("unavailable"));
  const onOpenFile = vi.fn();
  show(<AssistantMarkdown text="[故障复盘图](./chart.png)" />, onOpenFile);
  await waitFor(() => expect(bridge.read).toHaveBeenCalledTimes(1));
  expect(screen.queryByRole("button", { name: /复制完整路径/ })).toBeNull();
  fireEvent.click(screen.getByRole("link", { name: "故障复盘图" }));
  expect(onOpenFile).toHaveBeenCalledWith(
    expect.objectContaining({ canonicalPath: "/project/docs/chart.png" }),
  );
  expect(screen.queryByRole("img", { name: "故障复盘图" })).toBeNull();
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
