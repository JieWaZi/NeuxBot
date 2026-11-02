import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type * as Platform from "@/shared/platform";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
import { FileReferenceProvider } from "./file-reference-link";
import { WorkspaceMarkdownImage } from "./workspace-markdown-image";

const bridge = vi.hoisted(() => ({
  resolve: vi.fn<typeof Platform.resolveWorkspaceResourceReference>(),
  open: vi.fn<typeof Platform.openWorkspaceResourceScope>(),
  stat: vi.fn<typeof Platform.statWorkspaceResource>(),
  read: vi.fn<typeof Platform.readWorkspaceResource>(),
  observe: vi.fn<typeof Platform.observeWorkspaceResource>(),
}));
const sceneActive = vi.hoisted(() => vi.fn(() => true));
vi.mock("./app-scene-workspace", () => ({ useRouteSceneActive: sceneActive }));
vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof Platform>()),
  resolveWorkspaceResourceReference: bridge.resolve,
  openWorkspaceResourceScope: bridge.open,
  statWorkspaceResource: bridge.stat,
  readWorkspaceResource: bridge.read,
  observeWorkspaceResource: bridge.observe,
  hasPlatformService: () => true,
}));

const context = {
  chatId: "chat-1",
  baseDirectory: "/project/docs",
  workspaceRoot: "/project",
  sourceRoots: [],
};
const metadata = {
  relativePath: "docs/picture.png",
  size: 4,
  version: "1",
  modifiedAt: 1,
  mediaType: "image/png",
};
const file = { ...metadata, bytes: new Uint8Array([137, 80, 78, 71]) };
const clients: QueryClient[] = [];
let inViewport = true;
let observers: {
  callback: IntersectionObserverCallback;
  targets: Set<Element>;
}[] = [];
let nextURL = 0;
const createObjectURL = vi.fn((blob: Blob) => {
  void blob;
  return `blob:workspace-image-${++nextURL}`;
});
const revokeObjectURL = vi.fn();

function setup(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  function tree(content: ReactNode) {
    return (
      <QueryClientProvider client={client}>
        <FileReferenceProvider context={context}>
          {content}
        </FileReferenceProvider>
      </QueryClientProvider>
    );
  }
  const view = render(tree(children));
  return {
    ...view,
    client,
    rerender: (content: ReactNode) => view.rerender(tree(content)),
  };
}

beforeEach(() => {
  inViewport = true;
  observers = [];
  nextURL = 0;
  sceneActive.mockReturnValue(true);
  bridge.resolve.mockResolvedValue({
    status: "resolved",
    canonicalPath: "/project/docs/picture.png",
    baseDirectory: "/project/docs",
    workspaceRoot: "/project",
    sourceRoots: [],
  });
  bridge.open.mockResolvedValue({ id: "scope", root: "/project" });
  bridge.stat.mockResolvedValue(metadata);
  bridge.read.mockResolvedValue(file);
  bridge.observe.mockResolvedValue(async () => {});
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      current: (typeof observers)[number];
      constructor(callback: IntersectionObserverCallback) {
        this.current = { callback, targets: new Set() };
        observers.push(this.current);
      }
      observe(target: Element) {
        this.current.targets.add(target);
        queueMicrotask(() => {
          if (inViewport && this.current.targets.has(target))
            this.current.callback(
              [{ target, isIntersecting: true } as IntersectionObserverEntry],
              {} as IntersectionObserver,
            );
        });
      }
      unobserve(target: Element) {
        this.current.targets.delete(target);
      }
      disconnect() {
        this.current.targets.clear();
      }
    },
  );
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = createObjectURL;
      static revokeObjectURL = revokeObjectURL;
    },
  );
});

afterEach(() => {
  for (const client of clients.splice(0)) client.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

it("enters the viewport before resolving and reading, then releases its Blob URL", async () => {
  inViewport = false;
  const view = setup(
    <WorkspaceMarkdownImage src="./picture%20one.png" alt="示意图" />,
  );
  await act(async () => {});
  expect(bridge.resolve).not.toHaveBeenCalled();
  expect(bridge.read).not.toHaveBeenCalled();
  expect(screen.getByRole("img", { name: "示意图" })).not.toHaveAttribute(
    "src",
  );

  inViewport = true;
  act(() => {
    for (const observer of observers)
      observer.callback(
        [...observer.targets].map(
          (target) =>
            ({ target, isIntersecting: true }) as IntersectionObserverEntry,
        ),
        {} as IntersectionObserver,
      );
  });
  await waitFor(() =>
    expect(screen.getByRole("img", { name: "示意图" })).toHaveAttribute(
      "src",
      "blob:workspace-image-1",
    ),
  );
  expect(bridge.resolve).toHaveBeenCalledWith(
    "chat-1",
    "./picture one.png",
    "/project/docs",
  );
  expect(bridge.read).toHaveBeenCalledWith(
    "scope",
    "docs/picture.png",
    expect.any(AbortSignal),
    "/project",
  );
  inViewport = false;
  fireEvent.scroll(document);
  expect(bridge.read).toHaveBeenCalledTimes(1);
  expect(revokeObjectURL).not.toHaveBeenCalled();
  view.unmount();
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:workspace-image-1");
  await waitFor(() =>
    expect(
      view.client
        .getQueryCache()
        .findAll({ queryKey: ["workspace-markdown-image"] }),
    ).toHaveLength(0),
  );
});

it("aliases share one canonical resource read and keep the other visible consumer alive", async () => {
  function images(first: boolean, second: boolean) {
    return (
      <>
        <ContentVisibility.Provider value={first}>
          <WorkspaceMarkdownImage src="./picture.png" alt="第一张" />
        </ContentVisibility.Provider>
        <ContentVisibility.Provider value={second}>
          <WorkspaceMarkdownImage
            src="file:///project/docs/picture.png"
            alt="第二张"
          />
        </ContentVisibility.Provider>
      </>
    );
  }
  const view = setup(images(true, true));
  await waitFor(() =>
    expect(view.container.querySelectorAll("img")).toHaveLength(2),
  );
  expect(bridge.read).toHaveBeenCalledTimes(1);
  expect(bridge.stat).toHaveBeenCalledTimes(1);
  expect(bridge.observe).toHaveBeenCalledTimes(1);
  const secondURL = screen
    .getByRole("img", { name: "第二张" })
    .getAttribute("src");
  view.rerender(images(false, true));
  expect(revokeObjectURL).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("img", { name: "第二张" })).toHaveAttribute(
    "src",
    secondURL,
  );
  expect(
    view.client
      .getQueryCache()
      .findAll({ queryKey: ["workspace-markdown-image"] }),
  ).toHaveLength(1);
  view.rerender(images(false, false));
  await waitFor(() =>
    expect(
      view.client
        .getQueryCache()
        .findAll({ queryKey: ["workspace-markdown-image"] }),
    ).toHaveLength(0),
  );
  expect(revokeObjectURL).toHaveBeenCalledTimes(2);
  expect(bridge.observe.mock.calls[0]?.[3]?.aborted).toBe(true);
  view.unmount();
});

it("does not read a hidden Scene and cancels its last in-flight read when hidden", async () => {
  let complete: ((value: typeof file) => void) | undefined;
  bridge.read.mockImplementation(
    () =>
      new Promise((resolve) => {
        complete = resolve;
      }),
  );
  const image = (active: boolean) => (
    <ContentVisibility.Provider value={active}>
      <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />
    </ContentVisibility.Provider>
  );
  const view = setup(image(false));
  expect(bridge.resolve).not.toHaveBeenCalled();
  view.rerender(image(true));
  await waitFor(() => expect(bridge.read).toHaveBeenCalledTimes(1));
  const signal = bridge.read.mock.calls[0]?.[2];
  expect(signal?.aborted).toBe(false);
  view.rerender(image(false));
  expect(signal?.aborted).toBe(true);
  await act(async () => complete?.(file));
  expect(createObjectURL).not.toHaveBeenCalled();
  view.unmount();
});

it("releases the image when its Scene becomes inactive and reads again on return", async () => {
  const image = <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />;
  const view = setup(image);
  await waitFor(() =>
    expect(view.container.querySelector("img")).not.toBeNull(),
  );
  sceneActive.mockReturnValue(false);
  view.rerender(<WorkspaceMarkdownImage src="./picture.png" alt="示意图" />);
  await waitFor(() => expect(revokeObjectURL).toHaveBeenCalledTimes(1));
  expect(view.container.querySelector("img")).toBeNull();
  await waitFor(() =>
    expect(
      view.client
        .getQueryCache()
        .findAll({ queryKey: ["workspace-markdown-image"] }),
    ).toHaveLength(0),
  );
  sceneActive.mockReturnValue(true);
  view.rerender(<WorkspaceMarkdownImage src="./picture.png" alt="示意图" />);
  await waitFor(() => expect(bridge.read).toHaveBeenCalledTimes(2));
  await waitFor(() =>
    expect(view.container.querySelector("img")).not.toBeNull(),
  );
  view.unmount();
});

it.each([
  {
    stat: { ...metadata, size: workspaceSingleFileBytesLimit + 1 },
    text: "图片过大，无法预览",
  },
  {
    stat: { ...metadata, mediaType: "image/svg+xml" },
    text: "不支持此图片格式",
  },
  { stat: null, text: "图片无法载入" },
])(
  "retains alt text and explains rejected metadata: $text",
  async ({ stat, text }) => {
    bridge.stat.mockResolvedValue(stat);
    const view = setup(
      <WorkspaceMarkdownImage src="./picture.png" alt="保留说明" />,
    );
    await screen.findByText(text);
    expect(screen.getByRole("img", { name: "保留说明" })).not.toHaveAttribute(
      "src",
    );
    expect(bridge.read).not.toHaveBeenCalled();
    expect(createObjectURL).not.toHaveBeenCalled();
    view.unmount();
  },
);

it.each([
  {
    resource: { ...file, size: workspaceSingleFileBytesLimit + 1 },
    text: "图片过大，无法预览",
  },
  {
    resource: { ...file, mediaType: "image/svg+xml" },
    text: "不支持此图片格式",
  },
])(
  "validates the actual read after a stat/read replacement: $text",
  async ({ resource, text }) => {
    bridge.read.mockResolvedValue(resource);
    const view = setup(
      <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />,
    );
    await screen.findByText(text);
    expect(createObjectURL).not.toHaveBeenCalled();
    view.unmount();
  },
);

it("keeps missing images explicit and retries through the same authorized resolver", async () => {
  bridge.resolve.mockResolvedValueOnce({ status: "missing" });
  const view = setup(
    <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />,
  );
  await screen.findByText("图片无法载入");
  expect(bridge.read).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(view.container.querySelector("img")).not.toBeNull(),
  );
  expect(bridge.resolve).toHaveBeenCalledTimes(2);
  view.unmount();
});

it("blocks remote images by default, preserves explicit workspace opt-in, and drops srcSet", async () => {
  const view = setup(
    <WorkspaceMarkdownImage
      src="https://example.com/picture.png"
      srcSet="https://tracker.test/image.png 2x"
      alt="远程图"
    />,
  );
  await screen.findByText("远程图片未加载");
  expect(view.container.querySelector("img")).toBeNull();
  expect(bridge.resolve).not.toHaveBeenCalled();
  view.rerender(
    <WorkspaceMarkdownImage
      src="//example.com/picture.png"
      allowRemote
      srcSet="https://tracker.test/image.png 2x"
      alt="远程图"
    />,
  );
  await waitFor(() =>
    expect(screen.getByRole("img", { name: "远程图" })).toHaveAttribute(
      "src",
      "//example.com/picture.png",
    ),
  );
  expect(screen.getByRole("img", { name: "远程图" })).not.toHaveAttribute(
    "srcset",
  );
  fireEvent.error(screen.getByRole("img", { name: "远程图" }));
  await screen.findByText("图片无法载入");
  view.unmount();
});

it("never sends SVG data or a marker URL to the browser as an image source", async () => {
  const view = setup(
    <WorkspaceMarkdownImage
      src="data:image/svg+xml,%3Csvg%3E%3C/svg%3E"
      allowRemote
      alt="SVG"
    />,
  );
  await screen.findByText("不支持此图片格式");
  expect(view.container.querySelector("img")).toBeNull();
  view.rerender(
    <WorkspaceMarkdownImage
      src="https://ally.invalid/__workbench_markdown_image__?path=.%2Fpicture.png"
      alt="示意图"
    />,
  );
  await waitFor(() =>
    expect(view.container.querySelector("img")).toHaveAttribute(
      "src",
      "blob:workspace-image-1",
    ),
  );
  expect(bridge.resolve).toHaveBeenCalledWith(
    "chat-1",
    "./picture.png",
    "/project/docs",
  );
  view.unmount();
});

it.each(["png", "jpeg", "gif", "webp"])(
  "keeps bounded raster data:%s opt-in and releases the decoded Blob",
  async (type) => {
    const source = `data:image/${type};base64,iVBORw==`;
    const view = setup(<WorkspaceMarkdownImage src={source} alt="内嵌图" />);
    await screen.findByText("远程图片未加载");
    expect(createObjectURL).not.toHaveBeenCalled();
    view.rerender(
      <WorkspaceMarkdownImage src={source} alt="内嵌图" allowRemote />,
    );
    await waitFor(() =>
      expect(view.container.querySelector("img")).toHaveAttribute(
        "src",
        "blob:workspace-image-1",
      ),
    );
    expect(createObjectURL.mock.calls[0]?.[0]).toMatchObject({
      size: 4,
      type: `image/${type}`,
    });
    expect(bridge.resolve).not.toHaveBeenCalled();
    expect(bridge.read).not.toHaveBeenCalled();
    view.unmount();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:workspace-image-1");
    await waitFor(() =>
      expect(
        view.client
          .getQueryCache()
          .findAll({ queryKey: ["workspace-markdown-data-image"] }),
      ).toHaveLength(0),
    );
  },
);

it("releases decoded image resources when the document is hidden", async () => {
  const view = setup(
    <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />,
  );
  await waitFor(() =>
    expect(view.container.querySelector("img")).not.toBeNull(),
  );
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  fireEvent(document, new Event("visibilitychange"));
  await waitFor(() => expect(revokeObjectURL).toHaveBeenCalledTimes(1));
  expect(view.container.querySelector("img")).toBeNull();
  view.unmount();
});

it("refreshes a still-mounted image through the shared exact-file observation", async () => {
  const view = setup(
    <WorkspaceMarkdownImage src="./picture.png" alt="示意图" />,
  );
  await waitFor(() =>
    expect(view.container.querySelector("img")).toHaveAttribute(
      "src",
      "blob:workspace-image-1",
    ),
  );
  const unrelated = [
    "workspace-markdown-image",
    "chat-1",
    "/project",
    "other.png",
  ];
  view.client.setQueryData(unrelated, "unrelated");
  const subscription = bridge.observe.mock.calls[0];
  expect(subscription?.[1]).toBe("docs/picture.png");
  expect(subscription?.[4]).toBe("/project");
  expect(subscription?.[5]).toBe(false);
  bridge.read.mockResolvedValue({
    ...file,
    version: "2",
    bytes: new Uint8Array([137, 80, 78, 72]),
  });
  act(() =>
    subscription?.[2]({
      subscriptionId: "image",
      sequence: 1,
      state: "changed",
      resource: { ...metadata, version: "2" },
    }),
  );
  await waitFor(() =>
    expect(view.container.querySelector("img")).toHaveAttribute(
      "src",
      "blob:workspace-image-2",
    ),
  );
  expect(bridge.read).toHaveBeenCalledTimes(2);
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:workspace-image-1");
  expect(view.client.getQueryData(unrelated)).toBe("unrelated");
  bridge.stat.mockResolvedValue(null);
  act(() =>
    subscription?.[2]({
      subscriptionId: "image",
      sequence: 2,
      state: "changed",
      resource: null,
    }),
  );
  await screen.findByText("图片无法载入");
  expect(view.container.querySelector("img")).toBeNull();
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:workspace-image-2");
  view.unmount();
  expect(subscription?.[3]?.aborted).toBe(true);
});

it("does not render a data URL without alt as its placeholder or fetch an invalid marker", async () => {
  const source = "data:image/png;base64,iVBORw==";
  const view = setup(<WorkspaceMarkdownImage src={source} />);
  await screen.findByText("远程图片未加载");
  expect(view.container.textContent).not.toContain(source);
  view.rerender(
    <WorkspaceMarkdownImage
      src="https://ally.invalid/__workbench_markdown_image__"
      allowRemote
    />,
  );
  await screen.findByText("不支持此图片格式");
  expect(view.container.querySelector("img")).toBeNull();
  expect(bridge.read).not.toHaveBeenCalled();
  view.unmount();
});
