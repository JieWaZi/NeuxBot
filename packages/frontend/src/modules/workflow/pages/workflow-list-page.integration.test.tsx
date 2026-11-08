import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { SearchField } from "@/shared/ui/search-field";
import {
  WorkflowListPage,
  WorkflowResourceCollection,
} from "./workflow-list-page";

vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: () => () => undefined,
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));

const origin = "http://workflow-list.test";
const server = setupServer();
const intersections = new Map<Element, () => void>();
let definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  definition = {
    id: "flow",
    title: "周会准备流程",
    description: "生成通用议程和材料清单",
    currentRevisionId: "revision-1",
    ordinal: 1,
  };
  server.use(
    http.get(`${origin}/api/v1/resource-categories`, () =>
      HttpResponse.json([{ name: "设计" }, { name: "研究" }]),
    ),
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ items: [definition] }),
    ),
  );
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      targets = new Set<Element>();
      constructor(private callback: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.targets.add(target);
        intersections.set(target, () =>
          this.callback(
            [{ target, isIntersecting: true } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          ),
        );
      }
      disconnect() {
        for (const target of this.targets) intersections.delete(target);
      }
      unobserve(target: Element) {
        intersections.delete(target);
      }
    },
  );
});
afterEach(() => {
  cleanup();
  intersections.clear();
  vi.unstubAllGlobals();
  server.resetHandlers();
});
afterAll(() => server.close());

it("工作流按真实分类分组，未分类也使用稳定彩色图标，分类过滤进入请求", async () => {
  const queries: URLSearchParams[] = [];
  server.use(
    http.get(`${origin}/api/v1/workflows`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      queries.push(params);
      return HttpResponse.json({
        items:
          params.get("category") === "设计"
            ? [{ ...definition, categories: ["设计", "研究"] }]
            : [
                { ...definition, categories: ["设计", "研究"] },
                { ...definition, id: "other", title: "未分组流程" },
              ],
      });
    }),
  );
  renderList();
  await screen.findByRole("region", { name: "设计" });
  expect(screen.getByRole("region", { name: "研究" })).toBeVisible();
  const untagged = screen.getByRole("region", { name: "未分类" });
  expect(
    untagged.querySelector("[class*='bg-resource-category-']"),
  ).not.toBeNull();
  const user = userEvent.setup();
  await user.click(screen.getByRole("combobox", { name: "分类" }));
  await user.click(await screen.findByRole("option", { name: "设计" }));
  await waitFor(() => expect(queries.at(-1)?.get("category")).toBe("设计"));
  expect(
    await screen.findByRole("link", { name: "周会准备流程" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("link", { name: "未分组流程" }),
  ).not.toBeInTheDocument();
});

function List() {
  const [params, setParams] = useSearchParams();
  const search = params.get("search") ?? "";
  return (
    <WorkflowResourceCollection
      search={search}
      filters={
        <SearchField
          appearance="catalog"
          size="md"
          width="full"
          value={search}
          aria-label="搜索工作流"
          placeholder="搜索工作流"
          onChange={(event) =>
            setParams({ category: "workflows", search: event.target.value })
          }
        />
      }
    />
  );
}

function Location() {
  const location = useLocation();
  return <output>{JSON.stringify(location)}</output>;
}

function renderList(entry = "/resources?category=workflows") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/workflows" element={<WorkflowListPage />} />
          <Route path="/resources" element={<List />} />
          <Route path="/workflows/:id" element={<Location />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return queryClient;
}

it("目录条目只保留名称和说明，详情链接保留资源搜索上下文", async () => {
  const user = userEvent.setup();
  renderList("/resources?category=workflows&search=周会");
  const link = await screen.findByRole("link", { name: "周会准备流程" });
  expect(link).toHaveAttribute("href", "/workflows/flow");
  expect(screen.getByRole("article")).toHaveAttribute("data-appearance", "row");
  expect(screen.getByText("生成通用议程和材料清单")).toBeVisible();
  expect(screen.queryByText("版本 1")).not.toBeInTheDocument();
  expect(screen.queryByText("全部工作流")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /更多操作/ }),
  ).not.toBeInTheDocument();
  await user.click(link);
  expect(
    JSON.parse(screen.getByRole("status").textContent ?? "{}").state,
  ).toEqual({
    resourceBack: "/resources?category=workflows&search=周会",
  });
});

it("首次加载保留搜索并使用目录行骨架，失败提供重试而不显示空库", async () => {
  let rejectRequest: (() => void) | undefined;
  server.use(
    http.get(`${origin}/api/v1/workflows`, async () => {
      await new Promise<void>((resolve) => {
        rejectRequest = resolve;
      });
      return HttpResponse.json({ message: "目录暂时不可用" }, { status: 503 });
    }),
  );
  renderList();
  expect(screen.getByRole("searchbox", { name: "搜索工作流" })).toBeVisible();
  expect(
    screen.getByRole("status", { name: "正在读取工作流…" }),
  ).toHaveAttribute("aria-busy", "true");
  await waitFor(() => expect(rejectRequest).toBeDefined());
  await act(async () => rejectRequest?.());
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "重试" })).toBeEnabled();
  expect(screen.queryByText("创建你的第一个工作流")).not.toBeInTheDocument();
});

it("后台刷新失败保留已加载条目，并可就近重试", async () => {
  const user = userEvent.setup();
  const queryClient = renderList();
  const row = (
    await screen.findByRole("heading", { name: "周会准备流程" })
  ).closest("article");
  server.use(
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ message: "目录暂时不可用" }, { status: 503 }),
    ),
  );
  await act(async () => {
    await queryClient.invalidateQueries();
  });
  await screen.findByRole("alert");
  expect(
    screen.getByRole("heading", { name: "周会准备流程" }).closest("article"),
  ).toBe(row);
  server.use(
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ items: [definition] }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("heading", { name: "周会准备流程" }).closest("article"),
  ).toBe(row);
});

it("搜索调用真实工作流查询，无匹配与空库分别表达", async () => {
  const searches: string[] = [];
  server.use(
    http.get(`${origin}/api/v1/workflows`, ({ request }) => {
      const search = new URL(request.url).searchParams.get("search") ?? "";
      searches.push(search);
      return HttpResponse.json({
        items: search === "不存在" ? [] : [definition],
      });
    }),
  );
  renderList();
  await screen.findByRole("heading", { name: "周会准备流程" });
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索工作流" }), {
    target: { value: "不存在" },
  });
  await screen.findByText("没有匹配的资源");
  expect(screen.queryByText("创建你的第一个工作流")).not.toBeInTheDocument();
  expect(searches.at(-1)).toBe("不存在");
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索工作流" }), {
    target: { value: "" },
  });
  await screen.findByRole("heading", { name: "周会准备流程" });
});

it("工作流每批24项，滚动追加保留条目并传递搜索和游标，失败只重试下一批", async () => {
  const requests: URLSearchParams[] = [];
  let failNextPage = true;
  server.use(
    http.get(`${origin}/api/v1/workflows`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      requests.push(params);
      if (params.has("beforeId"))
        return failNextPage
          ? HttpResponse.json({ message: "下一批暂时不可用" }, { status: 503 })
          : HttpResponse.json({
              items: [{ ...definition, id: "flow-24", title: "最后一个流程" }],
            });
      return HttpResponse.json({
        items: Array.from({ length: 24 }, (_, index) => ({
          ...definition,
          id: `flow-${index}`,
          title: `流程 ${index}`,
        })),
        nextBefore: 123,
        nextBeforeId: "flow-23",
      });
    }),
  );
  renderList("/resources?category=workflows&search=流程");
  const first = (
    await screen.findByRole("heading", { name: "流程 0" })
  ).closest("article");
  expect(screen.getAllByRole("article")).toHaveLength(24);
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  await waitFor(() => expect(intersections.size).toBe(1));
  await act(async () => {
    for (const intersect of intersections.values()) intersect();
  });
  await screen.findByRole("alert");
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getAllByRole("article")).toHaveLength(24);
  failNextPage = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByRole("heading", { name: "最后一个流程" });
  expect(screen.getAllByRole("article")).toHaveLength(25);
  expect(
    screen.getByRole("heading", { name: "流程 0" }).closest("article"),
  ).toBe(first);
  expect(
    requests.every(
      (params) =>
        params.get("limit") === "24" && params.get("search") === "流程",
    ),
  ).toBe(true);
  expect(requests.at(-1)?.get("beforeId")).toBe("flow-23");
  expect(requests.at(-1)?.get("before")).toBe("123");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("旧列表地址继续进入资源库并保留搜索", async () => {
  renderList("/workflows?search=周会");
  await screen.findByRole("heading", { name: "周会准备流程" });
  expect(screen.getByRole("searchbox", { name: "搜索工作流" })).toHaveValue(
    "周会",
  );
});
