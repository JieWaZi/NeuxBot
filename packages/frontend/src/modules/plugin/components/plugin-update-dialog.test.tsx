import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import type {
  GetPluginAuthorizationResponse,
  GetPluginConfigurationResponse,
} from "@/shared/api/generated/types.gen";
import type { PluginDetail } from "../model/plugin-view";
import { PluginUpdateDialog } from "./plugin-update-dialog";
const fixture = vi.hoisted(() => ({
  prepare: vi.fn(),
  read: vi.fn(),
  configRead: vi.fn(),
  save: vi.fn(),
  start: vi.fn(),
  cancel: vi.fn(),
  invalidate: vi.fn(),
  configuration: {} as GetPluginConfigurationResponse,
  account: {} as GetPluginAuthorizationResponse,
  target: {} as PluginDetail,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginOptions: (options: { query?: { versionId?: string } }) => ({
    queryKey: [{ _id: "getPlugin", versionId: options.query?.versionId }],
    queryFn: () => fixture.read(options.query?.versionId),
  }),
  getPluginConfigurationOptions: () => ({
    queryKey: [{ _id: "getPluginConfiguration" }],
    queryFn: fixture.configRead,
  }),
  getPluginAuthorizationOptions: () => ({
    queryKey: ["auth"],
    queryFn: async () => structuredClone(fixture.account),
  }),
  getPluginAuthorizationSessionOptions: (options: {
    path: { sessionId: string };
  }) => ({
    queryKey: ["auth-session", options.path.sessionId],
    queryFn: async () => structuredClone(fixture.account.session),
  }),
  checkPluginAccountMutation: () => ({ mutationFn: vi.fn() }),
  startPluginAuthorizationMutation: () => ({ mutationFn: fixture.start }),
  cancelPluginAuthorizationMutation: () => ({ mutationFn: fixture.cancel }),
  logoutPluginAuthorizationMutation: () => ({ mutationFn: vi.fn() }),
  prepareCatalogPluginMutation: () => ({ mutationFn: fixture.prepare }),
  updatePluginInstallationsMutation: () => ({ mutationFn: fixture.save }),
  invalidatePluginQueries: fixture.invalidate,
}));
const installed: PluginDetail = {
  plugin: { id: "p", name: "办公" },
  resolvedVersionId: "v1",
  versions: [{ id: "v1", package: { manifest: { description: "旧内容" } } }],
  installations: [
    { wakerId: "w1", versionId: "v1", enabled: false },
    { wakerId: "w2", versionId: "v2", enabled: true },
  ],
  partnerNames: { w1: "伙伴一", w2: "伙伴二" },
  configurationRevision: 3,
  accountGeneration: 4,
  update: {
    available: true,
    catalogKey: "office",
    archiveSHA256: "fixed-sha",
    version: "2.0.0",
  },
};
beforeEach(() => {
  vi.resetAllMocks();
  fixture.target = {
    ...installed,
    resolvedVersionId: "v2",
    versions: [
      {
        id: "v2",
        package: {
          manifest: { description: "新版说明" },
          upstream: { tag: "v2026" },
        },
      },
      ...(installed.versions ?? []),
    ],
  };
  fixture.configuration = {
    versionId: "v2",
    fields: [{ key: "existing", label: "现有设置" }],
    values: { existing: "keep" },
    configuredSecrets: [],
    configurationRevision: 3,
    accountGeneration: 4,
    ready: true,
    blockers: [],
  };
  fixture.account = {
    versionId: "v2",
    configurationRevision: 3,
    accountGeneration: 4,
    account: { available: false },
    session: { id: "", state: "" },
  };
  fixture.prepare.mockImplementation(async () =>
    structuredClone(fixture.target),
  );
  fixture.read.mockImplementation(async (version?: string) =>
    structuredClone(version ? fixture.target : installed),
  );
  fixture.configRead.mockImplementation(async () =>
    structuredClone(fixture.configuration),
  );
  fixture.save.mockResolvedValue({ success: true });
  fixture.cancel.mockResolvedValue({ success: true });
  fixture.start.mockResolvedValue({ id: "mine", state: "waiting" });
  fixture.invalidate.mockResolvedValue(undefined);
});
afterEach(cleanup);
function show() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const close = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PluginUpdateDialog detail={installed} onClose={close} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, close };
}
async function prepare() {
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText(/v2026/);
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "更新全部伙伴" }),
    ).not.toBeDisabled(),
  );
}
function withAuthentication() {
  const pkg = fixture.target.versions?.[0]?.package;
  if (pkg)
    pkg.integration = {
      authentication: {
        strategy: "plugin-command",
        protocol: "neuxbot-auth/1",
      },
    };
  fixture.configuration.ready = false;
}
it("reads configuration only after explicit preparation and commits every fixed installation with the account generation", async () => {
  const { close } = show();
  expect(fixture.configRead).not.toHaveBeenCalled();
  await prepare();
  expect(fixture.save).not.toHaveBeenCalled();
  expect(fixture.prepare.mock.calls[0]?.[0]).toEqual(
    expect.objectContaining({
      path: { key: "office" },
      body: expect.objectContaining({
        expectedArchiveSHA256: "fixed-sha",
        trustConfirmed: true,
      }),
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toEqual(
    expect.objectContaining({
      body: {
        expectedRevision: 3,
        expectedAccountGeneration: 4,
        values: { existing: "keep" },
        secrets: [],
        switches: [
          { wakerId: "w1", expectedVersionId: "v1", versionId: "v2" },
          { wakerId: "w2", expectedVersionId: "v2", versionId: "v2" },
        ],
      },
    }),
  );
});
it("retains required draft settings and original revision after an atomic update conflict", async () => {
  fixture.configuration.fields = [
    { key: "endpoint", label: "服务地址", required: true },
  ];
  fixture.configuration.ready = false;
  fixture.save.mockRejectedValueOnce(new ApiError("配置已变化"));
  const { close } = show();
  await prepare();
  fireEvent.change(screen.getByRole("textbox", { name: /服务地址/ }), {
    target: { value: "https://example.test" },
  });
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("配置已变化");
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("textbox", { name: /服务地址/ })).toHaveValue(
    "https://example.test",
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: {
      expectedRevision: 3,
      expectedAccountGeneration: 4,
      values: { existing: "keep", endpoint: "https://example.test" },
    },
  });
});
it("waits for cached account readiness and still requires the user's final update confirmation", async () => {
  withAuthentication();
  const { client, close } = show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText(/v2026/);
  expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeDisabled();
  fixture.account = {
    ...fixture.account,
    accountGeneration: 5,
    account: { available: true },
    session: { id: "done", state: "ready" },
  };
  fixture.configuration.accountGeneration = 5;
  await act(async () => {
    client.setQueryData(["auth"], structuredClone(fixture.account));
    await client.invalidateQueries({
      queryKey: [{ _id: "getPluginConfiguration" }],
    });
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  expect(fixture.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
    body: { expectedAccountGeneration: 5 },
  });
});
it("keeps preparation and configuration read failures local without switching any installation", async () => {
  fixture.configRead.mockRejectedValueOnce(new ApiError("配置读取失败"));
  show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("配置读取失败");
  expect(fixture.save).not.toHaveBeenCalled();
});
it("compensates a late authorization response using the original context after closing", async () => {
  withAuthentication();
  let resolve: ((value: { id: string; state: string }) => void) | undefined;
  fixture.start.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const { close } = show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText(/v2026/);
  fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
  await waitFor(() => expect(fixture.start).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(close).toHaveBeenCalledOnce();
  fixture.account.accountGeneration = 9;
  await act(async () => resolve?.({ id: "late", state: "waiting" }));
  await waitFor(() =>
    expect(fixture.cancel.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { pluginId: "p", sessionId: "late" },
        query: { wakerId: "", versionId: "v2" },
        body: {
          expectedConfigurationRevision: 3,
          expectedAccountGeneration: 4,
        },
      }),
    ),
  );
});
it("does not cancel another owner's existing authorization", async () => {
  withAuthentication();
  fixture.account.session = {
    id: "someone-else",
    state: "waiting",
    actions: [{ type: "open-url", url: "https://example.com/auth" }],
  };
  show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText(/v2026/);
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(fixture.cancel).not.toHaveBeenCalled();
});
it("loads fresh configuration when the same target is prepared again after a conflict", async () => {
  fixture.save.mockRejectedValueOnce(new ApiError("冲突"));
  show();
  await prepare();
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await screen.findByRole("alert");
  fixture.configuration = {
    ...fixture.configuration,
    configurationRevision: 4,
    values: { existing: "fresh" },
  };
  fireEvent.click(screen.getByRole("button", { name: "重新读取并确认" }));
  await screen.findByRole("button", { name: "确认并准备更新" });
  await prepare();
  expect(screen.getByRole("textbox", { name: "现有设置" })).toHaveValue(
    "fresh",
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(fixture.save).toHaveBeenCalledTimes(2));
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedRevision: 4, values: { existing: "fresh" } },
  });
});
