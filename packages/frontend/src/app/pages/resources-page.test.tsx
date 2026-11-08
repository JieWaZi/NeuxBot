import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  render as renderUI,
  screen,
  fireEvent,
  cleanup,
} from "@testing-library/react";
import {
  MemoryRouter,
  Routes,
  Route,
  useNavigate,
  useLocation,
} from "react-router-dom";
import { afterEach, it, expect, vi } from "vitest";
import "@/shared/i18n";
import type * as PluginModule from "@/modules/plugin";
import { ResourcesPage } from "./resources-page";
import { ResourcesContextSidebar } from "../layout/resources-context-sidebar";
import {
  resourceCategoryFromParams,
  changeResourceCategory,
} from "./resources-navigation";
import { useSearchParams } from "react-router-dom";
function ResourceNavigation() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  return (
    <>
      <ResourcesContextSidebar
        category={resourceCategoryFromParams(params)}
        onCategoryChange={(category) =>
          navigate(`/resources?${changeResourceCategory(params, category)}`)
        }
      />
    </>
  );
}
import { WorkflowListPage } from "@/modules/workflow/pages/workflow-list-page";
vi.mock("@/modules/plugin", async (original) => ({
  ...(await original<typeof PluginModule>()),
  usePluginBrowse: () => ({ entries: [], isError: false, hasNextPage: false }),
  PluginResourceCollection: () => <section>插件内容</section>,
}));
vi.mock("@/modules/skill", () => ({
  SkillResourceCollection: ({
    search,
    filters,
    wakerId,
  }: {
    search: string;
    filters: ReactNode;
    wakerId: string;
  }) => (
    <section>
      技能内容 {search} {wakerId}
      {filters}
    </section>
  ),
}));
vi.mock("@/modules/connector", () => ({
  ConnectorResourceCollection: ({ filters }: { filters: ReactNode }) => (
    <section>连接器内容{filters}</section>
  ),
}));
vi.mock("@/modules/workflow", () => ({
  WorkflowResourceCollection: ({
    search,
    filters,
  }: {
    search: string;
    filters: ReactNode;
  }) => (
    <section>
      工作流内容 {search}
      {filters}
    </section>
  ),
}));
afterEach(cleanup);
function HistoryControls() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <output>{location.search}</output>
      <button onClick={() => navigate(-1)}>历史返回</button>
    </>
  );
}
it("分类和搜索由 URL 恢复，技能页忽略旧来源筛选", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/resources?category=skills&search=review&source=independent",
      ]}
    >
      <HistoryControls />
      <ResourceNavigation />
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getByText(/技能内容 review/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "插件" }));
  expect(screen.queryByText(/技能内容/)).not.toBeInTheDocument();
  expect(screen.getByText("插件内容")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "我的插件" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "历史返回" }));
  expect(screen.getByText(/技能内容 review/)).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent(
    "category=skills&search=review&source=independent",
  );
});

it.each([
  ["/resources?category=skills", "添加技能", "技能导入"],
  ["/resources?category=connectors", "添加连接器", "连接器创建"],
  ["/wakers/w1/resources?category=skills", "添加技能", "技能导入"],
  ["/wakers/w1/resources?category=connectors", "添加连接器", "连接器创建"],
])("%s 的添加操作位于页首，并打开对应流程", (entry, label, content) => {
  render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="wakers/:wakerId/resources" element={<ResourcesPage />} />
        <Route
          path="resources/connectors/new"
          element={<section>连接器创建</section>}
        />
        <Route
          path="wakers/:wakerId/resources/connectors/new"
          element={<section>连接器创建</section>}
        />
        <Route
          path="resources/skills/new"
          element={<section>技能导入</section>}
        />
        <Route
          path="wakers/:wakerId/resources/skills/new"
          element={<section>技能导入</section>}
        />
      </Routes>
    </MemoryRouter>,
  );
  const button = screen.getByRole("button", { name: label });
  expect(button.closest("header")).toBeNull();
  expect(button).toHaveClass("bg-resource-primary", "text-resource-on-primary");
  expect(
    screen.queryByRole("button", { name: "安装" }),
  ).not.toBeInTheDocument();
  fireEvent.click(button);
  expect(screen.getByText(content)).toBeVisible();
});

it.each([
  "/resources?category=plugins",
  "/wakers/w1/resources?category=plugins",
])("%s 只展示统一插件列表和系统资源，不提供本地插件导入入口", (entry) => {
  render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="wakers/:wakerId/resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getByText(/插件内容/)).toBeVisible();
  expect(
    screen.queryByRole("button", {
      name: /上传|导入|添加新版本|选择文件夹|选择 ZIP/,
    }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("工作流分类由资源左栏切换，清理筛选并从搜索行创建", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/resources?category=skills&search=review&source=plugins",
      ]}
    >
      <HistoryControls />
      <ResourceNavigation />
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="workflows/new" element={<WorkflowDraftLocation />} />
      </Routes>
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "工作流" }));
  expect(screen.getByText(/工作流内容/)).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("?category=workflows");
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "历史返回" }));
  expect(screen.getByText(/技能内容 review/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "工作流" }));
  const create = screen.getByRole("button", { name: "添加工作流" });
  expect(create.closest("header")).toBeNull();
  expect(screen.getByRole("searchbox", { name: "搜索工作流" })).toBeVisible();
  expect(
    screen.getByRole("heading", { name: "工作流", level: 1 }),
  ).toBeVisible();
  fireEvent.click(create);
  expect(screen.getByText("新建工作流画布")).toBeVisible();
  expect(screen.getByTestId("draft-origin")).toHaveTextContent(
    "/resources?category=workflows",
  );
  expect(screen.getByTestId("workflow-draft-id").textContent).toBeTruthy();
});

it("伙伴已安装资源继续只展示可安装的三种资源", () => {
  render(
    <MemoryRouter initialEntries={["/wakers/w1/resources"]}>
      <Routes>
        <Route path="wakers/:wakerId/resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getAllByRole("radio")).toHaveLength(3);
  expect(
    screen.queryByRole("radio", { name: "工作流" }),
  ).not.toBeInTheDocument();
});

it("旧工作流列表地址跳转资源库并保留搜索条件", () => {
  render(
    <MemoryRouter initialEntries={["/workflows?search=周会"]}>
      <HistoryControls />
      <ResourceNavigation />
      <Routes>
        <Route path="workflows" element={<WorkflowListPage />} />
        <Route path="resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.queryByRole("radio", { name: "工作流" })).toBeNull();
  expect(screen.getByText(/工作流内容 周会/)).toBeVisible();
  expect(screen.getByRole("searchbox", { name: "搜索工作流" })).toHaveValue(
    "周会",
  );
  expect(screen.getByRole("status")).toHaveTextContent("category=workflows");
});

it("全局资源页不再重复分类分段导航", () => {
  render(
    <MemoryRouter initialEntries={["/resources?category=skills"]}>
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.queryByRole("radiogroup", { name: "资源库" })).toBeNull();
  expect(screen.getByText(/技能内容/)).toBeVisible();
});

it("外部分类切换离开连接器添加页且不带到另一分类", () => {
  render(
    <MemoryRouter initialEntries={["/resources?category=connectors"]}>
      <ResourceNavigation />
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
        <Route
          path="resources/connectors/new"
          element={<section>连接器创建</section>}
        />
      </Routes>
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加连接器" }));
  expect(screen.getByText("连接器创建")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "技能" }));
  expect(screen.queryByText("连接器创建")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "连接器" }));
  expect(screen.queryByText("连接器创建")).toBeNull();
});

function render(ui: ReactNode) {
  return renderUI(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      {ui}
    </QueryClientProvider>,
  );
}

function WorkflowDraftLocation() {
  const location = useLocation();
  const state: unknown = location.state;
  return (
    <>
      <p>新建工作流画布</p>
      <span data-testid="draft-origin">
        {state &&
        typeof state === "object" &&
        "resourceBack" in state &&
        typeof state.resourceBack === "string"
          ? state.resourceBack
          : ""}
      </span>
      <span data-testid="workflow-draft-id">
        {new URLSearchParams(location.search).get("draftId")}
      </span>
    </>
  );
}

it.each([
  ["plugins", "插件", "搜索插件"],
  ["skills", "技能", "搜索技能"],
  ["connectors", "连接器", "搜索连接器"],
] as const)("%s 顶部使用当前资源类别名称", (category, label, searchLabel) => {
  render(
    <MemoryRouter initialEntries={[`/resources?category=${category}`]}>
      <ResourcesPage />
    </MemoryRouter>,
  );
  const breadcrumbs = screen.getByRole("navigation", { name: label });
  expect(breadcrumbs.querySelector('[aria-current="page"]')).toHaveTextContent(
    label,
  );
  expect(breadcrumbs).not.toHaveTextContent("资源库");
  if (searchLabel && category === "skills")
    expect(screen.getByRole("searchbox", { name: searchLabel })).toBeVisible();
});
