import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginConfigurationEditor } from "./plugin-configuration-editor";

const fixture = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("../api/use-plugin-configuration", () => ({
  usePluginConfiguration: () => ({
    data: {
      fields: [{ key: "endpoint", label: "服务地址" }],
      values: { endpoint: "https://example.test" },
      configurationRevision: 1,
    },
    isFetching: false,
    error: null,
  }),
}));
vi.mock("../api/plugin-queries", () => ({
  updatePluginConfigurationMutation: () => ({ mutationFn: fixture.save }),
  invalidatePluginQueries: async () => undefined,
}));
afterEach(cleanup);

it("locks configuration fields until the submitted snapshot finishes saving", async () => {
  let finish: (() => void) | undefined;
  fixture.save.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const saved = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PluginConfigurationEditor
        detail={{ plugin: { id: "p1" }, resolvedVersionId: "v1" }}
        onSaved={saved}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(fixture.save).toHaveBeenCalledOnce());
  expect(screen.getByRole("textbox", { name: "服务地址" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  finish?.();
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(screen.getByRole("textbox", { name: "服务地址" })).toBeEnabled();
});
