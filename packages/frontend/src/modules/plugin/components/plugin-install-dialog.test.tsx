import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import "@/shared/i18n";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { PluginInstallDialog } from "./plugin-install-dialog";
const fixture = vi.hoisted(() => ({
  detail: {
    plugin: { id: "plugin-1", name: "办公" },
    versions: [
      {
        id: "version-1",
        version: "1.0.0",
        package: { manifest: { name: "office" }, skills: [] },
      },
    ],
    installations: new Array<{
      wakerId: string;
      versionId: string;
      enabled: boolean;
    }>(),
    account: { available: false },
    configurationRevision: 3,
    ready: true,
  },
  observers: new Set<GlobalEventObserver>(),
  readDelay: 0,
  authorization: vi
    .fn()
    .mockResolvedValue({ id: "attempt-1", state: "waiting" }),
  cancel: vi.fn().mockResolvedValue({}),
  prepare: vi.fn(),
  install: vi
    .fn<
      (
        input: unknown,
      ) => Promise<{ results: { wakerId: string; installed: boolean }[] }>
    >()
    .mockResolvedValue({ results: [{ wakerId: "p1", installed: true }] }),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.observers.add(observer);
    return () => {
      fixture.observers.delete(observer);
    };
  },
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginOptions: () => ({
    queryKey: [{ _id: "getPlugin", path: { pluginId: "plugin-1" } }],
    queryFn: async ({ signal }: { signal: AbortSignal }) => {
      const current = structuredClone(fixture.detail);
      if (fixture.readDelay)
        await new Promise<void>((resolve, reject) => {
          const timer = setTimeout(resolve, fixture.readDelay);
          signal.addEventListener(
            "abort",
            () => {
              clearTimeout(timer);
              reject(signal.reason);
            },
            { once: true },
          );
        });
      return current;
    },
  }),
  listWakersOptions: () => ({
    queryKey: [{ _id: "listWakers" }],
    queryFn: async () => ({
      resources: [
        { id: "p1", name: "伙伴一" },
        { id: "p2", name: "伙伴二" },
      ],
      totalSize: 2,
    }),
  }),
  getWakerOptions: () => ({
    queryKey: [{ _id: "getWaker" }],
    queryFn: async () => ({ id: "p1", name: "伙伴一" }),
  }),
  getPluginAuthorizationOptions: () => ({
    queryKey: [{ _id: "getPluginAuthorization" }],
  }),
  cancelPluginAuthorizationMutation: () => ({ mutationFn: fixture.cancel }),
  installPluginMutation: () => ({ mutationFn: fixture.install }),
  prepareCatalogPluginMutation: () => ({ mutationFn: fixture.prepare }),
  startPluginAuthorizationMutation: () => ({
    mutationFn: fixture.authorization,
  }),
  updatePluginConfigurationMutation: () => ({ mutationFn: vi.fn() }),
  invalidatePluginQueries: async () => undefined,
}));
vi.mock("./plugin-auth-panel", () => ({
  PluginAuthPanel: ({
    onReady,
    onRestart,
  }: {
    onReady?: () => void;
    onRestart?: (scopes: string[]) => Promise<void>;
  }) => (
    <div>
      <p>扫码授权区域</p>
      <button onClick={onReady}>完成设备授权</button>
      <button
        onClick={() => void onRestart?.(["extra:scope"]).catch(() => undefined)}
      >
        刷新设备授权
      </button>
    </div>
  ),
}));
afterEach(cleanup);
beforeEach(() => {
  fixture.install.mockReset();
  fixture.install.mockResolvedValue({
    results: [{ wakerId: "p1", installed: true }],
  });
  fixture.detail.ready = true;
  fixture.readDelay = 0;
  fixture.detail.account.available = false;
  fixture.detail.installations = [];
  fixture.authorization.mockReset();
  fixture.authorization.mockResolvedValue({
    id: "attempt-1",
    state: "waiting",
  });
  fixture.cancel.mockClear();
  for (const version of fixture.detail.versions)
    version.package = {
      manifest: { name: "office" },
      skills: [],
    };
});
function show(
  onClose = vi.fn(),
  authorizeOnly = false,
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  }),
) {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PluginInstallDialog
          detail={fixture.detail}
          onClose={onClose}
          authorizeOnly={authorizeOnly}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return onClose;
}
describe("plugin installation", () => {
  it("uses the freshly downloaded authorization declaration when a catalog item only contains metadata", async () => {
    fixture.detail.ready = false;
    Object.assign(fixture.detail.versions[0]?.package ?? {}, {
      integration: {
        authentication: {
          strategy: "plugin-command",
          protocol: "neuxally-auth/1",
          command: "node",
          args: ["./.neuxally/auth/authorize.mjs"],
        },
      },
    });
    fixture.prepare.mockResolvedValue(structuredClone(fixture.detail));
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <PluginInstallDialog
            catalog={{
              key: "office",
              package: { manifest: { name: "office" } },
            }}
            wakerId="p1"
            onClose={vi.fn()}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "继续" }));
    await waitFor(() => expect(fixture.authorization).toHaveBeenCalledOnce());
    expect(await screen.findByText("扫码授权区域")).toBeVisible();
    expect(fixture.install).not.toHaveBeenCalled();
  });
  it("marks real existing installations as checked and disabled", async () => {
    fixture.detail.installations = [
      { wakerId: "p1", versionId: "version-1", enabled: true },
    ];
    show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    expect(
      await screen.findByRole("checkbox", { name: "伙伴一" }),
    ).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "伙伴一" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "伙伴二" })).not.toBeDisabled();
    expect(screen.getByText("已安装")).toBeVisible();
  });
  it("reauthorizes the shared account without reinstalling existing partners", async () => {
    fixture.detail.ready = false;
    fixture.detail.installations = [
      { wakerId: "p1", versionId: "version-1", enabled: true },
    ];
    Object.assign(fixture.detail.versions[0]?.package.manifest ?? {}, {
      extensions: { "com.ally": { provider: "feishu" } },
    });
    const close = show(vi.fn(), true);
    const authorize = await screen.findByRole("button", { name: "授权" });
    await waitFor(() => expect(authorize).toBeEnabled());
    fireEvent.click(authorize);
    await waitFor(() => expect(fixture.authorization).toHaveBeenCalledOnce());
    await waitFor(() => expect(fixture.observers.size).toBeGreaterThan(0));
    fixture.detail.account.available = true;
    fixture.detail.ready = true;
    for (const observer of fixture.observers) observer.onResync?.("test");
    expect(await screen.findByRole("status")).toHaveTextContent("授权完成");
    expect(fixture.install).not.toHaveBeenCalled();
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
  });
  it("retries only failed partners after a partial installation", async () => {
    fixture.install
      .mockResolvedValueOnce({
        results: [
          { wakerId: "p1", installed: true },
          { wakerId: "p2", installed: false },
        ],
      })
      .mockResolvedValueOnce({ results: [{ wakerId: "p2", installed: true }] });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "伙伴一" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "伙伴二" }));
    fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    fireEvent.click(await screen.findByRole("button", { name: "重试" }));
    await waitFor(() => expect(fixture.install).toHaveBeenCalledTimes(2));
    expect(fixture.install.mock.calls[1]?.[0]).toMatchObject({
      body: { wakerIds: ["p2"] },
    });
  });
  it("selects partners only after readiness and submits real version/revision", async () => {
    const close = show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    const checkbox = await screen.findByRole("checkbox", { name: "伙伴一" });
    fireEvent.click(checkbox);
    fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(fixture.install).toHaveBeenCalledTimes(1));
    expect(fixture.install.mock.calls[0]?.[0]).toMatchObject({
      path: { pluginId: "plugin-1" },
      body: {
        versionId: "version-1",
        expectedConfigurationRevision: 3,
        wakerIds: ["p1"],
      },
    });
    expect(await screen.findByRole("status")).toHaveTextContent("安装成功");
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
  });
  it("does not create an installation when the user closes the prepared resource", async () => {
    const close = show();
    fireEvent.click(await screen.findByRole("button", { name: "关闭" }));
    expect(close).toHaveBeenCalledTimes(1);
    expect(fixture.install).not.toHaveBeenCalled();
    expect(fixture.cancel).not.toHaveBeenCalled();
  });
  it("selects partners before authorization and installs them after the completion event", async () => {
    fixture.detail.ready = false;
    for (const version of fixture.detail.versions)
      Object.assign(version.package.manifest, {
        extensions: { "com.ally": { provider: "feishu" } },
      });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "伙伴一" }));
    fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "继续" }));
    await screen.findByText("扫码授权区域");
    expect(
      screen.queryByText("伙伴一", { exact: true }),
    ).not.toBeInTheDocument();
    await waitFor(() => expect(fixture.authorization).toHaveBeenCalledTimes(1));
    expect(fixture.install).not.toHaveBeenCalled();
    await waitFor(() => expect(fixture.observers.size).toBeGreaterThan(0));
    fixture.detail.ready = true;
    fixture.readDelay = 400;
    for (const observer of fixture.observers) observer.onResync?.("test");
    await waitFor(() => expect(fixture.install).toHaveBeenCalledTimes(1), {
      timeout: 3000,
    });
    expect(fixture.install.mock.calls[0]?.[0]).toMatchObject({
      body: { wakerIds: ["p1"] },
    });
  });
  it("never installs after the authorization dialog is closed", async () => {
    fixture.detail.ready = false;
    for (const version of fixture.detail.versions)
      Object.assign(version.package.manifest, {
        extensions: { "com.ally": { provider: "feishu" } },
      });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "伙伴一" }));
    fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "继续" }));
    await screen.findByText("扫码授权区域");
    await waitFor(() => expect(fixture.observers.size).toBeGreaterThan(0));
    const observers = [...fixture.observers];
    cleanup();
    fixture.detail.ready = true;
    for (const observer of observers) observer.onResync?.("test");
    expect(fixture.install).not.toHaveBeenCalled();
  });
  it("continues from the verified completion callback without waiting for another push", async () => {
    fixture.detail.ready = false;
    Object.assign(fixture.detail.versions[0]?.package.manifest ?? {}, {
      extensions: { "com.ally": { provider: "feishu" } },
    });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "伙伴一" }));
    fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "继续" }));
    await screen.findByText("扫码授权区域");
    await waitFor(() => expect(fixture.authorization).toHaveBeenCalledOnce());
    fixture.detail.ready = true;
    fireEvent.click(screen.getByRole("button", { name: "完成设备授权" }));
    await waitFor(() => expect(fixture.install).toHaveBeenCalledOnce());
    expect(await screen.findByRole("status")).toHaveTextContent("安装成功");
  });
});

it("uses the shared localized summary in installation and authorization panels", () => {
  Object.assign(fixture.detail.versions[0]?.package ?? {}, {
    presentation: {
      shortDescription: "English summary",
      localizedDescriptions: { en: "English summary", "zh-CN": "中文业务简介" },
    },
  });
  show(vi.fn(), true);
  expect(screen.getByText("中文业务简介")).toBeVisible();
  expect(screen.queryByText("English summary")).not.toBeInTheDocument();
});

async function beginWaitingInstallation(client?: QueryClient) {
  fixture.detail.ready = false;
  Object.assign(fixture.detail.versions[0]?.package.manifest ?? {}, {
    extensions: { "com.ally": { provider: "feishu" } },
  });
  const close = show(vi.fn(), false, client);
  fireEvent.click(await screen.findByRole("button", { name: "选择伙伴" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "伙伴一" }));
  fireEvent.keyDown(screen.getByRole("checkbox", { name: "伙伴一" }), {
    key: "Escape",
  });
  fireEvent.click(screen.getByRole("button", { name: "继续" }));
  await waitFor(() => expect(fixture.authorization).toHaveBeenCalledOnce());
  return close;
}

it.each(["close", "escape", "overlay", "unmount"])(
  "cancels only the owned pending attempt once on %s",
  async (mode) => {
    const close = await beginWaitingInstallation();
    await screen.findByText("扫码授权区域");
    if (mode === "close")
      fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    if (mode === "escape")
      fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    if (mode === "overlay") {
      const overlay = document.querySelector('[data-motion="overlay"]');
      if (!overlay) throw new Error("Missing dialog overlay");
      fireEvent.pointerDown(overlay, {
        button: 0,
        ctrlKey: false,
        pointerType: "mouse",
      });
      fireEvent.click(overlay);
    }
    cleanup();
    await waitFor(() => expect(fixture.cancel).toHaveBeenCalledOnce());
    expect(fixture.cancel.mock.calls[0]?.[0]).toMatchObject({
      path: { pluginId: "plugin-1", sessionId: "attempt-1" },
      query: { wakerId: "", versionId: "version-1" },
    });
    if (mode !== "unmount") expect(close).toHaveBeenCalledOnce();
    expect(fixture.install).not.toHaveBeenCalled();
  },
);

it("cancels a pending start response after closing before its session ID exists", async () => {
  let finish: ((value: { id: string; state: string }) => void) | undefined;
  fixture.authorization.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await beginWaitingInstallation();
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  cleanup();
  expect(fixture.cancel).not.toHaveBeenCalled();
  finish?.({ id: "late-attempt", state: "waiting" });
  await waitFor(() => expect(fixture.cancel).toHaveBeenCalledOnce());
  expect(fixture.cancel.mock.calls[0]?.[0]).toMatchObject({
    path: { sessionId: "late-attempt" },
  });
  expect(fixture.install).not.toHaveBeenCalled();
});

it("does not cancel or disconnect an authorization after successful installation closes", async () => {
  await beginWaitingInstallation();
  fixture.detail.ready = true;
  fireEvent.click(await screen.findByRole("button", { name: "完成设备授权" }));
  await waitFor(() => expect(fixture.install).toHaveBeenCalledOnce());
  expect(await screen.findByRole("status")).toHaveTextContent("安装成功");
  cleanup();
  expect(fixture.cancel).not.toHaveBeenCalled();
});

it("waits for the previous owned challenge to stop before replacing it and cancels the replacement on close", async () => {
  let finishCancel: (() => void) | undefined;
  fixture.cancel.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finishCancel = resolve;
      }),
  );
  await beginWaitingInstallation();
  fixture.authorization.mockResolvedValueOnce({
    id: "replacement-attempt",
    state: "waiting",
  });
  fireEvent.click(await screen.findByRole("button", { name: "刷新设备授权" }));
  await waitFor(() => expect(fixture.cancel).toHaveBeenCalledOnce());
  expect(fixture.authorization).toHaveBeenCalledOnce();
  finishCancel?.();
  await waitFor(() => expect(fixture.authorization).toHaveBeenCalledTimes(2));
  expect(fixture.authorization.mock.calls[1]?.[0]).toMatchObject({
    body: { additionalScopes: ["extra:scope"] },
  });
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  cleanup();
  await waitFor(() => expect(fixture.cancel).toHaveBeenCalledTimes(2));
  expect(fixture.cancel.mock.calls[1]?.[0]).toMatchObject({
    path: { sessionId: "replacement-attempt" },
  });
});

it("honors a completed authoritative snapshot when closing before the installation snapshot refreshes", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await beginWaitingInstallation(client);
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    session: { id: "attempt-1", state: "ready" },
    account: { available: true },
  });
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  cleanup();
  expect(fixture.cancel).not.toHaveBeenCalled();
  expect(fixture.install).not.toHaveBeenCalled();
});

it("surfaces a failed QR cancellation and allows the refresh to retry before starting another session", async () => {
  await beginWaitingInstallation();
  fixture.cancel.mockRejectedValueOnce(
    new Error("temporary cancellation failure"),
  );
  fireEvent.click(await screen.findByRole("button", { name: "刷新设备授权" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "操作未完成，请重试。",
  );
  expect(fixture.authorization).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "刷新设备授权" }));
  await waitFor(() => expect(fixture.cancel).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(fixture.authorization).toHaveBeenCalledTimes(2));
});
