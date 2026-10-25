import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { initializeApi } from "@/shared/api/runtime";
import {
  dismissErrorNotice,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { ALLY_LOCAL_ERROR_SURFACE } from "@/shared/api/local-error-surface";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { LibraryList } from "./knowledge-library";
import {
  knowledgeKeys,
  librariesOptions,
  toLibrary,
} from "./knowledge-queries";
import { knowledgeAdapter, type ResourceRef } from "@/shared/api/knowledge";
import { wikiPagesOptions } from "./knowledge-wiki-queries";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import { useKnowledgeWorkspaceQueries } from "./knowledge-workspace-queries";
import { emptyLibrary } from "./knowledge-model";
import { libraryExecutionDefaultsOptions } from "./knowledge-execution-defaults";
import { libraryEmbeddingProfileOptions } from "./knowledge-embedding-profile";
import {
  createSourceDeleter,
  KnowledgeSourceDelete,
} from "./knowledge-source-delete";
import { continueSourceCleanup } from "./knowledge-source-cleanup";
import {
  createKnowledgeOrganizeStart,
  createKnowledgeOrganizeIntent,
} from "./knowledge-organize-start";
import { KnowledgeLibraryActivity } from "./knowledge-library-activity";
import { KnowledgeDocumentReader } from "./knowledge-document-reader";
import { KnowledgeWikiEditor } from "./knowledge-wiki-editor";
import { KnowledgeWikiDelete } from "./knowledge-wiki-delete";
import { KnowledgeWikiRestore } from "./knowledge-wiki-restore";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";

vi.mock("@/shared/platform", () => ({
  saveBlob: async () => true,
  getCoreConnection: async () => ({
    baseUrl: "http://127.0.0.1:19876",
    token: "test-token",
  }),
}));
const server = setupServer();
beforeAll(async () => {
  server.listen({ onUnhandledRequest: "error" });
  await initializeApi();
});
afterEach(() => {
  server.resetHandlers();
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => {
    for (const notice of result.current) dismissErrorNotice(notice.id);
  });
  unmount();
});
afterAll(() => server.close());

function Directory() {
  const query = useQuery(librariesOptions(0));
  const notices = useErrorNotices();
  return (
    <>
      <output aria-label="全局错误数">{notices.length}</output>
      <LibraryList
        libraries={(query.data?.items ?? []).map(toLibrary)}
        open={() => {}}
        create={() => {}}
        state={query.error ? "error" : query.isPending ? "loading" : undefined}
        error={query.error}
        onRetry={() => void query.refetch()}
      />
    </>
  );
}

it.each([false, true])(
  "目录失败统一全局反馈并可恢复，存在缓存=%s",
  async (cached) => {
    let failed = true;
    let leakedSurfaceHeader = false;
    const library = { id: "cached", name: "缓存知识库", kind: "general" };
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/knowledge/connections/libraries",
        ({ request }) => {
          leakedSurfaceHeader ||= request.headers.has(ALLY_LOCAL_ERROR_SURFACE);
          return failed
            ? HttpResponse.json(
                { key: "KnowledgeForbidden", message: "目录暂不可读取" },
                { status: 403 },
              )
            : HttpResponse.json({
                items: [{ ...library, name: "已恢复知识库" }],
                total: 1,
              });
        },
      ),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    if (cached)
      client.setQueryData(librariesOptions(0).queryKey, {
        items: [library],
        total: 1,
      });
    const view = render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <Directory />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    try {
      await waitFor(() =>
        expect(screen.getByLabelText("全局错误数")).toHaveTextContent("1"),
      );
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      if (cached)
        expect(
          screen.getByRole("heading", { name: "缓存知识库" }),
        ).toBeVisible();
      expect(screen.getByRole("searchbox")).toBeEnabled();
      expect(
        screen.queryByRole("button", { name: "重试" }),
      ).not.toBeInTheDocument();
      failed = false;
      await act(async () => {
        await client.refetchQueries();
      });
      await screen.findByRole("heading", { name: "已恢复知识库" });
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(screen.getByLabelText("全局错误数")).toHaveTextContent("0");
      expect(leakedSurfaceHeader).toBe(false);
    } finally {
      view.unmount();
      client.clear();
    }
  },
);

function WikiDirectory({ resource }: { resource: ResourceRef }) {
  const query = useQuery(wikiPagesOptions(resource));
  const notices = useErrorNotices();
  return (
    <>
      <output aria-label="全局错误数">{notices.length}</output>
      <KnowledgeReadFeedback
        hasData={Boolean(query.data)}
        state={{
          pending: query.isPending,
          error: query.error,
          retry: () => void query.refetch(),
        }}
      />
      {query.data?.map((page) => (
        <p key={page.id}>{page.title}</p>
      ))}
    </>
  );
}

const wikiTargets = [
  {
    label: "本地",
    resource: {
      backend: "local",
      connectionId: "local",
      knowledgeBaseId: "kb",
    },
    root: "http://127.0.0.1:19876/api/v1/knowledge/libraries/kb",
  },
  {
    label: "远端",
    resource: {
      backend: "custom",
      connectionId: "remote",
      knowledgeBaseId: "kb",
    },
    root: "http://127.0.0.1:19876/api/v1/knowledge/connections/remote/libraries/kb",
  },
];

it.each(wikiTargets)(
  "$label Wiki 读取统一全局反馈且不将标记发送到HTTP",
  async ({ resource, root }) => {
    let failed = true;
    let leakedSurfaceHeader = false;
    server.use(
      http.get(`${root}/page-directory`, ({ request }) => {
        leakedSurfaceHeader ||= request.headers.has(ALLY_LOCAL_ERROR_SURFACE);
        return failed
          ? HttpResponse.json(
              { key: "KnowledgeForbidden", message: "Wiki暂不可读取" },
              { status: 403 },
            )
          : HttpResponse.json({
              items: [{ id: "one", title: "已恢复Wiki" }],
              total: 1,
            });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={client}>
        <WikiDirectory resource={resource} />
      </QueryClientProvider>,
    );
    try {
      await waitFor(() =>
        expect(screen.getByLabelText("全局错误数")).toHaveTextContent("1"),
      );
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(screen.getByLabelText("全局错误数")).toHaveTextContent("1");
      expect(leakedSurfaceHeader).toBe(false);
      failed = false;
      await act(async () => {
        await client.refetchQueries();
      });
      await screen.findByText("已恢复Wiki");
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(screen.getByLabelText("全局错误数")).toHaveTextContent("0");
    } finally {
      view.unmount();
      client.clear();
    }
  },
);

it.each(wikiTargets)(
  "$label 未声明就地反馈的Adapter读取仍生成全局错误通知",
  async ({ resource, root }) => {
    let leakedSurfaceHeader = false;
    server.use(
      http.get(`${root}/pages/one`, ({ request }) => {
        leakedSurfaceHeader ||= request.headers.has(ALLY_LOCAL_ERROR_SURFACE);
        return HttpResponse.json(
          { key: "KnowledgeForbidden", message: "未标记读取失败" },
          { status: 403 },
        );
      }),
    );
    const { result, unmount } = renderHook(useErrorNotices);
    try {
      await act(async () => {
        await expect(
          knowledgeAdapter(resource).getPage({
            path: { libraryId: "kb", pageId: "one" },
            throwOnError: true,
          }),
        ).rejects.toMatchObject({ code: "KnowledgeForbidden" });
      });
      expect(result.current).toHaveLength(1);
      expect(result.current[0]?.message).toBe("未标记读取失败");
      expect(leakedSurfaceHeader).toBe(false);
    } finally {
      unmount();
    }
  },
);

function WorkspaceJobs() {
  const { jobs } = useKnowledgeWorkspaceQueries({
    ref: { backend: "local", connectionId: "local", knowledgeBaseId: "kb" },
    detail: true,
    active: true,
    listOffset: 0,
    sourceFilters: {},
    location: "all",
  });
  const notices = useErrorNotices();
  return (
    <>
      <output aria-label="任务摘要读取">
        {jobs.isError ? "失败" : "读取中"}
      </output>
      <output aria-label="全局错误数">{notices.length}</output>
    </>
  );
}

it("任务摘要没有区域恢复入口时仍保留全局错误提示", async () => {
  const root = "http://127.0.0.1:19876/api/v1/knowledge/libraries/kb";
  server.use(
    http.get(root, () => HttpResponse.json({ id: "kb", name: "知识库" })),
    http.get(`${root}/sources`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(`${root}/jobs`, () =>
      HttpResponse.json(
        { key: "KnowledgeForbidden", message: "任务摘要读取失败" },
        { status: 403 },
      ),
    ),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <WorkspaceJobs />
    </QueryClientProvider>,
  );
  try {
    await waitFor(() =>
      expect(screen.getByLabelText("任务摘要读取")).toHaveTextContent("失败"),
    );
    expect(screen.getByLabelText("全局错误数")).toHaveTextContent("1");
  } finally {
    view.unmount();
    client.clear();
  }
});

const localLibrary = emptyLibrary("kb", "知识库", "general");
const operationRoot = "http://127.0.0.1:19876/api/v1/knowledge/libraries/kb";
const globalOperations: {
  label: string;
  failurePath: string;
  run: (client: QueryClient) => Promise<unknown>;
}[] = [
  {
    label: "执行偏好读取",
    failurePath: "/execution-defaults",
    run: (client) =>
      client.fetchQuery(libraryExecutionDefaultsOptions(localLibrary.ref)),
  },
  {
    label: "嵌入配置读取",
    failurePath: "/embedding",
    run: (client) =>
      client.fetchQuery(libraryEmbeddingProfileOptions(localLibrary.ref)),
  },
  {
    label: "原件删除接纳",
    failurePath: "/source-purges/confirm",
    run: () =>
      createSourceDeleter().perform(localLibrary, "version", {
        sourceId: "source",
        digest: "digest",
      }),
  },
  {
    label: "清理状态核对",
    failurePath: "/source-purges/purge",
    run: () => continueSourceCleanup(localLibrary, "purge"),
  },
  {
    label: "清理权限版本核对",
    failurePath: "",
    run: () => continueSourceCleanup(localLibrary, "purge"),
  },
  {
    label: "清理恢复写入",
    failurePath: "/source-purges/purge/recover",
    run: () => continueSourceCleanup(localLibrary, "purge"),
  },
  {
    label: "整理接纳",
    failurePath: "/organize",
    run: () =>
      createKnowledgeOrganizeStart({
        library: localLibrary,
        batch: ["version"],
        selection: {},
        intent: createKnowledgeOrganizeIntent(),
        message: "整理资料",
        perform: async (_key, action) => {
          await action("request");
        },
      }).execute(),
  },
];

it.each(globalOperations)(
  "$label 没有区域错误消费时保留原全局提示",
  async ({ failurePath, run }) => {
    let leakedSurfaceHeader = false;
    server.use(
      http.all("*", ({ request }) => {
        leakedSurfaceHeader ||= request.headers.has(ALLY_LOCAL_ERROR_SURFACE);
        const path = new URL(request.url).pathname;
        if (path === new URL(operationRoot + failurePath).pathname)
          return HttpResponse.json(
            { key: "KnowledgeForbidden", message: "附属操作失败" },
            { status: 403 },
          );
        if (path.endsWith("/source-purges/purge"))
          return HttpResponse.json({
            id: "purge",
            version: 1,
            state: "pending",
          });
        if (request.url === operationRoot)
          return HttpResponse.json({ id: "kb", policyVersion: 1 });
        return new HttpResponse(null, { status: 404 });
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result, unmount } = renderHook(useErrorNotices);
    try {
      await act(async () => {
        await expect(run(queryClient)).rejects.toMatchObject({
          code: "KnowledgeForbidden",
        });
      });
      expect(result.current).toHaveLength(1);
      expect(result.current[0]?.message).toBe("附属操作失败");
      expect(leakedSurfaceHeader).toBe(false);
    } finally {
      unmount();
      queryClient.clear();
    }
  },
);

it.each(["activity", "preview", "preview-detail"])(
  "%s 无区域错误消费时保留全局提示",
  async (kind) => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    server.use(
      http.get(
        `${operationRoot}/${kind === "activity" ? "activity" : kind === "preview-detail" ? "sources/version" : "source-purges/preview"}`,
        () =>
          HttpResponse.json(
            { key: "KnowledgeForbidden", message: "附属读取失败" },
            { status: 403 },
          ),
      ),
    );
    const { result, unmount } = renderHook(useErrorNotices);
    const view = render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          {kind === "activity" ? (
            <KnowledgeLibraryActivity library={localLibrary} />
          ) : (
            <KnowledgeSourceDelete
              library={localLibrary}
              source={{
                id: "version",
                sourceId: kind === "preview-detail" ? undefined : "source",
                name: "资料.md",
                size: "1 KB",
                state: "processed",
                diagnostic: "",
                date: "",
              }}
              remove={createSourceDeleter()}
              onClose={() => {}}
              onDeleted={async () => {}}
            />
          )}
        </TooltipProvider>
      </QueryClientProvider>,
    );
    try {
      await waitFor(() =>
        expect(
          queryClient
            .getQueryCache()
            .getAll()
            .some((query) => query.state.status === "error"),
        ).toBe(true),
      );
      expect(result.current).toHaveLength(1);
      expect(result.current[0]?.message).toBe("附属读取失败");
      if (kind === "preview-detail") {
        server.use(
          http.get(`${operationRoot}/sources/version`, () =>
            HttpResponse.json({
              id: "version",
              sourceId: "source",
              filename: "资料.md",
            }),
          ),
          http.get(`${operationRoot}/source-purges/preview`, () =>
            HttpResponse.json({ sourceId: "source", digest: "digest" }),
          ),
        );
        await act(async () => {
          await queryClient.refetchQueries({
            queryKey: [
              ...knowledgeKeys.resource(localLibrary.ref),
              "source-purge-preview",
              "version",
            ],
          });
        });
        await waitFor(() => expect(result.current).toHaveLength(0));
      }
    } finally {
      view.unmount();
      unmount();
      queryClient.clear();
    }
  },
);

it.each([false, true])(
  "下载重试恢复原错误并保留后来失败=%s",
  async (laterFailure) => {
    let downloadFailed = false;
    let retryStarted = false;
    let delayRetry = false;
    let release = () => {};
    const retryGate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get(`${operationRoot}/source-purges`, () =>
        HttpResponse.json({ items: [], total: 0 }),
      ),
      http.get(`${operationRoot}/sources/version`, () =>
        HttpResponse.json({ id: "version", filename: "资料.md" }),
      ),
      http.get(`${operationRoot}/sources/version/raw`, async () => {
        if (downloadFailed)
          return HttpResponse.json(
            { key: "KnowledgeForbidden", message: "原件下载失败" },
            { status: 403 },
          );
        if (delayRetry) {
          retryStarted = true;
          await retryGate;
        }
        return HttpResponse.json({
          filename: "资料.md",
          bytes: btoa("readable content"),
        });
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result, unmount } = renderHook(useErrorNotices);
    const view = render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <KnowledgeDocumentReader
            library={localLibrary}
            source={{
              id: "version",
              name: "资料.md",
              size: "1 KB",
              state: "processed",
              diagnostic: "",
              date: "",
            }}
            onClose={() => {}}
            onRebuild={() => {}}
            onDeleted={() => {}}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    try {
      await screen.findByText("readable content");
      downloadFailed = true;
      await userEvent.click(screen.getByRole("button", { name: "下载" }));
      await waitFor(() => expect(result.current).toHaveLength(1));
      expect(result.current[0]?.message).toBe("原件下载失败");
      expect(screen.getByText("readable content")).toBeVisible();
      const original = result.current[0];
      if (!original) throw new Error("下载失败未生成可见通知");
      downloadFailed = false;
      delayRetry = true;
      await userEvent.click(screen.getByRole("button", { name: "下载" }));
      await waitFor(() => expect(retryStarted).toBe(true));
      if (laterFailure)
        act(() => reportErrorNotice({ ...original, message: "新的下载失败" }));
      const expectedVersion = result.current[0]?.version;
      release();
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "下载" })).toBeEnabled(),
      );
      if (laterFailure) {
        expect(result.current).toHaveLength(1);
        expect(result.current[0]?.message).toBe("新的下载失败");
        expect(result.current[0]?.version).toBe(expectedVersion);
      } else await waitFor(() => expect(result.current).toHaveLength(0));
    } finally {
      release();
      view.unmount();
      unmount();
      queryClient.clear();
    }
  },
);

it.each(["edit", "delete", "restore"] as const)(
  "Wiki %s HTTP 失败只提示一次且成功重试恢复",
  async (kind) => {
    let failed = true;
    const onSaved = vi.fn();
    const page = {
      id: "one",
      slug: "concept/one",
      title: "人工页面",
      content: "保留正文",
      pageType: "concept",
      version: 3,
    };
    const resource = localLibrary.ref;
    const commands: KnowledgeWikiCommands = new Map();
    const path = `${operationRoot}/pages/one${kind === "restore" ? "/restore" : ""}`;
    server.use(
      http.all(path, () =>
        failed
          ? HttpResponse.json(
              { key: "KnowledgeForbidden", message: "Wiki 写入失败" },
              { status: 403 },
            )
          : HttpResponse.json({
              resourceId: "one",
              version: 4,
              state: "saved",
            }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const common = {
      resource,
      page,
      commands,
      commandKey: `${kind}/one`,
      structureVersion: 1,
      onClose: () => {},
    };
    const { result, unmount } = renderHook(useErrorNotices);
    const view = render(
      <QueryClientProvider client={queryClient}>
        {kind === "edit" ? (
          <KnowledgeWikiEditor
            {...common}
            kind="edit"
            themes={[]}
            parentId=""
            onSaved={onSaved}
          />
        ) : kind === "delete" ? (
          <KnowledgeWikiDelete {...common} onDeleted={onSaved} />
        ) : (
          <KnowledgeWikiRestore
            {...common}
            revisionVersion={2}
            onRestored={onSaved}
          />
        )}
      </QueryClientProvider>,
    );
    const label =
      kind === "edit" ? "保存" : kind === "delete" ? "删除" : "确认恢复";
    try {
      await userEvent.click(screen.getByRole("button", { name: label }));
      await waitFor(() => expect(result.current).toHaveLength(1));
      expect(result.current[0]?.message).toBe("Wiki 写入失败");
      expect(onSaved).not.toHaveBeenCalled();
      failed = false;
      await waitFor(() =>
        expect(screen.getByRole("button", { name: label })).toBeEnabled(),
      );
      await userEvent.click(screen.getByRole("button", { name: label }));
      await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
      await waitFor(() => expect(result.current).toHaveLength(0));
    } finally {
      view.unmount();
      unmount();
      queryClient.clear();
    }
  },
);
