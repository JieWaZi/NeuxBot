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
import type { PluginDetail } from "../model/plugin-view";

const fixture = vi.hoisted(() => ({
  rename: vi.fn<(input: unknown) => Promise<unknown>>(),
  configure: vi.fn<(input: unknown) => Promise<unknown>>(),
}));
vi.mock("../api/plugin-queries", () => ({
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
  fixture.rename.mockReset().mockResolvedValue({ success: true });
  fixture.configure.mockReset().mockResolvedValue({ success: true });
});
const detail: PluginDetail = {
  plugin: { id: "p", name: "原名称", description: "简介" },
  configurationRevision: 7,
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
      switches: [],
    },
  });
});
it("keeps failed settings editable and retries without resubmitting an already saved name", async () => {
  fixture.configure.mockRejectedValueOnce(new Error("failed"));
  const close = show();
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
