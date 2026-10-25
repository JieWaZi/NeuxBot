import { render, screen, fireEvent, cleanup } from "@testing-library/react";
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
import { WorkflowListPage } from "@/modules/workflow/pages/workflow-list-page";
vi.mock("@/modules/plugin", async (original) => ({
  ...(await original<typeof PluginModule>()),
  usePluginComponents: () => ({ items: [] }),
  PluginComponentCollection: () => <section>插件能力</section>,
  PluginResourceCollection: ({
    view,
    onViewChange,
  }: {
    view: string;
    onViewChange: (value: string) => void;
  }) => (
    <section>
      插件内容 {view}
      <button onClick={() => onViewChange("mine")}>我的插件</button>
    </section>
  ),
}));
vi.mock("@/modules/skill", () => ({
  SkillResourceCollection: ({
    search,
    source,
    wakerId,
    adding,
  }: {
    search: string;
    source: string;
    wakerId: string;
    adding: boolean;
  }) => (
    <section>
      技能内容 {search} {source} {wakerId}
      {adding && <p>技能导入</p>}
    </section>
  ),
}));
vi.mock("@/modules/connector", () => ({
  ConnectorResourceCollection: ({ adding }: { adding: boolean }) => (
    <section>连接器内容{adding && <p>连接器创建</p>}</section>
  ),
}));
vi.mock("@/modules/workflow", () => ({
  WorkflowResourceCollection: ({ search }: { search: string }) => (
    <section>工作流内容 {search}</section>
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
it("分类、搜索、视图和来源由 URL 恢复，切换时立即展示对应分类", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/resources?category=skills&search=review&source=independent",
      ]}
    >
      <HistoryControls />
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getByText(/技能内容 review independent/)).toBeVisible();
  fireEvent.click(screen.getByRole("radio", { name: "插件" }));
  expect(screen.queryByText(/技能内容/)).not.toBeInTheDocument();
  expect(screen.getByText(/插件内容 catalog/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "我的插件" }));
  expect(screen.getByText(/插件内容 mine/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "历史返回" }));
  fireEvent.click(screen.getByRole("button", { name: "历史返回" }));
  expect(screen.getByText(/技能内容 review independent/)).toBeVisible();
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
      </Routes>
    </MemoryRouter>,
  );
  const button = screen.getByRole("button", { name: label });
  expect(button.closest("header")).not.toBeNull();
  expect(button).toHaveClass("bg-button-primary");
  expect(
    screen.queryByRole("button", { name: "安装" }),
  ).not.toBeInTheDocument();
  fireEvent.click(button);
  expect(screen.getByText(content)).toBeVisible();
});

it.each([
  "/resources?category=plugins",
  "/resources?category=plugins&view=mine",
  "/wakers/w1/resources?category=plugins",
  "/wakers/w1/resources?category=plugins&view=mine",
])("%s 只展示插件库和系统资源，不提供本地插件导入入口", (entry) => {
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

it("工作流分类可从 URL 恢复，切换清理筛选并从页首创建", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/resources?category=skills&search=review&source=plugins",
      ]}
    >
      <HistoryControls />
      <Routes>
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="workflows/new" element={<p>新建工作流画布</p>} />
      </Routes>
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("radio", { name: "工作流" }));
  expect(screen.getByText("工作流内容")).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("?category=workflows");
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "历史返回" }));
  expect(screen.getByText(/技能内容 review plugins/)).toBeVisible();
  fireEvent.click(screen.getByRole("radio", { name: "工作流" }));
  const create = screen.getByRole("button", { name: "新建工作流" });
  expect(create.closest("header")).not.toBeNull();
  fireEvent.click(create);
  expect(screen.getByText("新建工作流画布")).toBeVisible();
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
      <Routes>
        <Route path="workflows" element={<WorkflowListPage />} />
        <Route path="resources" element={<ResourcesPage />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getByRole("radio", { name: "工作流" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  expect(screen.getByText("工作流内容 周会")).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("category=workflows");
});
