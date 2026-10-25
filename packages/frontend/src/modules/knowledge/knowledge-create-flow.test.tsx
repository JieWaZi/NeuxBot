import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, afterAll, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { CreateLibrary } from "./knowledge-library-dialogs";
import { KnowledgeUploadDialog as UploadSources } from "./knowledge-upload-dialog";
import { ApiError } from "@/shared/api/errors";
import { readRoute } from "./knowledge-model";

it("新建使用同一侧栏编辑器，分区切换保留基础、模型与分块草稿", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={vi.fn()} />
    </QueryClientProvider>,
  );
  const name = await screen.findByRole(
    "textbox",
    { name: "名称" },
    { timeout: 5000 },
  );
  const nav = screen.getByRole("navigation", { name: "知识库配置" });
  expect(nav).toBeVisible();
  await user.type(name, "统一编辑草稿");
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  const size = screen.getByRole("slider", { name: "分块大小" });
  size.focus();
  await user.keyboard("{End}");
  await user.click(screen.getByRole("button", { name: "基础信息" }));
  expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue(
    "统一编辑草稿",
  );
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  expect(screen.getByRole("slider", { name: "分块大小" })).toHaveAttribute(
    "aria-valuenow",
    "4000",
  );
  expect(
    screen.queryByRole("button", { name: "活动记录" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByLabelText("知识库 ID")).not.toBeInTheDocument();
});

const server = setupServer(
  http.get("http://ally.test/api/v1/runtimes", () =>
    HttpResponse.json({
      resources: [
        {
          id: "codex",
          name: "Codex",
          enabled: true,
          state: "ready",
          models: [
            { id: "model", name: "Model", permissionLevels: ["default"] },
          ],
          defaultModel: "model",
        },
      ],
    }),
  ),
  http.get("http://ally.test/api/v1/ai-accounts", () =>
    HttpResponse.json({ resources: [] }),
  ),
);
beforeAll(() => {
  apiClient.setConfig({ baseUrl: "http://ally.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

it.each(["relations", "history", "source", "project", "chat"])(
  "旧 %s 链接回到当前库知识目录",
  (view) => {
    expect(readRoute(new URLSearchParams({ view, library: "kb" })).view).toBe(
      "wiki",
    );
  },
);

it("新建默认通用库且不再提供知识类型选择", async () => {
  const create = vi.fn();
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={create} />
    </QueryClientProvider>,
  );
  expect(screen.queryByRole("radio", { name: "项目" })).not.toBeInTheDocument();
  await user.type(
    await screen.findByRole("textbox", { name: "名称" }),
    "资料库",
  );
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  expect(create).toHaveBeenCalledWith(
    expect.objectContaining({ kind: "general" }),
    expect.objectContaining({ runtimeId: "codex", model: "model" }),
    expect.any(String),
    expect.objectContaining({ submit: expect.any(Function) }),
  );
});

it("部分上传失败逐文件保留结果，重试不重传已成功原件", async () => {
  const upload = vi
    .fn()
    .mockResolvedValueOnce(undefined)
    .mockRejectedValueOnce(new ApiError("第二份失败"))
    .mockResolvedValueOnce(undefined);
  const user = userEvent.setup();
  render(<UploadSources opened close={() => {}} upload={upload} />);
  const input = await screen.findByLabelText<HTMLInputElement>("选择文件");
  await user.upload(input, [
    new File(["a"], "第一份.md"),
    new File(["b"], "第二份.md"),
  ]);
  await user.click(screen.getByRole("button", { name: /上传文档/ }));
  expect(await screen.findByText("上传失败")).toBeVisible();
  expect(screen.queryByText("第二份失败")).not.toBeInTheDocument();
  expect(screen.getByText("待整理")).toBeVisible();
  expect(upload).toHaveBeenCalledTimes(2);
  await user.click(screen.getByRole("button", { name: /重试失败文档/ }));
  await waitFor(() => expect(upload).toHaveBeenCalledTimes(3));
  expect(upload.mock.calls[2]?.[0].name).toBe("第二份.md");
});

it("关闭页面会停止当前上传并且不会继续发送后续文件", async () => {
  let signal: AbortSignal | undefined;
  let finish: (() => void) | undefined;
  const upload = vi.fn(
    (_file: File, _requestId: string, currentSignal: AbortSignal) => {
      signal = currentSignal;
      return new Promise<void>((resolve) => {
        finish = resolve;
      });
    },
  );
  const user = userEvent.setup();
  const view = render(
    <UploadSources opened close={() => {}} upload={upload} />,
  );
  await user.upload(
    await screen.findByLabelText<HTMLInputElement>("选择文件"),
    [new File(["a"], "原件.md"), new File(["b"], "后续.md")],
  );
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  await waitFor(() => expect(upload).toHaveBeenCalledTimes(1));
  view.unmount();
  expect(signal?.aborted).toBe(true);
  finish?.();
  await waitFor(() => expect(upload).toHaveBeenCalledTimes(1));
});

it("同一批重复选择相同文件时只排队一次", async () => {
  const upload = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup();
  render(<UploadSources opened close={() => {}} upload={upload} />);
  const file = new File(["same"], "重复.md", { lastModified: 1 });
  await user.upload(
    await screen.findByLabelText<HTMLInputElement>("选择文件"),
    [file, file],
  );
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  await waitFor(() => expect(screen.getAllByText("待整理")).toHaveLength(1));
  expect(upload).toHaveBeenCalledTimes(1);
});

it("新建知识库显式携带WeKnora分块设置并保留用户修改", async () => {
  const create = vi.fn();
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={create} />
    </QueryClientProvider>,
  );
  await user.type(
    await screen.findByRole("textbox", { name: "名称" }),
    "分块资料库",
  );
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  expect(screen.getByRole("slider", { name: "分块大小" })).toHaveAttribute(
    "aria-valuenow",
    "2048",
  );
  expect(screen.getByRole("slider", { name: "重叠大小" })).toHaveAttribute(
    "aria-valuenow",
    "0",
  );
  const size = screen.getByRole("slider", { name: "分块大小" });
  size.focus();
  await user.keyboard("{End}");
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  expect(create).toHaveBeenCalledWith(
    expect.objectContaining({
      chunkingConfig: expect.objectContaining({
        chunk_size: 4000,
        chunk_overlap: 0,
        strategy: "auto",
        parent_chunk_size: 4096,
        child_chunk_size: 384,
        token_limit: 0,
        languages: [],
        separators: ["\n\n", "\n", "。", "！", "？", ";", "；"],
      }),
    }),
    expect.objectContaining({ runtimeId: "codex" }),
    expect.any(String),
    expect.objectContaining({ submit: expect.any(Function) }),
  );
});

it("新建切换Wiki-only时应用上游分块默认，切回普通索引时恢复", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={vi.fn()} />
    </QueryClientProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  expect(screen.getByRole("slider", { name: "分块大小" })).toHaveAttribute(
    "aria-valuenow",
    "2048",
  );
  expect(screen.getByRole("slider", { name: "重叠大小" })).toHaveAttribute(
    "aria-valuenow",
    "0",
  );
  expect(screen.getByRole("switch", { name: "父子分块" })).not.toBeChecked();
  await user.click(screen.getByRole("button", { name: "基础信息" }));
  await user.click(screen.getByRole("checkbox", { name: "RAG 检索" }));
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  expect(screen.getByRole("slider", { name: "分块大小" })).toHaveAttribute(
    "aria-valuenow",
    "512",
  );
  expect(screen.getByRole("slider", { name: "重叠大小" })).toHaveAttribute(
    "aria-valuenow",
    "80",
  );
  expect(screen.getByRole("switch", { name: "父子分块" })).toBeChecked();
});

it("手动改过分块后切索引不会覆盖用户配置", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={vi.fn()} />
    </QueryClientProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  const size = screen.getByRole("slider", { name: "分块大小" });
  size.focus();
  await user.keyboard("{End}");
  await user.click(screen.getByRole("button", { name: "基础信息" }));
  await user.click(screen.getByRole("checkbox", { name: "RAG 检索" }));
  await user.click(await screen.findByRole("button", { name: "分块设置" }));
  expect(size).toHaveAttribute("aria-valuenow", "4000");
  expect(screen.getByRole("slider", { name: "重叠大小" })).toHaveAttribute(
    "aria-valuenow",
    "0",
  );
  expect(screen.getByRole("switch", { name: "父子分块" })).not.toBeChecked();
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

it("创建入口仅新建本地知识库，不提供保存位置或读取远端连接", async () => {
  const user = userEvent.setup();
  const create = vi.fn();
  const listConnections = vi.fn(() => HttpResponse.json({ items: [] }));
  server.use(
    http.get("http://ally.test/api/v1/knowledge/connections", listConnections),
  );
  render(
    <QueryClientProvider client={client()}>
      <CreateLibrary opened close={() => {}} create={create} />
    </QueryClientProvider>,
  );
  await user.type(
    await screen.findByRole("textbox", { name: "名称" }),
    "本地新库",
  );
  expect(
    screen.queryByRole("combobox", { name: "保存位置" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByText("选择保存知识内容的本地位置或已配置的远端连接。"),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  expect(create).toHaveBeenCalledWith(
    expect.objectContaining({
      storage: "local",
      ref: { backend: "local", connectionId: "local", knowledgeBaseId: "" },
    }),
    expect.any(Object),
    expect.any(String),
    expect.any(Object),
  );
  expect(listConnections).not.toHaveBeenCalled();
});
