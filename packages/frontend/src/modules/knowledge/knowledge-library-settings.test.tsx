import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { toApiError } from "@/shared/api/errors";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgeLibrarySettings } from "./knowledge-library-settings";
import type { KnowledgeEditorController } from "./knowledge-editor-controller";
import { emptyLibrary } from "./knowledge-model";
import { resourceKey } from "./knowledge-transfer";
import { libraryExecutionDefaultsOptions } from "./knowledge-execution-defaults";

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);
vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: ({ value }: { value: object }) => ({
    selection: {
      runtimeId: "codex",
      accountId: "system",
      model: "model-a",
      reasoning: "",
      ...value,
    },
    runtimes: [{ id: "codex", name: "Codex" }],
    accounts: [],
    models: [
      { id: "model-a", name: "模型 A" },
      { id: "model-b", name: "模型 B" },
    ],
    reasoningOptions: [],
    runtime: { id: "codex" },
    supportedPermissions: new Set(["default"]),
    loading: false,
    valid: true,
    error: null,
    nativeModelsQuery: { isFetching: false },
    thinkingQuery: { isFetching: false },
  }),
}));
const server = setupServer();
let errorInterceptor: number;
beforeAll(() => {
  apiClient.setConfig({ baseUrl: "http://ally.test" });
  errorInterceptor = apiClient.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  apiClient.interceptors.error.eject(errorInterceptor);
});
const library = emptyLibrary("kb", "原名称", "general");
const profile = {
  libraryId: "kb",
  version: 3,
  wakerId: "waker",
  runtimeId: "codex",
  accountId: "system",
  model: "model-a",
  reasoning: "",
};
function fixtures(total = 0) {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/embedding", () =>
      HttpResponse.json({
        libraryId: "kb",
        version: 0,
        libraryVersion: 1,
        baseUrl: "",
        model: "",
        hasApiKey: false,
      }),
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      () => HttpResponse.json(profile),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({ items: [], total }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/activity", () =>
      HttpResponse.json({ data: [], nextCursor: 0 }),
    ),
  );
}
function harness(
  controllers = new Map<string, KnowledgeEditorController>(),
  close = vi.fn(),
  initialLibrary = library,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (value = initialLibrary) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <KnowledgeLibrarySettings
          library={value}
          controllers={controllers}
          close={close}
        />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const rendered = render(view());
  return { controllers, close, client, view, ...rendered };
}

it("数据未返回时先显示侧栏，右侧显示骨架并禁止保存", async () => {
  fixtures();
  let release!: () => void;
  let requested = false;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async () => {
        requested = true;
        await pending;
        return HttpResponse.json(profile);
      },
    ),
  );
  harness();
  try {
    for (const name of ["基础信息", "模型配置", "分块设置", "活动记录"]) {
      expect(screen.getByRole("button", { name })).toBeVisible();
    }
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
    expect(
      screen.getByRole("status").querySelector('[aria-hidden="true"]'),
    ).not.toBeNull();
    expect(screen.getByRole("button", { name: "保存并关闭" })).toBeDisabled();
    expect(
      screen.queryByRole("textbox", { name: "名称" }),
    ).not.toBeInTheDocument();
    await waitFor(() => expect(requested).toBe(true), { timeout: 5_000 });
    expect(screen.getByRole("button", { name: "模型配置" })).toBeVisible();
    expect(screen.getByRole("status", { busy: true })).toBeVisible();
  } finally {
    release();
  }
  await screen.findByRole("textbox", { name: "名称" }, { timeout: 5_000 });
  expect(screen.queryByRole("status", { busy: true })).not.toBeInTheDocument();
});

it("设置加载前后保留同一个弹窗节点，不卸载再打开", async () => {
  fixtures();
  harness();
  const initialDialog = screen.getByRole("dialog", { name: "知识库设置" });
  await screen.findByRole("textbox", { name: "名称" }, { timeout: 5_000 });
  expect(screen.getByRole("dialog", { name: "知识库设置" })).toBe(
    initialDialog,
  );
});

it("设置使用真实defaults与activity，跨分区保留草稿并按各自版本保存", async () => {
  fixtures();
  const user = userEvent.setup();
  const core: Record<string, unknown>[] = [];
  const defaults: Record<string, unknown>[] = [];
  const activity: string[] = [];
  server.use(
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        defaults.push(body);
        return HttpResponse.json({ ...profile, ...body, version: 4 });
      },
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb",
      async ({ request }) => {
        core.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ resourceId: "kb", version: 2 });
      },
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/activity",
      ({ request }) => {
        activity.push(request.url);
        return HttpResponse.json({
          data: [
            {
              id: 9,
              action: "knowledge.created",
              targetType: "knowledge",
              actorUserId: "local:501",
              outcome: "success",
              details: { title: "真实原件.md" },
              createdAt: "2026-10-20T07:00:00Z",
            },
          ],
          nextCursor: 0,
        });
      },
    ),
  );
  const state = harness();
  const name = await screen.findByRole("textbox", { name: "名称" });
  await user.clear(name);
  await user.type(name, "保留草稿");
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  fireEvent.keyDown(screen.getByRole("combobox", { name: "模型" }), {
    key: "ArrowDown",
  });
  await user.click(screen.getByRole("option", { name: "模型 B" }));
  await user.click(screen.getByRole("button", { name: "活动记录" }));
  expect(await screen.findByText("真实原件.md")).toBeVisible();
  expect(screen.getByText("本机用户")).toBeVisible();
  expect(core).toHaveLength(0);
  expect(defaults).toHaveLength(0);
  fireEvent.pointerDown(screen.getByRole("button", { name: "筛选结果" }), {
    button: 0,
    ctrlKey: false,
  });
  await user.click(
    await screen.findByRole("menuitemradio", { name: "已接纳" }),
  );
  await waitFor(() =>
    expect(
      activity.some(
        (url) => new URL(url).searchParams.get("outcome") === "accepted",
      ),
    ).toBe(true),
  );
  await user.click(screen.getByRole("button", { name: "基础信息" }));
  expect(name).toHaveValue("保留草稿");
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(state.close).toHaveBeenCalledOnce());
  expect(defaults[0]).toMatchObject({ expectedVersion: 3, model: "model-b" });
  expect(core[0]).toMatchObject({
    expectedVersion: 1,
    name: "保留草稿",
    indexingStrategy: {
      wikiEnabled: true,
      vectorEnabled: false,
      keywordEnabled: false,
    },
  });
});

it("core保存未知后关闭重开沿原请求恢复，再保存执行默认", async () => {
  fixtures();
  const user = userEvent.setup();
  const core: Record<string, unknown>[] = [];
  let defaultWrites = 0;
  server.use(
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async ({ request }) => {
        defaultWrites++;
        return HttpResponse.json({
          ...profile,
          ...((await request.json()) as object),
          version: 4,
        });
      },
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb",
      async ({ request }) => {
        core.push((await request.json()) as Record<string, unknown>);
        return core.length === 1
          ? HttpResponse.json(
              { message: "库保存待核验", code: "Unknown" },
              { status: 500 },
            )
          : HttpResponse.json({ version: 2 });
      },
    ),
  );
  const state = harness();
  await user.type(await screen.findByRole("textbox", { name: "名称" }), "修改");
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  fireEvent.keyDown(screen.getByRole("combobox", { name: "模型" }), {
    key: "ArrowDown",
  });
  await user.click(screen.getByRole("option", { name: "模型 B" }));
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  const controller = state.controllers.get(resourceKey(library.ref));
  state.unmount();
  render(state.view());
  expect(await screen.findByRole("textbox", { name: "名称" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(core).toHaveLength(2));
  expect(core[1]).toEqual(core[0]);
  expect(defaultWrites).toBe(1);
  await waitFor(() => expect(controller?.getSnapshot().pending).toBe(false));
  const saved = controller?.getSnapshot();
  expect(saved?.saved.library.revision).toBe(2);
  expect(saved?.executionVersion).toBe(4);
});

it("事件刷新不覆盖已加载草稿，CAS冲突准确提示且允许保留编辑", async () => {
  fixtures(1);
  const user = userEvent.setup();
  server.use(
    http.put("http://ally.test/api/v1/knowledge/libraries/kb", () =>
      HttpResponse.json(
        { message: "库版本冲突", code: "VersionConflict" },
        { status: 409 },
      ),
    ),
  );
  const state = harness();
  const name = await screen.findByRole("textbox", { name: "名称" });
  expect(screen.getByRole("checkbox", { name: "RAG 检索" })).toBeDisabled();
  await user.clear(name);
  await user.type(name, "未提交草稿");
  await act(async () => {
    await state.client.invalidateQueries({
      queryKey: libraryExecutionDefaultsOptions(library.ref).queryKey,
    });
  });
  state.rerender(state.view({ ...library, name: "远端新名称", revision: 9 }));
  expect(name).toHaveValue("未提交草稿");
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(name).toBeEnabled());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(name).toHaveValue("未提交草稿");
});

it("活动首次展示最新10条，每次加载更多追加10条，刷新恢复首批", async () => {
  fixtures();
  const user = userEvent.setup();
  const requests: Array<{ after: number; limit: number }> = [];
  const entries = Array.from({ length: 25 }, (_, index) => ({
    id: 25 - index,
    targetType: "knowledge",
    action: "knowledge.created",
    outcome: "success",
    details: { title: `文档 ${25 - index}` },
  }));
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/activity",
      ({ request }) => {
        const params = new URL(request.url).searchParams;
        const after = Number(params.get("afterId"));
        const limit = Number(params.get("limit"));
        requests.push({ after, limit });
        const data = entries
          .filter((entry) => !after || entry.id < after)
          .slice(0, limit);
        return HttpResponse.json({ data, nextCursor: data.at(-1)?.id ?? 0 });
      },
    ),
  );
  harness();
  await screen.findByRole("textbox", { name: "名称" });
  await user.click(screen.getByRole("button", { name: "活动记录" }));
  await screen.findByText("文档 25");
  expect(screen.getAllByRole("row")).toHaveLength(11);
  expect(screen.queryByText("文档 15")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "加载更多记录" }));
  await screen.findByText("文档 15");
  expect(screen.getAllByRole("row")).toHaveLength(21);
  expect(screen.getByText("文档 25")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "加载更多记录" }));
  await screen.findByText("文档 1");
  expect(screen.getAllByRole("row")).toHaveLength(26);
  expect(requests).toEqual([
    { after: 0, limit: 10 },
    { after: 16, limit: 10 },
    { after: 6, limit: 10 },
  ]);
  await user.click(screen.getByRole("button", { name: "刷新活动记录" }));
  await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(11));
  expect(requests.at(-1)).toEqual({ after: 0, limit: 10 });
  expect(screen.queryByText("文档 15")).not.toBeInTheDocument();
});

it("活动游标加载和刷新读取真实条目，任务与Wiki对象不泄露内部类型", async () => {
  fixtures();
  const user = userEvent.setup();
  const cursors: string[] = [];
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/activity",
      ({ request }) => {
        const after = new URL(request.url).searchParams.get("afterId") || "";
        cursors.push(after);
        return HttpResponse.json(
          after
            ? {
                data: [
                  {
                    id: 29,
                    targetType: "wiki",
                    action: "wiki.content_changed",
                    outcome: "success",
                    details: { total: 3, trigger: "system" },
                  },
                ],
                nextCursor: 0,
              }
            : {
                data: [
                  {
                    id: 30,
                    targetType: "knowledge_task",
                    targetId: "do-not-show-full-id",
                    action: "knowledge.reparse_started",
                    outcome: "accepted",
                    details: { count: 2 },
                  },
                ],
                nextCursor: 30,
              },
        );
      },
    ),
  );
  harness();
  await screen.findByRole("textbox", { name: "名称" });
  await user.click(screen.getByRole("button", { name: "活动记录" }));
  expect(await screen.findByText("整理任务")).toBeVisible();
  expect(screen.getByText("数量 2")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "加载更多记录" }));
  expect(await screen.findByText("Wiki", { selector: "span" })).toBeVisible();
  expect(screen.getByText("总数 3")).toBeVisible();
  expect(screen.getByText("系统")).toBeVisible();
  expect(cursors).toEqual(["", "30"]);
  await user.click(screen.getByRole("button", { name: "刷新活动记录" }));
  await waitFor(() => expect(cursors).toEqual(["", "30", ""]));
  expect(screen.queryByText("总数 3")).not.toBeInTheDocument();
  expect(screen.queryByText("do-not-show-full-id")).not.toBeInTheDocument();
});

it("非空库补独立服务只锁已有模型，空密钥保留且用返回libraryVersion作为真实基线", async () => {
  fixtures(1);
  const user = userEvent.setup();
  const writes: Record<string, unknown>[] = [];
  const ownLibrary = { ...library, embeddingModelId: "own-embed" };
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/embedding", () =>
      HttpResponse.json({
        libraryId: "kb",
        version: 2,
        libraryVersion: 1,
        baseUrl: "https://old.example/v1",
        model: "own-embed",
        hasApiKey: true,
      }),
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/embedding",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        writes.push(body);
        return HttpResponse.json({
          ...body,
          libraryId: "kb",
          version: 3,
          libraryVersion: 2,
          hasApiKey: true,
        });
      },
    ),
  );
  const state = harness(new Map(), vi.fn(), ownLibrary);
  await screen.findByRole("textbox", { name: "名称" });
  const controller = state.controllers.get(resourceKey(library.ref));
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  expect(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
  ).toBeDisabled();
  expect(screen.getByLabelText("API Key")).toHaveValue("");
  const url = screen.getByRole("textbox", { name: "服务 URL" });
  await user.clear(url);
  await user.type(url, "https://new.example/v1");
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(state.close).toHaveBeenCalledOnce());
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    expectedVersion: 2,
    expectedLibraryVersion: 1,
    baseUrl: "https://new.example/v1",
    model: "own-embed",
  });
  expect(writes[0]).not.toHaveProperty("apiKey");
  expect(controller?.getSnapshot().saved.library.revision).toBe(2);
  expect(controller?.getSnapshot().embeddingVersion).toBe(3);
});
it("独立Embedding已成功而defaults未知时，重开不重发密钥并保留各真实回执", async () => {
  fixtures();
  const user = userEvent.setup();
  let embeddingWrites = 0;
  let coreWrites = 0;
  const defaults: Record<string, unknown>[] = [];
  server.use(
    http.put("http://ally.test/api/v1/knowledge/libraries/kb", () => {
      coreWrites++;
      return HttpResponse.json({ version: 2 });
    }),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/embedding",
      async ({ request }) => {
        embeddingWrites++;
        const body = (await request.json()) as Record<string, unknown>;
        expect(body).toMatchObject({
          expectedLibraryVersion: 2,
          expectedVersion: 0,
          apiKey: "private-key",
        });
        return HttpResponse.json({
          libraryId: "kb",
          version: 1,
          libraryVersion: 3,
          baseUrl: "https://own.example/v1",
          model: "own-embed",
          hasApiKey: true,
        });
      },
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async ({ request }) => {
        defaults.push((await request.json()) as Record<string, unknown>);
        return defaults.length === 1
          ? HttpResponse.json(
              { message: "执行默认回执未知", code: "Unknown" },
              { status: 500 },
            )
          : HttpResponse.json({ ...profile, ...defaults[1], version: 4 });
      },
    ),
  );
  const state = harness();
  await screen.findByRole("textbox", { name: "名称" });
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  await user.type(
    screen.getByRole("textbox", { name: "服务 URL" }),
    "https://own.example/v1",
  );
  await user.type(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
    "own-embed",
  );
  await user.type(screen.getByLabelText("API Key"), "private-key");
  fireEvent.keyDown(screen.getByRole("combobox", { name: "模型" }), {
    key: "ArrowDown",
  });
  await user.click(screen.getByRole("option", { name: "模型 B" }));
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  const controller = state.controllers.get(resourceKey(library.ref));
  await user.click(screen.getByRole("button", { name: "取消" }));
  state.unmount();
  render(state.view());
  await user.click(await screen.findByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(defaults).toHaveLength(2));
  await waitFor(() => expect(controller?.getSnapshot().pending).toBe(false));
  expect(defaults[1]).toEqual(defaults[0]);
  expect(coreWrites).toBe(1);
  expect(embeddingWrites).toBe(1);
  expect(controller?.getSnapshot().draft.embedding.apiKey).toBe("");
  expect(controller?.getSnapshot().saved.library.revision).toBe(3);
});

it("自有远端设置只读写远端核心配置，不读取本机模型或Embedding", async () => {
  const user = userEvent.setup();
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  const writes: unknown[] = [];
  server.use(
    http.get(`${remote}/sources`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(`${remote}/capabilities`, () =>
      HttpResponse.json({
        operations: { write: true },
        details: {
          write: { supported: true, permission: "allowed", condition: "ready" },
        },
      }),
    ),
    http.put(remote, async ({ request }) => {
      writes.push(await request.json());
      return HttpResponse.json({ version: 4 });
    }),
  );
  const remoteLibrary = {
    ...library,
    storage: "remote" as const,
    revision: 3,
    ref: { backend: "custom", connectionId: "remote", knowledgeBaseId: "kb" },
  };
  const { close } = harness(new Map(), vi.fn(), remoteLibrary);
  const field = await screen.findByRole("textbox", { name: "名称" });
  await user.clear(field);
  await user.type(field, "远端改名");
  await user.click(screen.getByRole("button", { name: "保存并关闭" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({ name: "远端改名", expectedVersion: 3 });
  expect(close).toHaveBeenCalled();
});
