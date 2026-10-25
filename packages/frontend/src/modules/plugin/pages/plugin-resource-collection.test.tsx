import {
  act,
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ResourcePageSearchField } from "@/shared/ui/resource-page";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { it, expect, vi, afterEach, beforeEach } from "vitest";
import "@/shared/i18n";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { listPluginCatalog } from "@/shared/api/generated/sdk.gen";
import type * as PluginQueries from "../api/plugin-queries";
import type * as ResourceSdk from "@/shared/api/generated/sdk.gen";
import type { PluginDetail } from "../model/plugin-view";
import { ApiError } from "@/shared/api/errors";
import { PluginResourceCollection } from "./plugin-resource-collection";

const fixture = vi.hoisted(() => ({
  refresh: vi.fn(),
  fetchedAt: 0,
  available: true,
  sourceKey: "catalog:office",
  entrySourceKey: undefined as string | undefined,
  sourceState: "available",
  displayName: "飞书办公",
  listeners: new Set<GlobalEventObserver>(),
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
vi.mock("../api/plugin-queries", async (original) => ({
  ...(await original<typeof PluginQueries>()),
  refreshPluginCatalogMutation: () => ({ mutationFn: fixture.refresh }),
}));
vi.mock("../api/use-plugin-resources", () => ({
  usePluginResources: () => ({
    data: [
      {
        plugin: { id: "existing", sourceKey: fixture.sourceKey },
        account: { available: fixture.available, checkedAt: 123 },
        installations: [{ wakerId: "w1", versionId: "v1" }],
      },
    ],
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof ResourceSdk>()),
  listPlugins: vi.fn(
    async (options?: { query?: { installedOnly?: boolean } }) => {
      const installed = {
        plugin: { id: "installed", name: "实际安装插件" },
        installations: [{ wakerId: "w1", versionId: "v1", enabled: false }],
      };
      const resources = options?.query?.installedOnly
        ? [installed]
        : [
            installed,
            {
              plugin: { id: "prepared", name: "尚未安装插件" },
              installations: [],
            },
          ];
      return {
        data: {
          resources,
          totalSize: resources.length,
          catalogSource: {
            state: fixture.sourceState,
            fetchedAt: fixture.fetchedAt,
          },
        },
      };
    },
  ),
  listPluginCatalog: vi.fn(async () => ({
    data: {
      source: { state: fixture.sourceState, fetchedAt: fixture.fetchedAt },
      entries: [
        {
          key: "office",
          sourceKey: fixture.entrySourceKey,
          package: {
            manifest: {
              name: "office",
              extensions: { "com.ally": { provider: "feishu" } },
            },
            presentation: { displayName: fixture.displayName },
          },
        },
      ],
      totalSize: 1,
    },
  })),
}));
vi.mock("../components/plugin-install-dialog", () => ({
  PluginInstallDialog: ({
    detail,
    authorizeOnly,
  }: {
    detail?: PluginDetail;
    authorizeOnly?: boolean;
  }) => (
    <div role="dialog">
      {detail?.plugin?.id}:{detail?.installations?.[0]?.wakerId}:
      {authorizeOnly ? "reauthorize" : "install"}
    </div>
  ),
}));
afterEach(() => {
  cleanup();
  fixture.listeners.clear();
});
beforeEach(() => {
  vi.clearAllMocks();
  fixture.displayName = "飞书办公";
  fixture.available = true;
  fixture.sourceKey = "catalog:office";
  fixture.entrySourceKey = undefined;
  fixture.sourceState = "available";
  fixture.fetchedAt = 0;
  fixture.refresh.mockResolvedValue({ success: true });
});

function showCatalog(withSearch = false) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PluginResourceCollection
          search=""
          view="catalog"
          onViewChange={() => {}}
          searchField={
            withSearch ? (
              <ResourcePageSearchField
                aria-label="搜索插件"
                placeholder="搜索插件…"
              />
            ) : undefined
          }
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

it("does not merge the same catalog key from another source into an installed resource", async () => {
  fixture.entrySourceKey = "catalog:another-source:office";
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "安装 飞书办公" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent("::install"),
  );
});

it("keeps cached catalog cards visible and explains a stale source", async () => {
  fixture.sourceState = "stale";
  showCatalog();
  expect(
    await screen.findByText(
      "插件库暂未连接，当前显示缓存内容，可能不是最新版本。",
    ),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "安装 飞书办公" })).toBeVisible();
});
it.each([
  [true, "安装 飞书办公", "install"],
  [false, "授权 飞书办公", "reauthorize"],
] as const)(
  "uses the existing API snapshot when opening a catalog action (%s)",
  async (available, label, mode) => {
    fixture.available = available;
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <PluginResourceCollection
            search=""
            view="catalog"
            onViewChange={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: label }));
    expect(screen.getByRole("dialog")).toHaveTextContent(`existing:w1:${mode}`);
  },
);

it("refreshes the active catalog when a background catalog update emits plugins.changed", async () => {
  showCatalog();
  expect(
    await screen.findByRole("button", { name: "安装 飞书办公" }),
  ).toBeVisible();
  expect(listPluginCatalog).toHaveBeenCalledTimes(1);
  fixture.displayName = "更新后的插件";
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
  await waitFor(() => expect(listPluginCatalog).toHaveBeenCalledTimes(2));
  expect(
    await screen.findByRole("button", { name: "安装 更新后的插件" }),
  ).toBeVisible();
});

it("shows installed resources and the installed total in my plugins", async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PluginResourceCollection
          search=""
          view="mine"
          onViewChange={() => {}}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("实际安装插件")).toBeVisible();
  expect(screen.queryByText("尚未安装插件")).not.toBeInTheDocument();
  expect(screen.getByText("1 项资源")).toBeVisible();
});

it("checks the catalog explicitly and refreshes active queries without removing installed cards", async () => {
  let finish: (() => void) | undefined;
  fixture.refresh.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PluginResourceCollection
          search=""
          view="mine"
          onViewChange={() => {}}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("实际安装插件")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "插件库更新信息" }));
  fireEvent.click(await screen.findByRole("button", { name: "检查更新" }));
  expect(
    await screen.findByRole("button", { name: "正在检查更新…" }),
  ).toBeDisabled();
  expect(screen.getByText("实际安装插件")).toBeVisible();
  await act(async () => finish?.());
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "检查更新" })).toBeEnabled(),
  );
});
it("preserves installed cards and shows a failed source after checking fails", async () => {
  fixture.sourceState = "stale";
  fixture.refresh.mockRejectedValueOnce(new ApiError("目录暂不可用"));
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PluginResourceCollection
          search=""
          view="mine"
          onViewChange={() => {}}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("实际安装插件")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "插件库更新信息" }));
  fireEvent.click(await screen.findByRole("button", { name: "检查更新" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("目录暂不可用");
  expect(screen.getByText("实际安装插件")).toBeVisible();
  expect(
    screen.getByText("插件库暂未连接，当前显示缓存内容，可能不是最新版本。"),
  ).toBeVisible();
  expect(screen.queryByText("已完成插件库检查。")).not.toBeInTheDocument();
});

it("shows only an info icon before search and keeps refresh inside the interactive hover panel", async () => {
  fixture.fetchedAt = Date.UTC(2026, 9, 19, 0, 0, 0);
  const user = userEvent.setup();
  showCatalog(true);
  await screen.findByRole("button", { name: "安装 飞书办公" });
  const info = screen.getByRole("button", { name: "插件库更新信息" });
  const search = screen.getByRole("searchbox", { name: "搜索插件" });
  expect(
    info.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(info.textContent).toBe("");
  expect(
    screen.queryByRole("button", { name: "检查更新" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/上次成功检查：/)).not.toBeInTheDocument();
  await user.hover(info);
  const panel = await screen.findByRole("dialog", { name: "插件库更新信息" });
  expect(panel).toHaveTextContent(
    `上次成功检查：${new Date(fixture.fetchedAt).toLocaleString()}`,
  );
  const refresh = screen.getByRole("button", { name: "检查更新" });
  expect(panel).toContainElement(refresh);
  expect(refresh.textContent).toBe("");
  await user.hover(refresh);
  await new Promise((resolve) => setTimeout(resolve, 200));
  expect(panel).toBeVisible();
  await user.click(refresh);
  await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
});
it.each(["{ArrowDown}", "{Enter}", " "])(
  "opens plugin update information by focus and supports keyboard refresh and Escape (%s)",
  async (key) => {
    const user = userEvent.setup();
    showCatalog(true);
    await screen.findByRole("button", { name: "安装 飞书办公" });
    const info = screen.getByRole("button", { name: "插件库更新信息" });
    expect(screen.queryByText("尚未核验插件库更新")).not.toBeInTheDocument();
    act(() => info.focus());
    expect(
      await screen.findByRole("dialog", { name: "插件库更新信息" }),
    ).toHaveTextContent("尚未核验插件库更新");
    await user.keyboard(key);
    const refresh = screen.getByRole("button", { name: "检查更新" });
    expect(refresh).toHaveFocus();
    expect(fixture.refresh).not.toHaveBeenCalled();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "插件库更新信息" }),
      ).not.toBeInTheDocument(),
    );
    expect(info).toHaveFocus();
    await user.click(info);
    expect(
      await screen.findByRole("dialog", { name: "插件库更新信息" }),
    ).toBeVisible();
  },
);

it("closes a hover-opened information panel after the pointer leaves its whole area", async () => {
  const user = userEvent.setup();
  showCatalog(true);
  await screen.findByRole("button", { name: "安装 飞书办公" });
  const info = screen.getByRole("button", { name: "插件库更新信息" });
  await user.hover(info);
  const panel = await screen.findByRole("dialog", { name: "插件库更新信息" });
  const refresh = screen.getByRole("button", { name: "检查更新" });
  await user.hover(refresh);
  expect(panel).toBeVisible();
  await user.unhover(refresh);
  await waitFor(() => expect(panel).not.toBeInTheDocument());
});
