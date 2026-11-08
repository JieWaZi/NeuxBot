import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { i18n } from "@/shared/i18n";
import { PluginCatalogDetailPage } from "./plugin-catalog-detail-page";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import type { PluginDetail } from "../model/plugin-view";

const fixture = vi.hoisted(() => ({
  version: 1,
  accountAvailable: true,
  installedError: false,
  read: vi.fn(),
  listeners: new Set<GlobalEventObserver>(),
}));
vi.mock("../components/plugin-presentation", () => ({
  PluginPresentation: () => null,
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.listeners.add(observer);
    return () => fixture.listeners.delete(observer);
  },
}));
function SkillRoute() {
  const location = useLocation();
  return (
    <output aria-label="技能路由">
      {location.pathname}
      {location.search}
    </output>
  );
}
vi.mock("../api/plugin-queries", () => {
  const pkg = {
    manifest: {
      name: "github",
      repository: "https://github.com/github/github-mcp-server",
      description: "旧简介",
      author: { name: "开发者署名" },
    },
    presentation: {
      displayName: "GitHub",
      displayNames: { en: "GitHub", "zh-CN": "GitHub" },
      localizedDescriptions: { en: "Manage repositories", "zh-CN": "旧简介" },
    },
    skills: [{ key: "skills/guide", name: "guide", description: "A guide" }],
    servers: [{ key: "github", url: "https://api.githubcopilot.com/mcp/" }],
    readme: "源码与钥匙串隔离",
  };
  return {
    getCatalogPluginOptions: (options: { query?: { locale?: string } }) => ({
      queryKey: ["catalog", options.query?.locale],
      queryFn: async () => {
        fixture.read();
        return {
          key: "github",
          sourceCommit: String(fixture.version).repeat(40),
          archiveSHA256: String(fixture.version).repeat(64),
          package: {
            ...pkg,
            readme:
              options.query?.locale === "en" ? "Business guide" : "业务说明",
          },
        };
      },
    }),
    listPluginsOptions: () => ({
      queryKey: ["installed"],
      queryFn: async () => {
        if (fixture.installedError) throw new Error("installed refresh failed");
        return {
          resources: [
            {
              plugin: { id: "p", sourceKey: "catalog:github" },
              resolvedVersionId: "v1",
              account: { available: fixture.accountAvailable },
              versions: [{ id: "v1", package: pkg }],
              installations: [],
            },
          ],
        };
      },
    }),
  };
});
vi.mock("../components/plugin-settings-dialog", () => ({
  PluginSettingsDialog: ({ detail }: { detail: PluginDetail }) => (
    <div role="dialog">{detail.plugin?.id}</div>
  ),
}));

// 页面行为用例先准备实际正文模块，避免将开发环境模块编译时间当作读取失败。
beforeAll(async () => {
  await import("../components/plugin-overview");
});

afterEach(async () => {
  cleanup();
  fixture.listeners.clear();
  fixture.version = 1;
  fixture.accountAvailable = true;
  fixture.installedError = false;
  fixture.read.mockClear();
  await i18n.changeLanguage("zh-CN");
});

it("keeps catalog instructions readable before configuration and authentication are ready", async () => {
  fixture.accountAvailable = false;
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/plugins/catalog/github"]}>
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<SkillRoute />}
          />
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("link", { name: "插件" })).toHaveAttribute(
    "href",
    "/resources",
  );
  expect(
    screen.queryByRole("link", { name: "资源库" }),
  ).not.toBeInTheDocument();
  expect(await screen.findByText("业务说明")).toBeVisible();
  expect(screen.getByRole("heading", { name: "插件信息" })).toBeVisible();
  expect(
    screen.queryByText("请先完成插件所需配置、认证和程序准备"),
  ).not.toBeInTheDocument();
});

it("catalog details use the selected source summary and README and edit the actual installed resource", async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/plugins/catalog/github"]}>
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<SkillRoute />}
          />
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await screen.findByRole("heading", { name: "GitHub" });
  const summary = screen.getByText("旧简介");
  expect(summary).toHaveClass("truncate");
  expect(summary).toHaveAttribute("title", summary.textContent);
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "说明" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("业务说明")).toBeVisible();
  expect(
    screen.queryByRole("button", {
      name: /上传|导入|添加新版本|选择文件夹|选择 ZIP/,
    }),
  ).not.toBeInTheDocument();
  expect(
    await screen.findByRole("button", { name: "安装到伙伴" }),
  ).toBeEnabled();
  fireEvent.click(await screen.findByRole("button", { name: "编辑插件" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent("p"),
  );
});

it("switches catalog README and summary with the UI language without reusing the other locale query", async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/plugins/catalog/github"]}>
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<SkillRoute />}
          />
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("业务说明")).toBeVisible();
  await act(() => i18n.changeLanguage("en-US"));
  expect(await screen.findByText("Business guide")).toBeVisible();
  expect(screen.getByText("Manage repositories")).toBeVisible();
  expect(screen.queryByText("业务说明")).not.toBeInTheDocument();
});

it("refreshes the detail and its skill source pins after a background catalog change", async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/plugins/catalog/github"]}>
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/skills/:skillKey"
            element={<SkillRoute />}
          />
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await screen.findByText("业务说明");
  fixture.version = 2;
  act(() => {
    for (const listener of fixture.listeners)
      listener.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "plugins",
        type: "plugins.changed",
        resource: { kind: "global" },
        eventId: "catalog-update",
        transient: false,
        payload: {},
      });
  });
  await waitFor(() => expect(fixture.read).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole("button", { name: "查看技能 guide" }));
  expect(screen.getByLabelText("技能路由")).toHaveTextContent(
    `sourceCommit=${"2".repeat(40)}&archiveSHA256=${"2".repeat(64)}`,
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps known installed partners visible when their background refresh fails", async () => {
  fixture.installedError = true;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(["installed"], {
    resources: [
      {
        plugin: { id: "p", sourceKey: "catalog:github" },
        resolvedVersionId: "v1",
        installations: [{ wakerId: "w1", versionId: "v1", enabled: true }],
        partnerNames: { w1: "已知伙伴" },
      },
    ],
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/resources/plugins/catalog/github"]}>
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey"
            element={<PluginCatalogDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("link", { name: "已知伙伴" })).toBeVisible();
  await waitFor(() =>
    expect(client.getQueryState(["installed"])?.status).toBe("error"),
  );
  expect(screen.getByRole("link", { name: "已知伙伴" })).toBeVisible();
  expect(screen.getAllByText("已安装")).toHaveLength(2);
  expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
});
