import { client } from "@/shared/api/generated/client.gen";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll } from "vitest";
const categoryServer = setupServer(
  http.get("http://127.0.0.1:8080/api/v1/resource-categories", () =>
    HttpResponse.json([{ id: "category", name: "开发" }]),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  categoryServer.listen({ onUnhandledRequest: "error" });
});
afterAll(() => categoryServer.close());
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginSettingsDialog } from "./plugin-settings-dialog";
import type { GetPluginConfigurationResponse } from "@/shared/api/generated/types.gen";
import type { PluginDetail } from "../model/plugin-view";

const fixture = vi.hoisted(() => ({
  rename: vi.fn<(input: unknown) => Promise<unknown>>(),
  configure: vi.fn<(input: unknown) => Promise<unknown>>(),
  read: vi.fn(),
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginConfigurationOptions: () => ({
    queryKey: [{ _id: "getPluginConfiguration" }],
    queryFn: fixture.read,
  }),
  updatePluginMutation: () => ({ mutationFn: fixture.rename }),
  updatePluginConfigurationMutation: () => ({ mutationFn: fixture.configure }),
  invalidatePluginQueries: async () => undefined,
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  fixture.read.mockResolvedValue(configuration);
  fixture.rename.mockReset().mockResolvedValue({ success: true });
  fixture.configure.mockReset().mockResolvedValue({ success: true });
});
const detail: PluginDetail = {
  plugin: { id: "p", name: "原名称", description: "简介" },
  resolvedVersionId: "v1",
  versions: [{ id: "v1" }],
};
const configuration: GetPluginConfigurationResponse = {
  configurationRevision: 7,
  accountGeneration: 4,
  fields: [
    { key: "endpoint", label: "服务地址" },
    { key: "token", label: "访问密钥", secret: true },
  ],
  values: { endpoint: "https://example.com" },
  configuredSecrets: ["token"],
};
function show() {
  const close = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PluginSettingsDialog detail={detail} onClose={close} />
    </QueryClientProvider>,
  );
  return close;
}
it("saves the name and settings together while retaining an untouched secret", async () => {
  const close = show();
  await screen.findByRole("textbox", { name: "服务地址" });
  expect(screen.getAllByRole("button", { name: "保存" })).toHaveLength(1);
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox", { name: "展示名称" }), {
    target: { value: "新名称" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "服务地址" }), {
    target: { value: "https://example.com/new" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(fixture.rename.mock.calls[0]?.[0]).toMatchObject({
    path: { pluginId: "p" },
    body: { name: "新名称", description: "简介" },
  });
  expect(fixture.configure.mock.calls[0]?.[0]).toMatchObject({
    path: { pluginId: "p" },
    body: {
      expectedRevision: 7,
      values: { endpoint: "https://example.com/new" },
      secrets: [],
    },
  });
});
it("keeps failed settings editable and retries without resubmitting an already saved name", async () => {
  fixture.configure.mockRejectedValueOnce(new Error("failed"));
  const close = show();
  await screen.findByRole("textbox", { name: "服务地址" });
  fireEvent.change(screen.getByRole("textbox", { name: "展示名称" }), {
    target: { value: "新名称" },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "清除此密钥" }));
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("textbox", { name: "展示名称" })).toHaveValue(
    "新名称",
  );
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(fixture.rename).toHaveBeenCalledOnce();
  expect(fixture.configure).toHaveBeenCalledTimes(2);
  expect(fixture.configure.mock.calls[1]?.[0]).toMatchObject({
    body: { expectedRevision: 7, secrets: [{ key: "token", clear: true }] },
  });
});
