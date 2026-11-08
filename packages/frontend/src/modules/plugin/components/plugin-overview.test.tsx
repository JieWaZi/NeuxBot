import {
  render,
  act,
  screen,
  fireEvent,
  cleanup,
  within,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { expect, it, vi, afterEach } from "vitest";
import { i18n } from "@/shared/i18n";
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
  open: vi.fn().mockResolvedValue(undefined),
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
  openExternalURL: readmeFixture.open,
}));
function CurrentRoute() {
  const location = useLocation();
  return (
    <output aria-label="当前路由">
      {location.pathname}
      {location.search}
    </output>
  );
}
afterEach(async () => {
  cleanup();
  vi.restoreAllMocks();
  await i18n.changeLanguage("zh-CN");
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
      <CurrentRoute />
      <PluginOverview
        pkg={pkg}
        skillContext={{
          catalogKey: "github",
          sourceCommit: "a".repeat(40),
          archiveSHA256: "b".repeat(64),
        }}
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
  expect(screen.getByRole("heading", { name: "已安装" })).toBeVisible();
  expect(
    screen
      .getAllByRole("heading", { level: 2 })
      .map((item) => item.textContent),
  ).toEqual(["已安装", "连接与设置", "技能", "插件信息"]);
  expect(screen.getAllByRole("link", { name: /开发伙伴/ })).toHaveLength(1);
  expect(screen.queryByText("https://api.githubcopilot.com")).toBeNull();
  expect(screen.getByRole("link", { name: /开发伙伴/ })).toHaveAttribute(
    "href",
    "/wakers/w1/resources",
  );
  expect(
    screen.getByRole("button", { name: "查看技能 skill-6" }),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "查看技能 skill-6" }));
  expect(screen.getByLabelText("当前路由")).toHaveTextContent(
    `/resources/plugins/catalog/github/skills/skill-6?sourceCommit=${"a".repeat(40)}&archiveSHA256=${"b".repeat(64)}`,
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
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

it("hides absent capabilities and names public links by their real host", () => {
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
  expect(
    screen.queryByRole("heading", { name: /^已安装/ }),
  ).not.toBeInTheDocument();
  expect(screen.getAllByText("官方网站")).toHaveLength(1);
  expect(screen.getAllByText("源代码")).toHaveLength(1);
  expect(screen.getByRole("link", { name: "官方网站" })).toHaveTextContent(
    "example.com",
  );
});

it.each([
  ["zh-CN", "上游版本", "插件版本"],
  ["en-US", "Upstream version", "Plugin version"],
])(
  "%s identifies the upstream release without relabeling it as the package version",
  async (locale, label, packageLabel) => {
    await i18n.changeLanguage(locale);
    render(
      <MemoryRouter>
        <PluginOverview
          pkg={{
            ...pkg,
            manifest: { ...pkg?.manifest, name: "github", version: "0.1.0" },
            upstream: { tag: "v1.0.98" },
          }}
        />
      </MemoryRouter>,
    );
    const field = screen.getByText(label);
    expect(field.parentElement).toHaveTextContent("v1.0.98");
    expect(screen.queryByText(packageLabel)).not.toBeInTheDocument();
    expect(screen.getAllByText("v1.0.98")).toHaveLength(1);
  },
);

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

it("keeps the README and public fields together in the final information section", () => {
  render(
    <MemoryRouter>
      <PluginOverview pkg={{ ...pkg, readme: "说明正文" }} />
    </MemoryRouter>,
  );
  expect(
    screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent),
  ).toEqual(["连接与设置", "技能", "插件信息"]);
  expect(
    screen.queryByRole("heading", { name: "简介" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "插件说明" }),
  ).not.toBeInTheDocument();
  const information = screen
    .getByRole("heading", { name: "插件信息" })
    .closest("section");
  expect(information).not.toBeNull();
  expect(information).toContainElement(screen.getByText("说明正文"));
  expect(information).toContainElement(screen.getByText("MIT"));
  expect(
    screen.queryByRole("heading", { name: "说明" }),
  ).not.toBeInTheDocument();
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
  const expand = await screen.findByRole("button", { name: "查看完整内容" });
  const informationHeader = screen
    .getByRole("heading", { name: "插件信息" })
    .closest("header");
  expect(informationHeader).toContainElement(expand);
  expect(screen.getAllByRole("button", { name: "查看完整内容" })).toHaveLength(
    1,
  );
  fireEvent.click(expand);
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

it("keeps standard MCP startup arguments out of the public plugin overview", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          manifest: { name: "logical" },
          servers: [
            {
              key: "tools",
              type: "stdio",
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText("private/start.mjs")).toBeNull();
  expect(screen.queryByText(/--token/)).toBeNull();
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
  "opens included MCP as a pinned detail page from %s",
  (mode) => {
    render(
      <MemoryRouter initialEntries={["/resources/plugins/p?version=v1"]}>
        <CurrentRoute />
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
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "查看连接器 github" }));
    const route = screen.getByRole("status", { name: "当前路由" });
    expect(route).toHaveTextContent(
      mode === "catalog"
        ? `/resources/plugins/catalog/github/connectors/github?sourceCommit=${"a".repeat(40)}&archiveSHA256=${"b".repeat(64)}`
        : "/resources/plugins/p/connectors/github?version=v1",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(readmeFixture.probe).not.toHaveBeenCalled();
  },
);

it("keeps the matching local version in a catalog connector detail link", () => {
  render(
    <MemoryRouter>
      <CurrentRoute />
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
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "查看连接器 github" }));
  expect(screen.getByRole("status", { name: "当前路由" })).toHaveTextContent(
    "&pluginId=p&version=local-v",
  );
});

it("uses a connection section with the cached unverified account and a management action", () => {
  const edit = vi.fn();
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          integration: {
            authentication: {
              strategy: "plugin-command",
              protocol: "neuxbot-auth/1",
            },
          },
        }}
        detail={{ plugin: { id: "p" }, account: { available: false } }}
        onEdit={edit}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("heading", { name: "连接与设置" })).toBeVisible();
  expect(screen.getByText("尚未核验账号")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "编辑插件" }));
  expect(edit).toHaveBeenCalledOnce();
});
it("does not create an empty management section for a rename-only package", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{ manifest: { name: "simple" } }}
        detail={{ plugin: { id: "p" } }}
        onEdit={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByRole("heading", { name: "管理" })).toBeNull();
  expect(screen.queryByRole("heading", { name: "连接与设置" })).toBeNull();
  expect(screen.queryByRole("button", { name: "编辑插件" })).toBeNull();
});

it("keeps localized setup instructions in the connection card and its support target only in information", async () => {
  readmeFixture.open.mockClear();
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          integration: {
            authentication: {
              strategy: "plugin-command",
              protocol: "neuxbot-auth/1",
            },
          },
          presentation: {
            supportURL: "https://support.github.com/",
            setup: {
              instructions: {
                "zh-CN": "中文连接步骤。",
                en: "English connection steps.",
              },
              helpURL: "https://support.github.com/",
            },
          },
        }}
        onEdit={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(await screen.findByText("中文连接步骤。")).toBeVisible();
  const connection = screen
    .getByRole("heading", { name: "连接与设置" })
    .closest("section");
  expect(connection).not.toBeNull();
  if (!connection) throw new Error("Missing connection section");
  expect(connection).toContainElement(screen.getByText("中文连接步骤。"));
  expect(within(connection).queryByRole("link")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "连接说明" }),
  ).not.toBeInTheDocument();
  const support = screen.getByRole("link", { name: "支持" });
  expect(screen.getAllByText("support.github.com")).toHaveLength(1);
  expect(
    screen.getByRole("heading", { name: "插件信息" }).closest("section"),
  ).toContainElement(support);
  fireEvent.click(support);
  await waitFor(() =>
    expect(readmeFixture.open).toHaveBeenCalledWith(
      "https://support.github.com/",
    ),
  );
  expect(screen.queryByRole("heading", { name: "账号连接" })).toBeNull();
  expect(screen.queryByRole("button", { name: "编辑插件" })).toBeNull();
  expect(screen.queryByText("尚未核验账号")).toBeNull();
  await act(() => i18n.changeLanguage("en-US"));
  expect(await screen.findByText("English connection steps.")).toBeVisible();
  expect(screen.queryByText("中文连接步骤。")).toBeNull();
});
it("does not create a connection card for a setup help URL alone", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          presentation: {
            setup: { helpURL: "https://support.github.com/" },
          },
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByRole("heading", { name: "连接与设置" })).toBeNull();
  expect(screen.queryByRole("link")).toBeNull();
});
it("falls back to English setup inside the existing connection card", async () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          presentation: {
            setup: {
              instructions: { en: "Prepare the account before connecting." },
            },
          },
        }}
      />
    </MemoryRouter>,
  );
  expect(
    await screen.findByText("Prepare the account before connecting."),
  ).toBeVisible();
  expect(screen.getAllByRole("heading", { name: "连接与设置" })).toHaveLength(
    1,
  );
  expect(screen.queryByRole("heading", { name: "连接说明" })).toBeNull();
});
it("merges MCP and account management into one connection card without changing readonly actions", () => {
  const edit = vi.fn();
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          integration: {
            authentication: {
              strategy: "plugin-command",
              protocol: "neuxbot-auth/1",
            },
          },
          servers: [{ key: "tools", url: "https://tools.example.com/mcp" }],
        }}
        detail={{
          plugin: { id: "p" },
          account: { checkedAt: 1, available: true, name: "测试用户" },
        }}
        skillContext={{ pluginId: "p", versionId: "fixed" }}
        onEdit={edit}
      />
    </MemoryRouter>,
  );
  const heading = screen.getByRole("heading", { name: "连接与设置" });
  const section = heading.closest("section");
  expect(screen.getAllByRole("heading", { name: "连接与设置" })).toHaveLength(
    1,
  );
  expect(screen.queryByRole("heading", { name: "连接器" })).toBeNull();
  expect(section).toContainElement(
    screen.getByRole("button", { name: "查看连接器 tools" }),
  );
  expect(screen.getByText("已连接：测试用户")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "编辑插件" }));
  expect(edit).toHaveBeenCalledOnce();
});
it("omits author capability statements from information", () => {
  readmeFixture.probe.mockClear();
  readmeFixture.connector.mockClear();
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          presentation: { capabilities: ["Search notes", "Create summaries"] },
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText("作者能力说明")).not.toBeInTheDocument();
  expect(screen.queryByText("Search notes")).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "连接与设置" })).toBeNull();
  expect(screen.queryByRole("button")).toBeNull();
  expect(readmeFixture.probe).not.toHaveBeenCalled();
  expect(readmeFixture.connector).not.toHaveBeenCalled();
});

it("shows repository identity beyond its hosting domain", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          manifest: { repository: "https://github.com/openai/skills.git" },
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link", { name: "源代码" })).toHaveTextContent(
    "openai/skills",
  );
});

it("keeps one source link when the website and repository point to the same repo", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{
          manifest: { repository: "https://github.com/larksuite/cli" },
          presentation: {
            websiteURL: "https://github.com/larksuite/cli/",
            supportURL: "https://github.com/larksuite/cli/issues",
          },
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link", { name: "源代码" })).toHaveTextContent(
    "larksuite/cli",
  );
  expect(
    screen.queryByRole("link", { name: "官方网站" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "支持" })).toHaveTextContent(
    "larksuite/cli/issues",
  );
});

it("uses the public configuration availability flag for the edit action", () => {
  const edit = vi.fn();
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{ manifest: { name: "configured" }, hasConfiguration: true }}
        detail={{ plugin: { id: "p" } }}
        onEdit={edit}
      />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "编辑插件" }));
  expect(edit).toHaveBeenCalledOnce();
});
