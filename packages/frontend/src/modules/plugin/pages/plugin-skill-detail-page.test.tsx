import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { toApiError } from "@/shared/api/errors";
import { client } from "@/shared/api/generated/client.gen";
import { PluginSkillDetailPage } from "./plugin-skill-detail-page";
import { AppRouteScene } from "@/app/scenes/app-route-scene";

vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
const reads: URL[] = [];
const clients: QueryClient[] = [];
const server = setupServer(
  http.get(
    "http://plugin-skill-page.test/api/v1/plugins/catalog/office/skill",
    ({ request }) => {
      reads.push(new URL(request.url));
      return HttpResponse.json({
        skill: {
          key: "skills/guide",
          name: "目录技能",
          description: "目录里的说明",
        },
        content:
          "---\nname: guide\n---\n# 固定版本正文\n\n" +
          "Complete paragraph.\n\n".repeat(200) +
          "最后一行原文。",
        version: { id: "snapshot", version: "2.3.4" },
      });
    },
  ),
  http.get(
    "http://plugin-skill-page.test/api/v1/plugins/p/skill",
    ({ request }) => {
      reads.push(new URL(request.url));
      return HttpResponse.json({
        skill: { key: "guide", name: "操作指南", description: "整理工作资料" },
        content: "---\nname: guide\n---\n# 使用指南\n\n阅读并整理资料。",
        resources: [{ path: "SKILL.md" }],
        version: { id: "pinned", version: "1.0.0" },
        versions: [{ id: "pinned", version: "1.0.0" }],
      });
    },
  ),
  http.get("http://plugin-skill-page.test/api/v1/plugins/p", () =>
    HttpResponse.json({
      plugin: { id: "p", name: "办公" },
      versions: [{ id: "pinned", package: { manifest: { name: "office" } } }],
    }),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://plugin-skill-page.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  cleanup();
  clients.forEach((item) => item.clear());
  clients.length = 0;
  reads.length = 0;
  server.resetHandlers();
});
afterAll(() => server.close());

function Scene() {
  const location = useLocation();
  if (
    location.pathname === "/resources" ||
    location.pathname.endsWith("/resources")
  )
    return (
      <div>
        返回结果 {location.pathname}
        {location.search}
      </div>
    );
  return (
    <AppRouteScene
      location={location.pathname + location.search}
      outletContext={{
        chatRightPanel: undefined,
        onTeamThreadDeleted: vi.fn(),
        setChatRightPanel: vi.fn(),
      }}
    />
  );
}

it.each(["", "w1"])(
  "opens the %s plugin skill as a read-only detail page and returns to its plugin list",
  async (wakerId) => {
    const base = wakerId ? `/wakers/${wakerId}` : "/resources";
    const back = wakerId
      ? `/wakers/${wakerId}/resources?category=plugins`
      : "/resources?category=plugins";
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    clients.push(queryClient);
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[
            {
              pathname: `${base}/plugins/p/skills/guide`,
              search: "?version=pinned",
              state: { resourceBack: back },
            },
          ]}
        >
          <Scene />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("heading", { name: "使用指南" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "操作指南" })).toBeVisible();
    expect(screen.getByRole("link", { name: "插件" })).toHaveAttribute(
      "href",
      back,
    );
    expect(
      screen.queryByRole("link", { name: "技能" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "信息" })).toBeVisible();
    expect(
      (await screen.findAllByRole("link", { name: "办公" }))[0],
    ).toHaveAttribute("href", `${base}/plugins/p?version=pinned`);
    expect(
      screen.getAllByRole("link", { name: "办公" })[0],
    ).not.toHaveAttribute("aria-current");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /编辑|更新|卸载|删除/ }),
    ).not.toBeInTheDocument();
    expect(reads[0]?.searchParams.get("versionId")).toBe("pinned");
    expect(reads[0]?.searchParams.get("key")).toBe("guide");
    expect(reads[0]?.searchParams.get("wakerId")).toBe(wakerId || null);
    fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText(`返回结果 ${back}`)).toBeVisible();
  },
);

it("reads a complete catalog skill from its pinned source and returns to the plugin with its list context", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(queryClient);
  const sourceCommit = "a".repeat(40),
    archiveSHA256 = "b".repeat(64);
  const back = "/resources/plugins/catalog/office?from=search";
  function Parent() {
    const location = useLocation();
    return (
      <output aria-label="父插件">
        {location.pathname}
        {location.search}:{location.state?.resourceBack}
      </output>
    );
  }
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[
          {
            pathname: "/resources/plugins/catalog/office/skills/skills%2Fguide",
            search: `?sourceCommit=${sourceCommit}&archiveSHA256=${archiveSHA256}`,
            state: {
              resourceBack: back,
              pluginResourceBack: "/resources?category=plugins&query=office",
              sourceName: "Office",
            },
          },
        ]}
      >
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<PluginSkillDetailPage />}
          />
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<Parent />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("heading", { name: "固定版本正文" }),
  ).toBeVisible();
  expect(screen.getByText("最后一行原文。")).toBeVisible();
  expect(screen.getByRole("heading", { name: "目录技能" })).toBeVisible();
  expect(screen.getByRole("link", { name: "插件" })).toHaveAttribute(
    "href",
    "/resources?category=plugins&query=office",
  );
  expect(screen.queryByRole("link", { name: "技能" })).not.toBeInTheDocument();
  expect(reads).toHaveLength(1);
  expect(reads[0]?.searchParams.get("sourceCommit")).toBe(sourceCommit);
  expect(reads[0]?.searchParams.get("archiveSHA256")).toBe(archiveSHA256);
  expect(reads[0]?.searchParams.get("skillKey")).toBe("skills/guide");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
  expect(screen.getByLabelText("父插件")).toHaveTextContent(
    `${back}:/resources?category=plugins&query=office`,
  );
});
it("does not resolve a catalog skill without its source snapshot", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(queryClient);
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={["/resources/plugins/catalog/office/skills/guide"]}
      >
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<PluginSkillDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("没有找到这个技能")).toBeVisible();
  expect(reads).toHaveLength(0);
  expect(
    screen.getByRole("button", { name: "返回技能列表" }),
  ).toBeInTheDocument();
});

it("shows the precise unsupported-package error from a pinned skill deep link and returns to management", async () => {
  const interceptor = client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  server.use(
    http.get("http://plugin-skill-page.test/api/v1/plugins/p/skill", () =>
      HttpResponse.json(
        {
          key: "PluginPackageUnsupported",
          message: "当前插件包不受支持，请从插件目录重新安装。",
          messageSource: "public",
        },
        { status: 422 },
      ),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(queryClient);
  try {
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[
            {
              pathname: "/resources/plugins/p/skills/guide",
              search: "?version=pinned",
              state: { resourceBack: "/resources/plugins/p?version=pinned" },
            },
          ]}
        >
          <Routes>
            <Route
              path="/resources/plugins/:pluginId/skills/:skillKey"
              element={<PluginSkillDetailPage />}
            />
            <Route
              path="/resources/plugins/:pluginId"
              element={<div>插件管理页</div>}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "当前插件包不受支持，请从插件目录重新安装。",
    );
    expect(
      screen.queryByRole("heading", { name: "SKILL.md" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText("插件管理页")).toBeVisible();
  } finally {
    client.interceptors.error.eject(interceptor);
  }
});
