import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { ThemeProvider } from "@/shared/theme";
import { toApiError } from "@/shared/api/errors";
import { client } from "@/shared/api/generated/client.gen";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { KnowledgeWikiHistory } from "./knowledge-wiki-history";
import type { ResourceRef } from "./knowledge-transfer";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";

vi.mock("@/shared/ui/text-diff-view", () => ({
  TextDiffView: ({
    beforeContent,
    afterContent,
  }: {
    beforeContent: string;
    afterContent: string;
  }) => (
    <div>
      <pre aria-label="比较前">{beforeContent}</pre>
      <pre aria-label="比较后">{afterContent}</pre>
    </div>
  ),
}));
const server = setupServer();
const root = "http://ally.test/api/v1/knowledge/libraries/kb";
const resource = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
} as const;
const current = {
  id: "one",
  slug: "concept/one",
  version: 3,
  title: "当前标题",
  summary: "当前说明",
  content: "当前正文",
  pageType: "concept",
};
const revisions = [
  {
    id: "r2",
    pageId: "one",
    version: 2,
    title: "第二版标题",
    summary: "第二版说明",
    content: "第二版正文",
  },
  {
    id: "r1",
    pageId: "one",
    version: 1,
    title: "首版标题",
    summary: "首版说明",
    content: "首版正文",
  },
];
beforeAll(() => {
  client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  window.localStorage.setItem("ally.ui.theme", "light");
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => {
  server.resetHandlers();
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});
async function expectGlobalReadError() {
  const notices = renderHook(useErrorNotices);
  await waitFor(() => expect(notices.result.current.length).toBeGreaterThan(0));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  notices.unmount();
}
afterAll(() => server.close());
function show(
  options: {
    page?: typeof current;
    resource?: ResourceRef;
    commands?: KnowledgeWikiCommands;
    writable?: boolean;
  } = {},
) {
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      HttpResponse.json({ items: revisions, total: 2 }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const props = {
    resource: options.resource ?? resource,
    page: options.page ?? current,
    writable: options.writable ?? true,
    structureVersion: 8,
    commands: options.commands ?? new Map(),
    commandKey: "restore:one",
    onClose: vi.fn(),
    onRestored: vi.fn(),
    onLink: vi.fn(),
  };
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <KnowledgeWikiHistory {...props} />
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return { ...view, props, queryClient };
}
async function select(version: number) {
  await userEvent.click(
    await screen.findByRole("button", {
      name: new RegExp(`^版本 ${version}(?:[^0-9]|$)`),
    }),
  );
}
it("包含独立当前版本，默认比较最近改动，历史支持增量、累计及原文", async () => {
  show();
  await screen.findByRole("button", { name: /版本 3.*当前版本/ });
  await waitFor(() =>
    expect(
      screen.getAllByLabelText("比较前").map((node) => node.textContent),
    ).toEqual(["第二版标题", "第二版说明", "第二版正文"]),
  );
  expect(
    screen.getAllByLabelText("比较后").map((node) => node.textContent),
  ).toEqual(["当前标题", "当前说明", "当前正文"]);
  expect(
    screen.queryByRole("button", { name: "恢复此版本" }),
  ).not.toBeInTheDocument();
  await select(2);
  await waitFor(() =>
    expect(
      screen.getAllByLabelText("比较前").map((node) => node.textContent),
    ).toEqual(["首版标题", "首版说明", "首版正文"]),
  );
  await userEvent.click(screen.getByRole("radio", { name: "累计比较" }));
  await waitFor(() =>
    expect(
      screen.getAllByLabelText("比较后").map((node) => node.textContent),
    ).toEqual(["当前标题", "当前说明", "当前正文"]),
  );
  expect(
    screen.getAllByLabelText("比较前").map((node) => node.textContent),
  ).toEqual(["第二版标题", "第二版说明", "第二版正文"]);
  await userEvent.click(screen.getByRole("radio", { name: "原文" }));
  expect(screen.getByLabelText("版本原文")).toHaveTextContent(
    "第二版标题 第二版说明 第二版正文",
  );
});
it("首版以空内容为基准，缺失上一版本明确提示，不能显示伪造的版本范围", async () => {
  show({ page: { ...current, version: 1 } });
  await screen.findByText("首次创建");
  expect(
    screen.getAllByLabelText("比较前").every((node) => node.textContent === ""),
  ).toBe(true);
});
it("历史缺失只降级该比较，普通错误提供重试", async () => {
  show();
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      HttpResponse.json({ items: [revisions[0]], total: 1 }),
    ),
  );
  await select(2);
  expect(await screen.findByText(/版本 1 的快照未保留/)).toBeVisible();
  expect(screen.queryByText("版本 1 → 版本 2")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("radio", { name: "累计比较" }));
  expect(await screen.findByText("版本 2 → 版本 3")).toBeVisible();
});
it("恢复先确认可取消，提交所选历史及当前版本，成功刷新所属资源", async () => {
  const { props, queryClient } = show();
  const invalidated = vi.spyOn(queryClient, "invalidateQueries");
  const requests: unknown[] = [];
  server.use(
    http.post(`${root}/pages/one/restore`, async ({ request }) => {
      requests.push(await request.json());
      return HttpResponse.json({
        resourceId: "one",
        version: 4,
        state: "saved",
      });
    }),
  );
  await select(2);
  await userEvent.click(
    await screen.findByRole("button", { name: "恢复此版本" }),
  );
  let dialog = screen.getByRole("dialog", { name: "恢复版本" });
  expect(dialog).toHaveTextContent("版本 2");
  await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(requests).toHaveLength(0);
  await userEvent.click(screen.getByRole("button", { name: "恢复此版本" }));
  dialog = screen.getByRole("dialog", { name: "恢复版本" });
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认恢复" }),
  );
  await waitFor(() => expect(props.onRestored).toHaveBeenCalledOnce());
  expect(requests).toEqual([
    expect.objectContaining({
      requestId: expect.any(String),
      expectedVersion: 3,
      revisionVersion: 2,
      structureVersion: 8,
    }),
  ]);
  expect(invalidated).toHaveBeenCalledWith({
    queryKey: ["knowledge", "local", "local", "kb"],
  });
  expect(invalidated).toHaveBeenCalledWith({
    queryKey: ["knowledge", "libraries"],
  });
});
it("恢复结果未知保留原命令，关闭重开后即使历史行消失也用同一身份核对", async () => {
  const commands: KnowledgeWikiCommands = new Map();
  const first = show({ commands });
  const requests: unknown[] = [];
  let fail = true;
  server.use(
    http.post(`${root}/pages/one/restore`, async ({ request }) => {
      requests.push(await request.json());
      return fail
        ? new HttpResponse(null, { status: 504 })
        : HttpResponse.json({ resourceId: "one", version: 4, state: "saved" });
    }),
  );
  await select(2);
  await userEvent.click(
    await screen.findByRole("button", { name: "恢复此版本" }),
  );
  let dialog = screen.getByRole("dialog", { name: "恢复版本" });
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认恢复" }),
  );
  await waitFor(() =>
    expect(
      within(dialog).getByRole("button", { name: "确认恢复" }),
    ).toBeEnabled(),
  );
  expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  expect(commands.get("restore:one")).toMatchObject({
    kind: "restore-page",
    revisionVersion: 2,
  });
  first.unmount();
  const second = show({ commands, page: { ...current, version: 4 } });
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
  );
  dialog = await screen.findByRole("dialog", { name: "恢复版本" });
  fail = false;
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认恢复" }),
  );
  await waitFor(() => expect(second.props.onRestored).toHaveBeenCalledOnce());
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
  expect(commands.size).toBe(0);
});
it("只读页面不提供恢复", async () => {
  show({ writable: false });
  await select(2);
  expect(await screen.findByText("版本 1 → 版本 2")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "恢复此版本" }),
  ).not.toBeInTheDocument();
});

it("历史读取失败保留当前版本，全局报告并在重新读取后恢复", async () => {
  const { queryClient } = show();
  let failed = true;
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      failed
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json({ items: revisions, total: 2 }),
    ),
  );
  await expectGlobalReadError();
  expect(
    screen.getByRole("button", { name: /版本 3.*当前版本/ }),
  ).toBeVisible();
  failed = false;
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await screen.findByRole("button", { name: /^版本 2/ });
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(screen.getByText("版本 2 → 版本 3")).toBeVisible();
  expect(screen.getAllByLabelText("比较前")[0]).toHaveTextContent("第二版标题");
});

it("版本冲突保留错误但不当作未知结果，不报告恢复成功", async () => {
  const commands: KnowledgeWikiCommands = new Map();
  const { props } = show({ commands });
  server.use(
    http.post(`${root}/pages/one/restore`, () =>
      HttpResponse.json(
        {
          key: "KnowledgeVersionConflict",
          message: "页面已被更新，请刷新后重试",
        },
        { status: 409 },
      ),
    ),
  );
  await select(2);
  await userEvent.click(
    await screen.findByRole("button", { name: "恢复此版本" }),
  );
  await userEvent.click(screen.getByRole("button", { name: "确认恢复" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "确认恢复" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(props.onRestored).not.toHaveBeenCalled();
  expect(commands.size).toBe(0);
});

it("远端快照未保留时不显示恢复；切换版本后迟到响应不覆盖所选原文", async () => {
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  let release: () => void = () => undefined;
  const delayed = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(`${remote}/pages/one/revisions`, () =>
      HttpResponse.json({
        items: revisions.map((revision) => ({
          ...revision,
          content: undefined,
        })),
        total: 2,
      }),
    ),
    http.get(`${remote}/pages/one/revisions/2`, async () => {
      await delayed;
      return new HttpResponse(null, { status: 404 });
    }),
    http.get(`${remote}/pages/one/revisions/1`, () =>
      HttpResponse.json(revisions[1]),
    ),
  );
  show({
    resource: {
      backend: "weknora",
      connectionId: "remote",
      knowledgeBaseId: "kb",
    },
  });
  await select(1);
  await userEvent.click(screen.getByRole("radio", { name: "原文" }));
  expect(await screen.findByLabelText("版本原文")).toHaveTextContent(
    "首版正文",
  );
  release();
  await select(2);
  expect(
    await screen.findByText("此版本的快照未保留，无法查看或恢复。"),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "恢复此版本" }),
  ).not.toBeInTheDocument();
  await select(1);
  expect(await screen.findByLabelText("版本原文")).toHaveTextContent(
    "首版正文",
  );
});

it("AI维护修订在历史中标记实际Agent来源", async () => {
  show();
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      HttpResponse.json({
        items: [{ ...revisions[0], editSource: "agent" }],
        total: 1,
      }),
    ),
  );
  await screen.findByText("AI 修订");
});

it.each([
  { label: "上一版本", missingVersion: 2 },
  { label: "所选版本", missingVersion: 1 },
])(
  "$label 快照失败全局报告，恢复后才允许使用快照",
  async ({ missingVersion }) => {
    const remoteRoot =
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
    let failed = true;
    const { queryClient } = show({
      resource: {
        backend: "custom",
        connectionId: "remote",
        knowledgeBaseId: "kb",
      },
    });
    server.use(
      http.get(`${remoteRoot}/pages/one/revisions`, () =>
        HttpResponse.json({
          items: [{ version: 2 }, { version: 1 }],
          total: 2,
        }),
      ),
      http.get(`${remoteRoot}/pages/one/revisions/:version`, ({ params }) =>
        failed && Number(params.version) === missingVersion
          ? HttpResponse.json(
              { key: "KnowledgeForbidden", message: "快照暂不可读取" },
              { status: 403 },
            )
          : HttpResponse.json(
              revisions.find(
                (revision) => revision.version === Number(params.version),
              ),
            ),
      ),
    );
    if (missingVersion === 1) await select(1);
    await expectGlobalReadError();
    if (missingVersion === 1)
      expect(
        screen.queryByRole("button", { name: "恢复此版本" }),
      ).not.toBeInTheDocument();
    failed = false;
    await act(async () => {
      await queryClient.refetchQueries();
    });
    await waitFor(() =>
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
    );
    if (missingVersion === 1)
      expect(screen.getByRole("button", { name: "恢复此版本" })).toBeEnabled();
    else expect(screen.getByText("版本 2 → 版本 3")).toBeVisible();
  },
);
