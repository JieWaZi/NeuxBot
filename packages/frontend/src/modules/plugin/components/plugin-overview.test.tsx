import {
  render,
  screen,
  fireEvent,
  cleanup,
  within,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi, afterEach } from "vitest";
import "@/shared/i18n";
import { PluginOverview } from "./plugin-overview";
import type { PluginCatalogEntry } from "../model/plugin-view";
import { PluginReadme } from "./plugin-readme";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
const readmeFixture = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  connector: vi.fn(),
  probe: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  getPluginConnector: readmeFixture.connector,
  testPluginConnector: readmeFixture.probe,
  getCatalogPluginConnector: readmeFixture.connector,
  getPluginReadme: readmeFixture.load,
  getCatalogPluginReadme: readmeFixture.load,
}));
vi.mock("@/shared/platform", () => ({
  saveBlob: readmeFixture.save,
  openExternalURL: vi.fn(),
}));
vi.mock("./plugin-skill-dialog", () => ({
  PluginSkillDialog: ({ name }: { name: string }) => (
    <section aria-label="技能正文">{name}</section>
  ),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
const pkg: PluginCatalogEntry["package"] = {
  manifest: {
    name: "github",
    version: "1.0.1",
    author: { name: "GitHub" },
    license: "MIT",
  },
  presentation: {
    displayName: "GitHub",
    developerName: "GitHub",
    shortDescription: "Manage repositories",
  },
  upstream: { author: "GitHub", tag: "1.0.1" },
  skills: Array.from({ length: 7 }, (_, i) => ({
    key: `skill-${i}`,
    name: `skill-${i}`,
    description: `Skill ${i}`,
  })),
  servers: [
    { key: "github", type: "http", url: "https://api.githubcopilot.com/mcp/" },
  ],
};
it("shows plugin metadata and installed partners, and opens an included skill", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={pkg}
        skillContext={{ catalogKey: "github" }}
        detail={{
          installations: [{ wakerId: "w1", enabled: true, versionId: "v1" }],
          partnerNames: { w1: "开发伙伴" },
          versions: [{ id: "v1", version: "1.0.1" }],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("GitHub")).toBeVisible();
  expect(screen.getByText("MIT")).toBeVisible();
  expect(screen.getByRole("heading", { name: "已安装 1" })).toBeVisible();
  expect(screen.getByRole("link", { name: /开发伙伴/ })).toHaveAttribute(
    "href",
    "/wakers/w1/resources",
  );
  expect(
    screen.queryByRole("button", { name: "查看技能 skill-6" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "查看全部 7 项技能" }));
  fireEvent.click(screen.getByRole("button", { name: "查看技能 skill-6" }));
  expect(screen.getByLabelText("技能正文")).toHaveTextContent("skill-6");
});
it("keeps unknown installation state distinct from an empty installation list", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={pkg}
        installationStatus={<p role="status">正在读取安装伙伴</p>}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("status")).toHaveTextContent("正在读取安装伙伴");
  expect(screen.getByRole("heading", { name: "已安装" })).toBeVisible();
  expect(screen.queryByText("尚未安装到伙伴")).not.toBeInTheDocument();
});

it("hides absent capabilities and keeps link values as icons", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          manifest: {
            name: "empty",
            homepage: "https://example.com",
            repository: "https://example.com/repo",
          },
          skills: [],
          servers: [],
        }}
      />
    </MemoryRouter>,
  );
  expect(
    screen.queryByRole("heading", { name: /技能/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: /连接器/ }),
  ).not.toBeInTheDocument();
  expect(screen.getAllByText("官方网站")).toHaveLength(1);
  expect(screen.getAllByText("源代码")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "官方网站" })).toHaveTextContent(
    "",
  );
});

it("shows the same plugin metadata in partner scope without other partner installations", () => {
  render(
    <MemoryRouter>
      <PluginOverview pkg={pkg} scope="partner" />
    </MemoryRouter>,
  );
  expect(screen.getByRole("heading", { name: "插件信息" })).toBeVisible();
  expect(screen.getByText("GitHub")).toBeVisible();
  expect(screen.getByText("1.0.1")).toBeVisible();
  expect(screen.getByText("MIT")).toBeVisible();
  expect(screen.getByText("Manage repositories")).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: /^已安装/ }),
  ).not.toBeInTheDocument();
});

it("groups the explanation and plugin information before capabilities", () => {
  render(
    <MemoryRouter>
      <PluginOverview pkg={{ ...pkg, readme: "说明正文" }} />
    </MemoryRouter>,
  );
  expect(
    screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent),
  ).toEqual(["说明", "插件信息", "技能 7", "连接器 1", "已安装 0"]);
  expect(
    screen.queryByRole("heading", { name: "简介" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "插件说明" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("说明正文")).toBeVisible();
});

it("opens the complete long README and closes it when the source changes", async () => {
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(500);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(240);
  const readme =
    "# 完整说明\n\n| 能力 | 说明 |\n| --- | --- |\n| 检索 | 查询 |\n\n```text\n不可截断的结尾\n```";
  const view = render(
    <MemoryRouter>
      <PluginOverview pkg={{ ...pkg, readme }} />
    </MemoryRouter>,
  );
  fireEvent.click(await screen.findByRole("button", { name: "查看完整内容" }));
  const dialog = await screen.findByRole("dialog", { name: "完整说明" });
  expect(await within(dialog).findByText("不可截断的结尾")).toBeInTheDocument();
  view.rerender(
    <MemoryRouter>
      <PluginOverview pkg={{ ...pkg, readme: "另一个来源的说明" }} />
    </MemoryRouter>,
  );
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("keeps a short README inline without an unnecessary full-content action", () => {
  render(
    <MemoryRouter>
      <PluginOverview pkg={{ ...pkg, readme: "简短说明" }} />
    </MemoryRouter>,
  );
  expect(screen.getByText("简短说明")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "查看完整内容" }),
  ).not.toBeInTheDocument();
});

it("never presents an oversized README fallback as the complete README", () => {
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(500);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(240);
  render(<PluginReadme content="备用长简介" status="too-large" />);
  expect(screen.getByText("备用长简介")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "查看完整内容" }),
  ).not.toBeInTheDocument();
});

it("downloads the complete oversized README from its fixed version instead of showing a truncated dialog", async () => {
  readmeFixture.load.mockResolvedValue({
    data: { content: "原文".repeat(600_000) },
  });
  readmeFixture.save.mockResolvedValue(true);
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PluginReadme
        content="插件简介"
        status="too-large"
        source={{ pluginId: "p", versionId: "v-frozen" }}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "下载原始说明" }));
  await waitFor(() => expect(readmeFixture.save).toHaveBeenCalledOnce());
  expect(readmeFixture.load).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "p" },
      query: { versionId: "v-frozen", wakerId: undefined },
    }),
  );
  const blob = readmeFixture.save.mock.calls[0]?.[0];
  expect(blob.size).toBe(3_600_000);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps native implementation paths and digests out of the public plugin overview", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          manifest: { name: "logical", version: "2.0.0" },
          nativeComponents: [{ kind: "hooks", paths: ["hooks/hooks.json"] }],
          nativeImplementations: {
            codex: {
              path: "codex-plugins/github",
              name: "github",
              version: "0.1.12",
              format: "codex",
              digest: "a".repeat(64),
              components: [],
              skillCount: 0,
              serverCount: 1,
            },
            claude: {
              path: "claude-plugins/github",
              name: "github",
              version: "1.0.0",
              format: "claude",
              digest: "b".repeat(64),
              components: [],
              skillCount: 0,
              serverCount: 1,
            },
          },
        }}
      />
    </MemoryRouter>,
  );
  expect(
    screen.queryByRole("heading", { name: "原生组件" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("hooks/hooks.json")).not.toBeInTheDocument();
  expect(screen.queryByText("codex-plugins/github")).not.toBeInTheDocument();
  expect(screen.queryByText("claude-plugins/github")).not.toBeInTheDocument();
  expect(screen.queryByText("0.1.12")).not.toBeInTheDocument();
});

it("downloads the displayed locale from the same pinned catalog source", async () => {
  readmeFixture.load.mockClear();
  readmeFixture.load.mockResolvedValue({ data: { content: "中文完整说明" } });
  const source = {
    catalogKey: "office",
    sourceCommit: "a".repeat(40),
    archiveSHA256: "b".repeat(64),
    locale: "zh-CN",
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PluginReadme content="" status="too-large" source={source} />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "下载原始说明" }));
  await waitFor(() => expect(readmeFixture.load).toHaveBeenCalledOnce());
  expect(readmeFixture.load).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { key: "office" },
      query: {
        sourceCommit: source.sourceCommit,
        archiveSHA256: source.archiveSHA256,
        locale: "zh-CN",
      },
    }),
  );
});

it.each(["installed", "catalog"])(
  "opens the included MCP in a read-only dialog from %s",
  async (mode) => {
    readmeFixture.connector.mockClear();
    readmeFixture.connector.mockResolvedValue({
      data: {
        connector: {
          id: "github",
          name: "github",
          transport: "streamable_http",
          url: "https://api.githubcopilot.com/mcp/",
        },
        compatibility: [{ runtimeId: "codex", supported: true }],
      },
    });
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <PluginOverview
            pkg={pkg}
            skillContext={
              mode === "catalog"
                ? {
                    catalogKey: "github",
                    sourceCommit: "a".repeat(40),
                    archiveSHA256: "b".repeat(64),
                  }
                : { pluginId: "p", versionId: "v1" }
            }
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "查看连接器 github" }));
    const dialog = await screen.findByRole("dialog", { name: "github" });
    expect(
      await within(dialog).findByText("https://api.githubcopilot.com/mcp/"),
    ).toBeVisible();
    expect(
      within(dialog).getByText("Codex", { selector: "span" }),
    ).toBeVisible();
    expect(
      within(dialog).queryByRole("button", { name: "编辑" }),
    ).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("switch")).not.toBeInTheDocument();
    expect(readmeFixture.connector).toHaveBeenCalledWith(
      expect.objectContaining(
        mode === "catalog"
          ? {
              path: { key: "github" },
              query: {
                connectorKey: "github",
                sourceCommit: "a".repeat(40),
                archiveSHA256: "b".repeat(64),
              },
            }
          : {
              path: { pluginId: "p" },
              query: { versionId: "v1", key: "github", wakerId: undefined },
            },
      ),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  },
);

it("discovers tools from the exact authorized local version in a catalog modal", async () => {
  readmeFixture.probe.mockClear();
  readmeFixture.connector.mockResolvedValue({
    data: {
      connector: {
        id: "c",
        name: "github",
        transport: "streamable_http",
        testStatus: "untested",
      },
    },
  });
  readmeFixture.probe.mockImplementation(async () => {
    const result = {
      data: {
        connector: {
          id: "c",
          name: "github",
          transport: "streamable_http",
          testStatus: "succeeded",
          testedAt: Date.now(),
          testToolCount: 1,
          testTools: [{ name: "list_repositories", description: "列出仓库" }],
        },
      },
    };
    readmeFixture.connector.mockResolvedValue(result);
    return result;
  });
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <PluginOverview
          pkg={pkg}
          skillContext={{
            catalogKey: "github",
            sourceCommit: "a".repeat(40),
            archiveSHA256: "b".repeat(64),
          }}
          detail={{
            plugin: { id: "p" },
            versions: [
              {
                id: "local-v",
                provenance: {
                  sourceCommit: "a".repeat(40),
                  archiveSHA256: "b".repeat(64),
                },
              },
            ],
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "查看连接器 github" }));
  const dialog = await screen.findByRole("dialog", { name: "github" });
  fireEvent.click(
    await within(dialog).findByRole("button", { name: "测试连接" }),
  );
  expect(
    await within(dialog).findByRole("heading", { name: "list_repositories" }),
  ).toBeVisible();
  expect(readmeFixture.probe).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "p" },
      query: expect.objectContaining({
        versionId: "local-v",
        key: "github",
        sourceCommit: "a".repeat(40),
        archiveSHA256: "b".repeat(64),
      }),
    }),
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "查看连接器 github" }));
  expect(
    await screen.findByRole("heading", { name: "list_repositories" }),
  ).toBeVisible();
  expect(readmeFixture.probe).toHaveBeenCalledTimes(1);
});
