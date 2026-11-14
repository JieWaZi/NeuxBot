import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import type { ReactNode } from "react";
import { i18n } from "@/shared/i18n";
import { AppSceneWorkspaceContextProvider } from "@/shared/workspace/app-scene-workspace";
import { PluginPresentation } from "./plugin-presentation";
import type { PluginCatalogEntry, PluginDetail } from "../model/plugin-view";

const state = vi.hoisted(() => ({
  installed: true,
  enabled: true,
  versionId: "v1",
  sourceKey: "catalog:preview",
  sourceCommit: "commit-one",
  archiveSHA256: "archive-one",
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginOptions: () => ({
    queryKey: ["example-plugin"],
    queryFn: async () => ({
      plugin: { id: "p1", sourceKey: state.sourceKey },
      resolvedVersionId: state.versionId,
      versions: [
        {
          id: state.versionId,
          provenance: {
            sourceCommit: state.sourceCommit,
            archiveSHA256: state.archiveSHA256,
          },
        },
      ],
      installations: state.installed
        ? [
            {
              wakerId: "w1",
              enabled: state.enabled,
              versionId: state.versionId,
            },
          ]
        : [],
    }),
  }),
  listWakersOptions: () => ({
    queryKey: ["example-partners"],
    queryFn: async () => ({
      resources: [{ id: "w1", name: "伙伴一" }],
      totalSize: 1,
    }),
  }),
  getWakerOptions: () => ({
    queryKey: ["example-partner"],
    queryFn: async () => ({ id: "w1", name: "伙伴一" }),
  }),
}));
vi.mock("./deferred-plugin-dialogs", () => ({
  PluginInstallDialog: ({ onClose }: { onClose: () => void }) => (
    <button onClick={onClose}>取消安装</button>
  ),
}));
function RetainedScene({ children }: { children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const active = location.pathname === "/resources/plugins/p1";
  return (
    <>
      <div data-testid="retained-plugin-page" hidden={!active}>
        <AppSceneWorkspaceContextProvider
          value={{
            routeSceneId: "plugin",
            snapshot: { activeTabId: active ? "plugin" : undefined, tabs: [] },
            dispatch: () => {},
            syncWorkspace: () => {},
          }}
        >
          {children}
        </AppSceneWorkspaceContextProvider>
      </div>
      <Routes>
        <Route path="/chat" element={<div>聊天草稿页</div>} />
        <Route
          path="/wakers/new"
          element={<button onClick={() => navigate(-1)}>取消创建</button>}
        />
        <Route path="*" element={null} />
      </Routes>
      <Location />
    </>
  );
}

function Location() {
  const location = useLocation();
  return (
    <output data-testid="destination">
      {JSON.stringify({
        path: location.pathname + location.search,
        state: location.state,
      })}
    </output>
  );
}
afterEach(() => {
  cleanup();
  state.installed = true;
  state.enabled = true;
  state.versionId = "v1";
  state.sourceKey = "catalog:preview";
  state.sourceCommit = "commit-one";
  state.archiveSHA256 = "archive-one";
});
const pkg: PluginCatalogEntry["package"] = {
  presentation: {
    defaultPrompt: ["Review changes"],
    defaultPromptLocalizations: { "zh-CN": ["检查变更"] },
  },
};
function mount(
  wakerId = "",
  installed = false,
  packageData = pkg,
  sourceKey?: string,
  overrides: Partial<PluginDetail> = {},
  catalog?: PluginCatalogEntry,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const detail: PluginDetail = {
    plugin: { id: "p1", sourceKey },
    resolvedVersionId: "v1",
    installations: installed
      ? [{ wakerId: "w1", enabled: true, versionId: "v1" }]
      : [],
    ...overrides,
  };
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/resources/plugins/p1"]}>
        <RetainedScene>
          <PluginPresentation
            pkg={packageData}
            detail={detail}
            catalog={catalog}
            wakerId={wakerId}
          />
        </RetainedScene>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it("uses the product prompt for a local plugin without an author example and carries its installed reference into a fresh draft", () => {
  mount(
    "w1",
    true,
    {
      manifest: { name: "skill-bundle-local" },
      presentation: { displayName: "Orbit" },
    },
    "local-skills:p1",
  );
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个插件。" }));
  const destination = JSON.parse(
    screen.getByTestId("destination").textContent ?? "{}",
  );
  expect(destination.path).toMatch(/^\/chat\?draftId=.+&wakerId=w1$/);
  expect(destination.state.chatDraftIntent).toMatchObject({
    text: "帮我使用这个插件。",
    wakerId: "w1",
    pluginId: "p1",
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps an author default prompt ahead of the product fallback when its localized list is empty", () => {
  mount("", false, {
    presentation: {
      defaultPrompt: ["作者示例"],
      defaultPromptLocalizations: { "zh-CN": [] },
    },
  });
  expect(screen.getByRole("button", { name: "作者示例" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "帮我使用这个插件。" }),
  ).not.toBeInTheDocument();
});

it("localizes the product fallback without writing a prompt into the package", async () => {
  const empty: PluginCatalogEntry["package"] = {
    presentation: { defaultPrompt: [] },
  };
  await i18n.changeLanguage("en-US");
  try {
    mount("", false, empty);
    expect(
      screen.getByRole("button", { name: "Help me use this plugin." }),
    ).toBeVisible();
    expect(empty.presentation?.defaultPrompt).toEqual([]);
  } finally {
    await i18n.changeLanguage("zh-CN");
  }
});
it("current enabled installation fills a fresh draft with its plugin reference", () => {
  mount("w1", true);
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  const value = JSON.parse(
    screen.getByTestId("destination").textContent ?? "{}",
  );
  expect(value.path).toMatch(/^\/chat\?draftId=.+&wakerId=w1$/);
  expect(value.state.chatDraftIntent).toMatchObject({
    text: "检查变更",
    wakerId: "w1",
    pluginId: "p1",
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("global example requires a partner and uses its actual enabled relation", async () => {
  mount();
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
  fireEvent.click(await screen.findByRole("radio", { name: "伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(
    JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state
      .chatDraftIntent.pluginId,
  ).toBe("p1");
});
it("uninstalled example offers installation but cannot create a draft until it has a matching enabled relation", async () => {
  state.installed = false;
  mount();
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  fireEvent.click(await screen.findByRole("radio", { name: "伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "安装" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "安装" }));
  fireEvent.click(screen.getByRole("button", { name: "取消安装" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "安装" })).toBeEnabled(),
  );
  expect(screen.getByRole("radio", { name: "伙伴一" })).toBeChecked();
  const submit = screen.getByRole("button", { name: "继续填写草稿" });
  expect(submit).toBeDisabled();
  fireEvent.click(submit);
  expect(
    JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state,
  ).toBeNull();
});
it("opening and cancelling partner creation retains the example", async () => {
  mount();
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  fireEvent.click(screen.getByRole("button", { name: "创建伙伴" }));
  const cancel = await screen.findByRole("button", { name: "取消创建" });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  fireEvent.click(cancel);
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(screen.getAllByText("检查变更")).toHaveLength(2);
});

it("closes its partner dialog when navigation hides the retained scene", async () => {
  mount();
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  fireEvent.click(await screen.findByRole("radio", { name: "伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(screen.getByText("聊天草稿页")).toBeVisible();
  expect(screen.getByTestId("retained-plugin-page")).toBeInTheDocument();
  expect(screen.getByTestId("retained-plugin-page")).not.toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it.each([
  { enabled: false, versionId: "v1" },
  { enabled: true, versionId: "v2" },
])(
  "does not attach a disabled or different partner version (%j)",
  async (relation) => {
    state.enabled = relation.enabled;
    state.versionId = relation.versionId;
    mount("w1", true, pkg, undefined, {
      installations: [{ wakerId: "w1", ...relation }],
    });
    fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
    await screen.findByRole("radio", { name: "伙伴一" });
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
    expect(screen.getByRole("dialog")).toBeVisible();
  },
);

it.each([
  {
    sourceKey: "catalog:other",
    sourceCommit: "commit-one",
    archiveSHA256: "archive-one",
  },
  {
    sourceKey: "catalog:preview",
    sourceCommit: "commit-two",
    archiveSHA256: "archive-one",
  },
  {
    sourceKey: "catalog:preview",
    sourceCommit: "commit-one",
    archiveSHA256: "archive-two",
  },
])(
  "does not reference a local installation with different catalog provenance (%j)",
  async (provenance) => {
    Object.assign(state, provenance);
    mount(
      "",
      false,
      pkg,
      "catalog:preview",
      {},
      {
        key: "preview",
        sourceKey: "catalog:preview",
        sourceCommit: "commit-one",
        archiveSHA256: "archive-one",
        package: pkg,
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
    fireEvent.click(await screen.findByRole("radio", { name: "伙伴一" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "安装" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
    expect(
      JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state,
    ).toBeNull();
  },
);

it("references a catalog preview only after matching its local source and immutable digest", async () => {
  mount(
    "",
    false,
    pkg,
    "catalog:preview",
    {},
    {
      key: "preview",
      sourceKey: "catalog:preview",
      sourceCommit: "commit-one",
      archiveSHA256: "archive-one",
      package: pkg,
    },
  );
  fireEvent.click(screen.getByRole("button", { name: "检查变更" }));
  fireEvent.click(await screen.findByRole("radio", { name: "伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(
    JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state
      .chatDraftIntent.pluginId,
  ).toBe("p1");
});

it("ignores empty author prompts before using the localized product fallback", () => {
  mount("", false, {
    presentation: {
      defaultPrompt: [" "],
      defaultPromptLocalizations: { "zh-CN": ["\n"] },
    },
  });
  expect(
    screen.getByRole("button", { name: "帮我使用这个插件。" }),
  ).toBeVisible();
});
