import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  act,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import type { PluginDetail } from "../model/plugin-view";
import { PluginUpdateDialog } from "./plugin-update-dialog";

const fixture = vi.hoisted(() => ({
  auth: vi.fn(),
  startAuth: vi.fn(),
  cancel: vi.fn(),
  read: vi.fn(),
  prepare: vi.fn(),
  save: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", () => ({
  cancelPluginAuthorizationMutation: () => ({ mutationFn: fixture.cancel }),
  getPluginAuthorizationOptions: () => ({
    queryKey: ["auth"],
    queryFn: fixture.auth,
  }),
  startPluginAuthorizationMutation: () => ({ mutationFn: fixture.startAuth }),
  logoutPluginAuthorizationMutation: () => ({ mutationFn: vi.fn() }),
  getPluginOptions: (options: { query?: { versionId?: string } }) => ({
    queryKey: [{ _id: "getPlugin", versionId: options.query?.versionId }],
    queryFn: () => fixture.read(options.query?.versionId),
  }),
  prepareCatalogPluginMutation: () => ({ mutationFn: fixture.prepare }),
  updatePluginConfigurationMutation: () => ({ mutationFn: fixture.save }),
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
  update: {
    available: true,
    catalogKey: "office",
    archiveSHA256: "fixed-sha",
    version: "2.0.0",
    wakerIds: ["w1"],
  },
  fields: [{ key: "existing", label: "现有设置" }],
  values: { existing: "keep" },
  configurationRevision: 3,
};
function target(): PluginDetail {
  return {
    ...installed,
    resolvedVersionId: "v2",
    ready: true,
    versions: [
      {
        id: "v2",
        package: {
          manifest: { version: "0.1.0", description: "新版说明" },
          upstream: { tag: "v2026" },
        },
      },
      ...(installed.versions ?? []),
    ],
  };
}
function show(wakerId = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PluginUpdateDialog
          detail={installed}
          wakerId={wakerId}
          onClose={onClose}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { onClose, client };
}
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  fixture.auth.mockResolvedValue({ account: { available: false } });
  fixture.startAuth.mockResolvedValue({
    id: "owned-session",
    state: "waiting",
  });
  fixture.read.mockImplementation(async (versionId?: string) =>
    structuredClone(versionId ? target() : installed),
  );
  fixture.prepare.mockResolvedValue(target());
  fixture.save.mockResolvedValue({ success: true });
  fixture.invalidate.mockResolvedValue(undefined);
});
async function prepare() {
  fireEvent.click(
    await screen.findByRole("button", { name: "确认并准备更新" }),
  );
  await screen.findByText("新版说明");
  await waitFor(() =>
    expect(screen.queryByText("正在处理…")).not.toBeInTheDocument(),
  );
  await act(async () => {
    await Promise.resolve();
  });
}
it("prepares a fixed target and explicitly updates every installed partner including disabled and already-current installs", async () => {
  const { onClose } = show("w1");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  await prepare();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getByText("将更新全部 2 个伙伴")).toBeVisible();
  expect(screen.getByText("伙伴二")).toBeVisible();
  expect(fixture.prepare.mock.calls[0]?.[0]).toMatchObject({
    path: { key: "office" },
    body: { expectedArchiveSHA256: "fixed-sha", trustConfirmed: true },
  });
  expect(fixture.save).not.toHaveBeenCalled();
  expect(screen.getByText(/插件版本 · v2026/)).toBeVisible();
  expect(screen.queryByText("0.1.0")).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
    path: { pluginId: "p" },
    body: {
      expectedRevision: 3,
      values: { existing: "keep" },
      switches: [
        { wakerId: "w1", expectedVersionId: "v1", versionId: "v2" },
        { wakerId: "w2", expectedVersionId: "v2", versionId: "v2" },
      ],
    },
  });
  expect(fixture.invalidate).toHaveBeenCalledOnce();
});
it("retains preparation errors for retry without changing any installation", async () => {
  fixture.prepare.mockRejectedValueOnce(new ApiError("归档已变化"));
  const { onClose } = show();
  fireEvent.click(
    await screen.findByRole("button", { name: "确认并准备更新" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("归档已变化");
  expect(fixture.save).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
  await prepare();
});
it("keeps draft settings and the original optimistic revision after a failed atomic switch", async () => {
  const detail = {
    ...target(),
    ready: false,
    fields: [{ key: "endpoint", label: "服务地址", required: true }],
  };
  fixture.prepare.mockResolvedValue(detail);
  fixture.read.mockImplementation(async (id?: string) =>
    structuredClone(id ? detail : installed),
  );
  fixture.save.mockRejectedValueOnce(new ApiError("配置已变化"));
  const { onClose } = show();
  await prepare();
  fireEvent.change(screen.getByRole("textbox", { name: /服务地址/ }), {
    target: { value: "https://example.test" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("配置已变化");
  expect(screen.getByRole("textbox", { name: /服务地址/ })).toHaveValue(
    "https://example.test",
  );
  expect(onClose).not.toHaveBeenCalled();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: {
      expectedRevision: 3,
      values: { endpoint: "https://example.test", existing: "keep" },
    },
  });
});
it("waits for account readiness but never auto-submits after authorization", async () => {
  let current = { ...target(), ready: false, account: { available: false } };
  const pkg = current.versions?.[0]?.package;
  if (pkg) pkg.integration = { authentication: { strategy: "plugin-command" } };
  fixture.prepare.mockResolvedValue(current);
  fixture.read.mockImplementation(async (id?: string) =>
    structuredClone(id ? current : installed),
  );
  const { onClose, client } = show();
  await prepare();
  expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeDisabled();
  current = { ...current, ready: true, account: { available: true } };
  await act(async () => {
    client.setQueryData(["auth"], {
      account: { available: true },
      session: { id: "completed", state: "ready" },
    });
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  expect(fixture.save).not.toHaveBeenCalled();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
});
it("does not close until affected queries have refreshed", async () => {
  let refreshed: (() => void) | undefined;
  fixture.invalidate.mockReturnValue(
    new Promise<void>((resolve) => {
      refreshed = resolve;
    }),
  );
  const { onClose } = show();
  await prepare();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(fixture.invalidate).toHaveBeenCalled());
  expect(onClose).not.toHaveBeenCalled();
  await act(async () => refreshed?.());
  expect(onClose).toHaveBeenCalledOnce();
});

it("reloads changed catalog metadata after preparation fails instead of repeatedly preparing an obsolete digest", async () => {
  fixture.prepare.mockRejectedValueOnce(new ApiError("归档已变化"));
  fixture.read.mockResolvedValue({
    ...installed,
    update: { ...installed.update, archiveSHA256: "new-fixed-sha" },
  });
  show();
  fireEvent.click(
    await screen.findByRole("button", { name: "确认并准备更新" }),
  );
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "重新读取并确认" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认并准备更新" }));
  await waitFor(() => expect(fixture.prepare).toHaveBeenCalledTimes(2));
  expect(fixture.prepare.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedArchiveSHA256: "new-fixed-sha" },
  });
});
it.each([false, true])(
  "only cancels its own waiting authorization when closing (completed=%s)",
  async (completed) => {
    const detail = target();
    if (detail.versions?.[0]?.package)
      detail.versions[0].package.integration = {
        authentication: { strategy: "plugin-command" },
      };
    fixture.prepare.mockResolvedValue(detail);
    fixture.read.mockResolvedValue(detail);
    const { client, onClose } = show();
    await prepare();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "连接账号" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "连接账号" }));
    await waitFor(() => expect(fixture.startAuth).toHaveBeenCalledOnce());
    if (completed)
      client.setQueryData(["auth"], {
        session: { id: "owned-session", state: "ready" },
      });
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(onClose).toHaveBeenCalledOnce();
    if (completed) expect(fixture.cancel).not.toHaveBeenCalled();
    else
      await waitFor(() =>
        expect(fixture.cancel.mock.calls[0]?.[0]).toMatchObject({
          path: { pluginId: "p", sessionId: "owned-session" },
          query: { versionId: "v2" },
        }),
      );
    expect(fixture.save).not.toHaveBeenCalled();
  },
);

it("updates with new required draft settings and an already connected account even when the saved configuration summary is not ready", async () => {
  const detail = {
    ...target(),
    ready: false,
    account: {},
    fields: [{ key: "endpoint", label: "服务地址", required: true }],
  };
  if (detail.versions?.[0]?.package)
    detail.versions[0].package.integration = {
      authentication: { strategy: "plugin-command" },
    };
  fixture.prepare.mockResolvedValue(detail);
  fixture.read.mockResolvedValue(detail);
  fixture.auth.mockResolvedValue({
    account: { available: true, name: "已连接账号" },
  });
  const { onClose } = show();
  await prepare();
  expect(await screen.findByText("已连接：已连接账号")).toBeVisible();
  fireEvent.change(screen.getByRole("textbox", { name: /服务地址/ }), {
    target: { value: "https://new.test" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
    body: {
      values: { endpoint: "https://new.test", existing: "keep" },
      switches: [
        { wakerId: "w1", expectedVersionId: "v1", versionId: "v2" },
        { wakerId: "w2", expectedVersionId: "v2", versionId: "v2" },
      ],
    },
  });
});

it("cancels a newly started authorization whose response arrives after the update dialog has closed", async () => {
  const detail = target();
  if (detail.versions?.[0]?.package)
    detail.versions[0].package.integration = {
      authentication: { strategy: "plugin-command" },
    };
  fixture.prepare.mockResolvedValue(detail);
  fixture.read.mockResolvedValue(detail);
  let finish: ((session: { id: string; state: string }) => void) | undefined;
  fixture.startAuth.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { onClose } = show();
  await prepare();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "连接账号" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "连接账号" }));
  await waitFor(() => expect(fixture.startAuth).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  await act(async () => finish?.({ id: "late-session", state: "waiting" }));
  await waitFor(() =>
    expect(fixture.cancel.mock.calls[0]?.[0]).toMatchObject({
      path: { pluginId: "p", sessionId: "late-session" },
    }),
  );
  expect(onClose).toHaveBeenCalledOnce();
  expect(fixture.save).not.toHaveBeenCalled();
});
it("does not cancel another owner's existing waiting authorization when the dialog closes", async () => {
  const detail = target();
  if (detail.versions?.[0]?.package)
    detail.versions[0].package.integration = {
      authentication: { strategy: "plugin-command" },
    };
  fixture.prepare.mockResolvedValue(detail);
  fixture.read.mockResolvedValue(detail);
  fixture.auth.mockResolvedValue({
    account: { available: false },
    session: { id: "other-owner", state: "waiting" },
  });
  show();
  await prepare();
  expect(
    await screen.findByText("请完成当前授权步骤，完成后会自动更新状态。"),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(fixture.cancel).not.toHaveBeenCalled();
});

it("uses the fresh configuration revision when re-preparing the same target after a conflict", async () => {
  fixture.save.mockRejectedValueOnce(new ApiError("配置已变化"));
  const { onClose } = show();
  await prepare();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("配置已变化");
  const refreshedTarget = {
    ...target(),
    configurationRevision: 4,
    values: { existing: "fresh" },
  };
  fixture.read.mockImplementation(async (versionId?: string) =>
    structuredClone(
      versionId
        ? refreshedTarget
        : {
            ...installed,
            configurationRevision: 4,
            values: { existing: "fresh" },
          },
    ),
  );
  fixture.prepare.mockResolvedValue(refreshedTarget);
  fireEvent.click(screen.getByRole("button", { name: "重新读取并确认" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "确认并准备更新" }),
    ).toBeEnabled(),
  );
  await prepare();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "更新全部伙伴" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "更新全部伙伴" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedRevision: 4, values: { existing: "fresh" } },
  });
});
