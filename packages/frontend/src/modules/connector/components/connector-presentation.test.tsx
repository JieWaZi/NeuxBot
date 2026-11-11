import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { ConnectorPresentation } from "./connector-presentation";

const state = vi.hoisted(() => ({
  installed: true,
  enabled: true,
  connectorId: "c1",
  policy: "per_execution" as string | undefined,
  version: "v1",
  packageStatus: "available" as string | undefined,
  error: false,
}));
const connectorRead = vi.hoisted(() => vi.fn());
const pluginRead = vi.hoisted(() => vi.fn());
vi.mock("@/modules/plugin", () => ({
  PartnerPicker: ({ onChange }: { onChange: (ids: string[]) => void }) => (
    <button onClick={() => onChange(["w1"])}>选择伙伴一</button>
  ),
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listWakerConnectorsOptions: (options: unknown) => ({
    queryKey: ["try-connectors", options],
    queryFn: async () => {
      connectorRead(options);
      if (state.error) throw new Error("offline");
      return state.installed
        ? [
            {
              enabled: state.enabled,
              connector: {
                id: state.connectorId,
                activationPolicy: state.policy,
              },
            },
          ]
        : [];
    },
  }),
  getPluginOptions: (options: unknown) => ({
    queryKey: ["try-plugin", options],
    queryFn: async () => {
      pluginRead(options);
      return {
        packageStatus: state.packageStatus,
        installations: state.installed
          ? [
              {
                wakerId: "w1",
                enabled: state.enabled,
                versionId: state.version,
              },
            ]
          : [],
      };
    },
  }),
}));

function RetainedScene({ children }: { children: ReactNode }) {
  const location = useLocation();
  return (
    <>
      <div
        data-testid="retained-connector-page"
        hidden={location.pathname === "/chat"}
      >
        {children}
      </div>
      <Routes>
        <Route path="/chat" element={<p>聊天草稿页</p>} />
        <Route path="*" element={null} />
      </Routes>
      <output data-testid="destination">
        {JSON.stringify({
          path: location.pathname + location.search,
          state: location.state,
        })}
      </output>
    </>
  );
}
function mount(props: Parameters<typeof ConnectorPresentation>[0]) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/connectors/c1"]}>
        <RetainedScene>
          <ConnectorPresentation {...props} />
        </RetainedScene>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function destination() {
  return JSON.parse(screen.getByTestId("destination").textContent ?? "{}");
}
async function selectPartner() {
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个连接器。" }));
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
}
afterEach(() => {
  cleanup();
  Object.assign(state, {
    installed: true,
    enabled: true,
    connectorId: "c1",
    policy: "per_execution",
    version: "v1",
    packageStatus: "available",
    error: false,
  });
  connectorRead.mockClear();
  pluginRead.mockClear();
});

it("uses the shared product TryCard and creates a connector-referenced draft for the current verified partner", () => {
  mount({
    name: "研究服务",
    context: { kind: "connector", connectorId: "c1" },
    wakerId: "w1",
    usable: true,
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个连接器。" }));
  expect(destination().path).toMatch(/^\/chat\?draftId=.+&wakerId=w1$/);
  expect(destination().state.chatDraftIntent).toMatchObject({
    wakerId: "w1",
    text: "帮我使用这个连接器。",
    connectorId: "c1",
  });
  expect(destination().state.chatDraftIntent.pluginId).toBeUndefined();
  expect(connectorRead).not.toHaveBeenCalled();
});

it("queries only the explicitly selected partner before carrying a connector reference", async () => {
  mount({
    name: "研究服务",
    context: { kind: "connector", connectorId: "c1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个连接器。" }));
  expect(connectorRead).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  expect(connectorRead).toHaveBeenCalledWith(
    expect.objectContaining({ path: { wakerId: "w1" } }),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(destination().state.chatDraftIntent.connectorId).toBe("c1");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByTestId("retained-connector-page")).not.toBeVisible();
});

it.each([
  "missing",
  "disabled",
  "wrong identity",
  "team_context",
  "workflow_context",
])("cannot carry a false connector reference: %s", async (reason) => {
  state.installed = reason !== "missing";
  state.enabled = reason !== "disabled";
  state.connectorId = reason === "wrong identity" ? "another" : "c1";
  state.policy = reason.endsWith("_context") ? reason : "per_execution";
  mount({
    name: "研究服务",
    context: { kind: "connector", connectorId: "c1" },
  });
  await selectPartner();
  expect(
    await screen.findByText("需要已安装并启用此连接器的伙伴。"),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
  expect(destination().state).toBeNull();
});

it("shares the composer policy normalization for an omitted default policy", async () => {
  state.policy = undefined;
  mount({
    name: "研究服务",
    context: { kind: "connector", connectorId: "c1" },
  });
  await selectPartner();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
});

it("keeps read errors local and retries the selected installation", async () => {
  state.error = true;
  mount({
    name: "研究服务",
    context: { kind: "connector", connectorId: "c1" },
  });
  await selectPartner();
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
  state.error = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
});

it("references only the owning plugin after checking the same enabled version", async () => {
  mount({
    name: "插件服务",
    context: { kind: "plugin", pluginId: "p1", versionId: "v1" },
  });
  await selectPartner();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(destination().state.chatDraftIntent.pluginId).toBe("p1");
  expect(destination().state.chatDraftIntent.connectorId).toBeUndefined();
});

it.each([
  "missing",
  "disabled",
  "old version",
  "unsupported",
  "unknown package",
])("does not reference an unavailable plugin snapshot: %s", async (reason) => {
  state.installed = reason !== "missing";
  state.enabled = reason !== "disabled";
  state.version = reason === "old version" ? "old" : "v1";
  state.packageStatus =
    reason === "unsupported"
      ? "unsupported"
      : reason === "unknown package"
        ? undefined
        : "available";
  mount({
    name: "插件服务",
    context: { kind: "plugin", pluginId: "p1", versionId: "v1" },
  });
  await selectPartner();
  expect(
    await screen.findByText("需要已安装并启用此连接器的伙伴。"),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
});

it.each(["preview", "system", "disabled current partner"])(
  "always keeps the TryCard with an honest disabled arrow for %s",
  (reason) => {
    mount({
      name: "目录服务",
      context:
        reason === "preview"
          ? { kind: "preview" }
          : { kind: "connector", connectorId: "c1" },
      systemManaged: reason === "system",
      wakerId: reason === "disabled current partner" ? "w1" : undefined,
    });
    const arrow = screen.getByRole("button", { name: "帮我使用这个连接器。" });
    expect(arrow).toBeDisabled();
    expect(arrow).toHaveAttribute(
      "title",
      reason === "system"
        ? "此连接器由系统按场景使用，不能在普通聊天中试用。"
        : "需要已安装并启用此连接器的伙伴。",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(connectorRead).not.toHaveBeenCalled();
    expect(pluginRead).not.toHaveBeenCalled();
  },
);

it("blocks an already open picker if the connector becomes system managed", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  function App({ blocked }: { blocked: boolean }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <RetainedScene>
            <ConnectorPresentation
              name="研究服务"
              context={{ kind: "connector", connectorId: "c1" }}
              systemManaged={blocked}
            />
          </RetainedScene>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  const { rerender } = render(<App blocked={false} />);
  await selectPartner();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  rerender(<App blocked />);
  const button = screen.getByRole("button", { name: "继续填写草稿" });
  expect(button).toBeDisabled();
  fireEvent.click(button);
  expect(destination().state).toBeNull();
});
