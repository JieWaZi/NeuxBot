import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { PluginDetailPage } from "@/modules/plugin/pages/plugin-detail-page";
import { PluginCatalogDetailPage } from "@/modules/plugin/pages/plugin-catalog-detail-page";
import { SkillDetailPage } from "@/modules/skill/pages/skill-detail-page";
import { WakerSkillsPage } from "@/modules/skill/pages/waker-skills-page";
import { ConnectorDetailPage } from "@/modules/connector/pages/connector-detail-page";
import { WakerConnectorsPage } from "@/modules/connector/pages/waker-connectors-page";
import { ResourcesPage } from "./resources-page";

vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

const releases = new Set<() => void>();
const clients: QueryClient[] = [];
const server = setupServer(
  http.get("http://resource-loading.test/*", async () => {
    await new Promise<void>((resolve) => releases.add(resolve));
    return HttpResponse.json({});
  }),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://resource-loading.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  cleanup();
  for (const queryClient of clients) queryClient.clear();
  clients.length = 0;
  for (const release of releases) release();
  releases.clear();
  server.resetHandlers();
});

it("技能元数据就绪后，正文、文件与版本记录分别保留加载骨架", async () => {
  server.use(
    http.get(
      "http://resource-loading.test/api/v1/wakers/w1/skills/skill-1",
      () =>
        HttpResponse.json({
          skill: { id: "skill-1", name: "研究方法", source: "git" },
          currentVersion: { id: "v1", version: "1.0.0" },
        }),
    ),
  );
  mount("/wakers/w1/skills/skill-1");
  await screen.findByRole("heading", { name: "研究方法" });
  for (const name of ["说明", "资源文件 0", "当前版本 v1.0.0"]) {
    fireEvent.click(screen.getByRole("radio", { name }));
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
    expect(
      screen.getByRole("status").querySelector(".animate-pulse"),
    ).not.toBeNull();
  }
});

it("插件后台刷新保留已显示的内容和页签，不退回骨架", async () => {
  server.use(
    http.get("http://resource-loading.test/api/v1/plugins/plugin-1", () =>
      HttpResponse.json({
        plugin: { id: "plugin-1", name: "办公插件" },
        versions: [
          {
            id: "v1",
            package: {
              manifest: { name: "office", description: "整理办公资料" },
            },
          },
        ],
      }),
    ),
  );
  mount("/resources/plugins/plugin-1");
  await screen.findByRole("heading", { name: "办公插件" });
  expect(screen.getAllByText("整理办公资料")[0]).toBeVisible();
  await waitFor(() =>
    expect(screen.queryByRole("status")).not.toBeInTheDocument(),
  );
  server.resetHandlers();
  act(() => {
    void clients[0]?.invalidateQueries();
  });
  await waitFor(() => expect(clients[0]?.isFetching()).toBeGreaterThan(0));
  expect(screen.getByRole("heading", { name: "办公插件" })).toBeVisible();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  expect(screen.getAllByText("整理办公资料")[0]).toBeVisible();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});
afterAll(() => server.close());

function mount(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(queryClient);
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="resources" element={<ResourcesPage />} />
          <Route path="wakers/:wakerId/resources" element={<ResourcesPage />} />
          <Route
            path="resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
          <Route
            path="resources/plugins/:pluginId"
            element={<PluginDetailPage />}
          />
          <Route
            path="wakers/:wakerId/plugins/:pluginId"
            element={<PluginDetailPage />}
          />
          <Route
            path="resources/skills/:skillId"
            element={<SkillDetailPage />}
          />
          <Route
            path="wakers/:wakerId/skills/:skillId"
            element={<SkillDetailPage />}
          />
          <Route
            path="resources/connectors/:connectorId"
            element={<ConnectorDetailPage />}
          />
          <Route
            path="wakers/:wakerId/connectors/:connectorId"
            element={<ConnectorDetailPage />}
          />
          <Route path="wakers/:wakerId/skills" element={<WakerSkillsPage />} />
          <Route
            path="wakers/:wakerId/connectors"
            element={<WakerConnectorsPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

it.each([
  "/resources",
  "/resources?view=mine",
  "/resources?category=skills",
  "/resources?category=skills&source=plugins",
  "/resources?category=skills&source=independent",
  "/resources?category=connectors",
  "/resources?category=connectors&source=plugins",
  "/resources?category=connectors&source=independent",
  "/wakers/w1/resources",
  "/wakers/w1/resources?category=skills",
  "/wakers/w1/resources?category=connectors",
  "/wakers/w1/skills",
  "/wakers/w1/connectors",
])("%s 冷加载保留搜索与操作，内容区使用九项卡片骨架", (path) => {
  const view = mount(path);
  expect(screen.getByRole("heading", { level: 1 })).toBeVisible();
  expect(screen.getByRole("searchbox")).toBeVisible();
  expect(screen.getAllByRole("button").length).toBeGreaterThan(0);
  for (const status of screen.getAllByRole("status")) {
    expect(status).toHaveAccessibleName();
    expect(status).toHaveAttribute("aria-busy", "true");
    expect(status.querySelectorAll('[data-slot="card"]')).toHaveLength(9);
    expect(within(status).queryByRole("button")).not.toBeInTheDocument();
  }
  expect(view.container.querySelector(".agent-grid-spinner")).toBeNull();
});

it.each([
  "/resources/plugins/catalog/office",
  "/resources/plugins/plugin-1",
  "/resources/skills/skill-1",
  "/resources/connectors/connector-1",
  "/wakers/w1/plugins/plugin-1",
  "/wakers/w1/skills/skill-1",
  "/wakers/w1/connectors/connector-1",
])("%s 冷加载保留返回与页头，详情区使用可访问的分区骨架", (path) => {
  const view = mount(path);
  expect(screen.getByRole("heading", { level: 1 })).toBeVisible();
  expect(screen.getByRole("button", { name: /返回/ })).toBeEnabled();
  const status = screen.getByRole("status");
  expect(status).toHaveAccessibleName();
  expect(status).toHaveAttribute("aria-busy", "true");
  expect(status.querySelector(".animate-pulse")).not.toBeNull();
  expect(view.container.querySelector(".agent-grid-spinner")).toBeNull();
  if (path.includes("/skills/")) {
    expect(screen.getByRole("radiogroup")).toBeVisible();
  }
});
