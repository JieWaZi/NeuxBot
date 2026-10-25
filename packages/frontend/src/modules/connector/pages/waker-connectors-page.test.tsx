import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";

import { WakerConnectorsPage } from "./waker-connectors-page";
const { remove } = vi.hoisted(() => ({
  remove: vi.fn<
    (options: { path: Record<string, string> }) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("../components/connector-form-dialog", () => ({
  ConnectorFormDialog: () => null,
}));
vi.mock("../api/connector-queries", () => ({
  listWakerConnectorsOptions: () => ({
    queryKey: ["connectors"],
    queryFn: async () =>
      [
        { id: "builtin", name: "系统记忆", scope: "built_in" },
        { id: "a", name: "公开 A", scope: "public" },
        { id: "b", name: "公开 B", scope: "public" },
      ].map((connector) => ({
        connector: { ...connector, transport: "stdio", testStatus: "untested" },
        enabled: true,
        affectedWakerCount: 2,
        compatibility: [],
      })),
  }),
  deleteWakerConnectorMutation: () => ({ mutationFn: remove }),
  setWakerConnectorEnabledMutation: () => ({ mutationFn: vi.fn() }),
  invalidateVisibleConnectorQueries: async () => undefined,
}));
beforeEach(() => remove.mockClear());
it("omits the market, protects built-ins and deletes selected public connectors", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <MemoryRouter initialEntries={["/wakers/owner/connectors"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/connectors"
            element={<WakerConnectorsPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await screen.findByRole("checkbox", { name: "选择连接器 公开 A" });
  expect(screen.queryByText("连接器市场")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("checkbox", { name: "选择连接器 系统记忆" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("checkbox", { name: "选择" }));
  expect(screen.getByText("已选择 2 项")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "删除所选" }));
  const dialog = screen.getByRole("dialog", { name: "删除所选连接器" });
  expect(within(dialog).getByText("公开 A（影响 2 个伙伴）")).toBeVisible();
  expect(remove).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
  await waitFor(() => expect(remove).toHaveBeenCalledTimes(2));
  expect(remove.mock.calls.map((call) => call[0])).toEqual([
    expect.objectContaining({ path: { connectorId: "a", wakerId: "owner" } }),
    expect.objectContaining({ path: { connectorId: "b", wakerId: "owner" } }),
  ]);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("switch", { name: "停用 系统记忆" })).toBeChecked();
});
