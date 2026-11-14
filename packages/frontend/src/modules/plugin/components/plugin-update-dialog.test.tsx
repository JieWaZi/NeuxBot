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
    wakerIds: ["w1"],
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
  fixture.start.mockResolvedValue({
    id: "mine",
    state: "waiting",
    created: true,
  });
  fixture.invalidate.mockResolvedValue(undefined);
});
afterEach(cleanup);
function show(detail = installed, wakerId = "", rollback = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const close = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PluginUpdateDialog
          detail={detail}
          wakerId={wakerId}
          rollback={rollback}
          onClose={close}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, close };
}
it("reads the stored rollback target and confirms it for every installation", async () => {
  fixture.target.rollback = {
    available: true,
    versionId: "v2",
    version: "0.9.0",
  };
  show(
    {
      ...installed,
      installations: installed.installations?.map((relation) => ({
        ...relation,
        versionId: "v1",
      })),
      rollback: { available: true, versionId: "v2", version: "0.9.0" },
      update: { available: false },
    },
    "w1",
    true,
  );
  expect(fixture.read).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "准备回滚" }));
  const submit = await screen.findByRole("button", { name: "回滚全部伙伴" });
  await waitFor(() => expect(submit).toBeEnabled());
  expect(fixture.read).toHaveBeenCalledWith("v2");
  expect(fixture.prepare).not.toHaveBeenCalled();
  fireEvent.click(submit);
  await waitFor(() =>
    expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
      query: { wakerId: "w1" },
      body: expect.objectContaining({
        rollback: true,
        switches: [
          { wakerId: "w1", expectedVersionId: "v1", versionId: "v2" },
          { wakerId: "w2", expectedVersionId: "v1", versionId: "v2" },
        ],
      }),
    }),
  );
});
it("reconfirms the retained version before reading rollback configuration", async () => {
  fixture.target.rollback = { available: false };
  show(
    {
      ...installed,
      rollback: { available: true, versionId: "v2", version: "0.9.0" },
    },
    "w1",
    true,
  );
  fireEvent.click(screen.getByRole("button", { name: "准备回滚" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "当前没有可回滚的版本。",
  );
  expect(fixture.configRead).not.toHaveBeenCalled();
  expect(fixture.start).not.toHaveBeenCalled();
});
it("explains a rollback that was withdrawn before the confirmation opened", () => {
  show({ ...installed, rollback: { available: false } }, "w1", true);
  expect(screen.getByText("当前没有可回滚的版本。")).toBeVisible();
  expect(screen.queryByRole("button", { name: "准备回滚" })).toBeNull();
  expect(fixture.read).not.toHaveBeenCalled();
});
async function prepare() {
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText("新版说明");
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "更新全部伙伴" }),
    ).not.toBeDisabled(),
  );
}
it("keeps the dialog open until a submitted version change finishes", async () => {
  let finish!: (value: { success: boolean }) => void;
  fixture.save.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { close } = show();
  await prepare();
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(fixture.save).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  fireEvent.keyDown(document, { key: "Escape" });
  const overlay = document.querySelector('[data-motion="overlay"]');
  if (!overlay) throw new Error("Missing dialog overlay");
  fireEvent.pointerDown(overlay, { button: 0, pointerType: "mouse" });
  expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
  expect(close).not.toHaveBeenCalled();
  await act(async () => finish({ success: true }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
});
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
it("shows the actual installed package version for each partner and the catalog target", () => {
  show({
    ...installed,
    versions: [
      { id: "v1", version: "1.0.0" },
      { id: "v2", version: "1.5.0" },
    ],
    update: { ...installed.update, wakerIds: ["w1", "w2"] },
  });
  expect(screen.getByText("1.0.0 → 2.0.0")).toBeVisible();
  expect(screen.getByText("1.5.0 → 2.0.0")).toBeVisible();
});
it("explains when the advertised target is already installed", () => {
  show({ ...installed, update: { available: false } });
  expect(screen.getByText("当前没有可更新的版本。")).toBeVisible();
  expect(screen.queryByRole("button", { name: "确认并准备更新" })).toBeNull();
});
it("reads configuration only after explicit preparation and commits every fixed installation with the account generation", async () => {
  const { close } = show(installed, "w1");
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
      query: { wakerId: "w1" },
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
it("retains the draft and allows the same request to retry after PluginBusy", async () => {
  fixture.configuration.fields = [
    { key: "endpoint", label: "服务地址", required: true },
  ];
  fixture.configuration.ready = false;
  fixture.save.mockRejectedValueOnce(
    new ApiError("插件正在使用", "PluginBusy"),
  );
  const { close } = show();
  await prepare();
  fireEvent.change(screen.getByRole("textbox", { name: /服务地址/ }), {
    target: { value: "https://example.test" },
  });
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("插件正在使用");
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
  await screen.findByText("新版说明");
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
  let resolve:
    | ((value: { id: string; state: string; created: boolean }) => void)
    | undefined;
  fixture.start.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const { close } = show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText("新版说明");
  fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
  await waitFor(() => expect(fixture.start).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(close).toHaveBeenCalledOnce();
  fixture.account.accountGeneration = 9;
  await act(async () =>
    resolve?.({ id: "late", state: "waiting", created: true }),
  );
  await waitFor(() =>
    expect(fixture.cancel.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { pluginId: "p", sessionId: "late" },
        query: { wakerId: "", versionId: "v2" },
        body: {
          expectedConfigurationRevision: 3,
          expectedAccountGeneration: 4,
          ownershipNonce: fixture.start.mock.calls[0]?.[0].body.ownershipNonce,
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
  await screen.findByText("新版说明");
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(fixture.cancel).not.toHaveBeenCalled();
});
it("does not claim a concurrently created authorization returned by Core", async () => {
  withAuthentication();
  fixture.start.mockResolvedValueOnce({
    id: "other-dialog",
    state: "waiting",
    created: false,
  });
  show();
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await screen.findByText("新版说明");
  fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
  await waitFor(() => expect(fixture.invalidate).toHaveBeenCalled());
  await act(async () =>
    fireEvent.click(screen.getByRole("button", { name: "取消" })),
  );
  expect(fixture.cancel).not.toHaveBeenCalled();
});
it("requires re-confirmation after another request changes the shared configuration revision", async () => {
  fixture.save.mockImplementationOnce(async () => {
    fixture.configuration = {
      ...fixture.configuration,
      configurationRevision: 4,
      values: { existing: "fresh" },
    };
    throw new ApiError("冲突", "PluginConfigurationConflict");
  });
  show();
  await prepare();
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  expect(fixture.save).toHaveBeenCalledOnce();
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
it("allows a corrected draft to retry a configuration conflict at the same revision", async () => {
  fixture.save.mockRejectedValueOnce(
    new ApiError("设置不兼容", "PluginConfigurationConflict"),
  );
  show();
  await prepare();
  const submit = screen.getByRole("button", { name: "更新全部伙伴" });
  fireEvent.click(submit);
  await screen.findByRole("alert");
  expect(submit).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox", { name: "现有设置" }), {
    target: { value: "corrected" },
  });
  await waitFor(() => expect(submit).toBeEnabled());
  fireEvent.click(submit);
  await waitFor(() => expect(fixture.save).toHaveBeenCalledTimes(2));
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedRevision: 3, values: { existing: "corrected" } },
  });
});
it("prevents repeating a rejected rollback until the installations are reread", async () => {
  fixture.save.mockRejectedValueOnce(new ApiError("安装已变化", "40900006501"));
  const rollback = { available: true, versionId: "v2", version: "0.9.0" };
  fixture.target.rollback = rollback;
  show({ ...installed, rollback }, "w1", true);
  fireEvent.click(screen.getByRole("button", { name: "准备回滚" }));
  const submit = await screen.findByRole("button", { name: "回滚全部伙伴" });
  await waitFor(() => expect(submit).toBeEnabled());
  fireEvent.click(submit);
  await screen.findByRole("alert");
  expect(submit).toBeDisabled();
  fireEvent.click(submit);
  expect(fixture.save).toHaveBeenCalledOnce();
});
it("refreshes the exact target account generation before retrying an account conflict", async () => {
  fixture.save.mockImplementationOnce(async () => {
    fixture.configuration.accountGeneration = 5;
    throw new ApiError("账号已变化", "PluginAccountConflict");
  });
  show();
  await prepare();
  const submit = screen.getByRole("button", { name: "更新全部伙伴" });
  fireEvent.click(submit);
  await screen.findByRole("alert");
  await waitFor(() => expect(submit).toBeEnabled());
  fireEvent.click(submit);
  await waitFor(() => expect(fixture.save).toHaveBeenCalledTimes(2));
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedRevision: 3, expectedAccountGeneration: 5 },
  });
});
