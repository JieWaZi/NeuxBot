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
  for (const name of ["SKILL.md", "资源文件 0", "版本"]) {
    fireEvent.click(screen.getByRole("radio", { name }));
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
    expect(
      screen.getByRole("status").querySelector(".animate-pulse"),
    ).not.toBeNull();
  }
});

it("我的技能按实际安装关系禁用重复安装，并支持单独启停和移除伙伴", async () => {
  const base = "http://resource-loading.test/api/v1";
  const skill = {
    id: "brainstorming",
    name: "brainstorming",
    source: "zip",
    categories: ["编程"],
  };
  const profiles = [
    { id: "w1", name: "伙伴甲" },
    { id: "w2", name: "伙伴乙" },
  ];
  const currentVersion = { id: "v1", version: "1.0.0" };
  const installations = new Map<string, boolean>();
  const snapshot = () => ({
    skill,
    currentVersion,
    affectedWakerCount: installations.size,
    installedPartners: profiles.flatMap((partner) => {
      const enabled = installations.get(partner.id);
      return enabled === undefined ? [] : [{ ...partner, enabled }];
    }),
  });
  server.use(
    http.get(`${base}/skills`, () =>
      HttpResponse.json({ resources: [snapshot()], totalSize: 1 }),
    ),
    http.get(`${base}/skills/brainstorming`, () =>
      HttpResponse.json(snapshot()),
    ),
    http.get(`${base}/skills/brainstorming/versions`, () =>
      HttpResponse.json([currentVersion]),
    ),
    http.get(`${base}/skills/brainstorming/versions/v1`, () =>
      HttpResponse.json({
        version: currentVersion,
        content: "# Brainstorming",
      }),
    ),
    http.get(`${base}/resource-categories`, () => HttpResponse.json([])),
    http.get(`${base}/wakers`, () =>
      HttpResponse.json({ resources: profiles, totalSize: 2 }),
    ),
    http.post(`${base}/skills/brainstorming/install`, async ({ request }) => {
      const { wakerIds } = (await request.json()) as { wakerIds: string[] };
      for (const id of wakerIds) installations.set(id, true);
      return HttpResponse.json({ success: true });
    }),
    http.put(
      `${base}/wakers/:wakerId/skills/brainstorming/enabled`,
      async ({ request, params }) => {
        const { enabled } = (await request.json()) as { enabled: boolean };
        installations.set(String(params.wakerId), enabled);
        return HttpResponse.json({ success: true });
      },
    ),
    http.delete(
      `${base}/wakers/:wakerId/skills/brainstorming`,
      ({ params }) => {
        installations.delete(String(params.wakerId));
        return HttpResponse.json({ success: true });
      },
    ),
  );

  mount("/resources?category=skills");
  const available = await screen.findByRole("region", { name: "编程" });
  fireEvent.click(within(available).getByRole("button", { name: "安装" }));
  const install = screen.getByRole("dialog");
  for (const partner of profiles)
    fireEvent.click(
      await within(install).findByRole("checkbox", { name: partner.name }),
    );
  fireEvent.click(within(install).getByRole("button", { name: "安装" }));
  fireEvent.click(await within(install).findByRole("button", { name: "关闭" }));

  const installed = await screen.findByRole("region", { name: "我的技能" });
  const link = within(installed).getByRole("link", {
    name: "查看技能 brainstorming",
  });
  expect(
    screen.getAllByRole("link", { name: "查看技能 brainstorming" }),
  ).toEqual([link]);
  fireEvent.click(
    within(installed).getByRole("button", { name: "安装到伙伴" }),
  );
  const reinstall = within(screen.getByRole("dialog"));
  for (const partner of profiles) {
    const checkbox = await reinstall.findByRole("checkbox", {
      name: partner.name,
    });
    expect(checkbox).toBeChecked();
    expect(checkbox).toBeDisabled();
  }
  expect(reinstall.getByRole("button", { name: "安装" })).toBeDisabled();
  fireEvent.click(reinstall.getByRole("button", { name: "取消" }));
  fireEvent.click(link);

  const first = await screen.findByRole("switch", {
    name: "伙伴甲的技能 brainstorming",
  });
  const second = screen.getByRole("switch", {
    name: "伙伴乙的技能 brainstorming",
  });
  expect(first).toBeChecked();
  expect(second).toBeChecked();
  fireEvent.click(first);
  await waitFor(() => expect(first).not.toBeChecked());
  expect(second).toBeChecked();
  fireEvent.click(first);
  await waitFor(() => expect(first).toBeChecked());

  const firstRow = screen.getByRole("group", { name: "伙伴甲" });
  fireEvent.click(within(firstRow).getByRole("button", { name: "移除" }));
  const remove = screen.getByRole("dialog", { name: "移除技能" });
  fireEvent.click(within(remove).getByRole("button", { name: "确认移除" }));
  await waitFor(() => expect(remove).not.toBeInTheDocument());
  expect(first).not.toBeInTheDocument();
  expect(second).toBeChecked();
  fireEvent.click(second);
  await waitFor(() => expect(second).not.toBeChecked());

  fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
  const remaining = await screen.findByRole("region", { name: "我的技能" });
  expect(await within(remaining).findByText("已安装到 1 个伙伴")).toBeVisible();

  fireEvent.click(
    within(remaining).getByRole("button", { name: "安装到伙伴" }),
  );
  const installAfterRemoval = within(screen.getByRole("dialog"));
  const removedPartner = await installAfterRemoval.findByRole("checkbox", {
    name: "伙伴甲",
  });
  const disabledPartner = installAfterRemoval.getByRole("checkbox", {
    name: "伙伴乙",
  });
  expect(disabledPartner).toBeChecked();
  expect(disabledPartner).toBeDisabled();
  expect(removedPartner).not.toBeChecked();
  fireEvent.click(removedPartner);
  expect(
    installAfterRemoval.getByRole("button", { name: "安装" }),
  ).toBeEnabled();
  fireEvent.click(installAfterRemoval.getByRole("button", { name: "取消" }));

  fireEvent.click(
    within(remaining).getByRole("link", { name: "查看技能 brainstorming" }),
  );
  const lastRow = await screen.findByRole("group", { name: "伙伴乙" });
  fireEvent.click(within(lastRow).getByRole("button", { name: "移除" }));
  fireEvent.click(
    within(screen.getByRole("dialog", { name: "移除技能" })).getByRole(
      "button",
      { name: "确认移除" },
    ),
  );
  await waitFor(() => expect(lastRow).not.toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
  expect(
    within(await screen.findByRole("region", { name: "编程" })).getByRole(
      "button",
      { name: "安装" },
    ),
  ).toBeVisible();
  expect(screen.queryByRole("region", { name: "我的技能" })).toBeNull();
});

it("指定伙伴时等待技能详情，并阻止安装关系已存在的伙伴提交", async () => {
  const base = "http://resource-loading.test/api/v1";
  const partner = { id: "w1", name: "伙伴甲" };
  const summary = {
    skill: { id: "brainstorming", name: "brainstorming", source: "zip" },
    currentVersion: { id: "v1", version: "1.0.0" },
    affectedWakerCount: 0,
    installedPartners: [],
  };
  server.use(
    http.get(`${base}/skills`, () =>
      HttpResponse.json({ resources: [summary], totalSize: 1 }),
    ),
    http.get(`${base}/skills/brainstorming`, () =>
      HttpResponse.json({}, { status: 500 }),
    ),
    http.get(`${base}/wakers/w1`, () => HttpResponse.json(partner)),
  );
  mount("/resources?category=skills&partner=w1");
  fireEvent.click(await screen.findByRole("button", { name: "安装" }));
  const dialog = within(screen.getByRole("dialog"));
  const submit = dialog.getByRole("button", { name: "安装" });
  expect(submit).toBeDisabled();
  const retry = await dialog.findByRole("button", { name: "重试" });
  expect(submit).toBeDisabled();

  server.use(
    http.get(`${base}/skills/brainstorming`, () =>
      HttpResponse.json({
        ...summary,
        affectedWakerCount: 1,
        installedPartners: [{ ...partner, enabled: false }],
      }),
    ),
  );
  fireEvent.click(retry);
  const choice = await dialog.findByRole("checkbox", { name: "伙伴甲" });
  expect(choice).toBeChecked();
  expect(choice).toBeDisabled();
  expect(submit).toBeDisabled();
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
])("%s 冷加载保留搜索与操作，内容区使用匹配资源布局的九项骨架", (path) => {
  const view = mount(path);
  expect(screen.getByRole("heading", { level: 1 })).toBeVisible();
  expect(screen.getByRole("searchbox")).toBeVisible();
  expect(screen.getAllByRole("button").length).toBeGreaterThan(0);
  for (const status of screen.getAllByRole("status")) {
    expect(status).toHaveAccessibleName();
    expect(status).toHaveAttribute("aria-busy", "true");
    if (
      path === "/resources" ||
      path === "/wakers/w1/resources" ||
      path.includes("category=skills") ||
      path.includes("category=connectors") ||
      path.endsWith("/connectors") ||
      path.endsWith("/skills")
    ) {
      expect(status.querySelectorAll(".size-resource-icon")).toHaveLength(9);
      expect(status.querySelectorAll('[data-slot="card"]')).toHaveLength(0);
    } else {
      expect(status.querySelectorAll('[data-slot="card"]')).toHaveLength(9);
    }
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
