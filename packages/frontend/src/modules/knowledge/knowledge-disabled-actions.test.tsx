import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ThemeProvider } from "@/shared/theme/theme-provider";
import { KnowledgeLibraryActions } from "./knowledge-library-actions";
import { KnowledgeWikiIndexContent } from "./knowledge-wiki-index";
import { KnowledgeWikiHistory } from "./knowledge-wiki-history";
import { KnowledgeLibraryActivity } from "./knowledge-library-activity";
import { KnowledgeWikiView } from "./knowledge-wiki-view";
import { emptyLibrary } from "./knowledge-model";

// 分页与禁用状态测试保留差异内容，Canvas 渲染由 TextDiffView 自身测试覆盖。
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
const resource = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
const root = "http://disabled.test/api/v1/knowledge/libraries/kb";
beforeAll(() => {
  window.localStorage.setItem("ally.ui.theme", "light");
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://disabled.test" });
});
afterEach(() => {
  server.resetHandlers();
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});
afterAll(() => {
  window.localStorage.removeItem("ally.ui.theme");
  server.close();
});

it.each([false, true])(
  "图谱目标不可用时禁用入口，返回文档=%s",
  async (returning) => {
    const onGraph = vi.fn();
    render(
      <TooltipProvider>
        <KnowledgeLibraryActions
          library={emptyLibrary("kb", "资料", "general")}
          onUpload={vi.fn()}
          onOrganize={vi.fn()}
          organizeDisabled={false}
          onSettings={vi.fn()}
          onGraph={onGraph}
          graphActive={returning}
          graphDisabled={!returning}
        />
      </TooltipProvider>,
    );
    const button = screen.getByRole("button", {
      name: returning ? "文档" : "图谱",
    });
    if (returning) expect(button).toBeEnabled();
    else expect(button).toBeDisabled();
    await userEvent.click(button);
    expect(onGraph).toHaveBeenCalledTimes(returning ? 1 : 0);
  },
);

it("维护聊天能力不可用时保留禁用的AI修订按钮", async () => {
  const page = {
    id: "one",
    slug: "concept/one",
    title: "页面",
    pageType: "concept",
    content: "正文",
  };
  server.use(
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({ items: [page], total: 1 }),
    ),
    http.get(`${root}/themes`, () => HttpResponse.json({ items: [] })),
    http.get(`${root}/pages/one`, () => HttpResponse.json(page)),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <KnowledgeWikiView
          resource={resource}
          pageId="one"
          writable
          structureVersion={1}
          onPage={vi.fn()}
          onMaintenance={vi.fn()}
          maintenanceDisabled
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("button", { name: "AI 修订" })).toBeDisabled();
});

it.each(["index", "history", "activity"])(
  "%s 后页读取失败保留内容并禁用加载更多",
  async (kind) => {
    let failed = false;
    const page = {
      id: "one",
      slug: "concept/one",
      title: "页面",
      content: "当前正文",
      version: 3,
    };
    server.use(
      http.get(`${root}/wiki/index`, () => {
        failed = true;
        return new HttpResponse(null, { status: 503 });
      }),
      http.get(`${root}/pages/one/revisions`, ({ request }) => {
        if (Number(new URL(request.url).searchParams.get("offset"))) {
          failed = true;
          return new HttpResponse(null, { status: 503 });
        }
        return HttpResponse.json({
          items: [
            { id: "r2", version: 2, title: "历史标题", content: "历史正文" },
          ],
          total: 2,
        });
      }),
      http.get(`${root}/activity`, ({ request }) => {
        if (new URL(request.url).searchParams.get("afterId")) {
          failed = true;
          return new HttpResponse(null, { status: 503 });
        }
        return HttpResponse.json({
          data: [{ id: 1, action: "kb.created", outcome: "success" }],
          nextCursor: 1,
        });
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          {kind === "index" ? (
            <KnowledgeWikiIndexContent
              resource={resource}
              data={{
                intro: "范围",
                groups: [
                  {
                    type: "concept",
                    total: 2,
                    nextCursor: "next",
                    items: [{ slug: "concept/one", title: "第一页" }],
                  },
                ],
              }}
              refreshedAt={1}
              onLink={vi.fn()}
              remarkPlugins={[]}
            />
          ) : kind === "history" ? (
            <ThemeProvider>
              <KnowledgeWikiHistory
                resource={resource}
                page={page}
                writable={false}
                structureVersion={1}
                commands={new Map()}
                commandKey="history"
                onClose={vi.fn()}
                onRestored={vi.fn()}
              />
            </ThemeProvider>
          ) : (
            <KnowledgeLibraryActivity
              library={emptyLibrary("kb", "资料", "general")}
            />
          )}
        </TooltipProvider>
      </QueryClientProvider>,
    );
    const more = await screen.findByRole("button", {
      name: kind === "activity" ? "加载更多记录" : "加载更多",
    });
    if (kind === "history") {
      expect(await screen.findByText("历史正文")).toBeVisible();
      expect(await screen.findByText("当前正文")).toBeVisible();
    }
    await userEvent.click(more);
    await waitFor(() => expect(failed).toBe(true));
    await waitFor(() => expect(more).toBeDisabled());
    expect(
      screen.queryByRole("button", { name: "重试" }),
    ).not.toBeInTheDocument();
    if (kind === "history") {
      expect(screen.getByText("历史正文")).toBeVisible();
      expect(screen.getByText("当前正文")).toBeVisible();
    }
    view.unmount();
    queryClient.clear();
  },
);
