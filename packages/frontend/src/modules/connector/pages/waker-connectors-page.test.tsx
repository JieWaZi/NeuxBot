import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { WakerConnectorsPage } from "./waker-connectors-page";
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/connector-queries", () => ({
  listWakerConnectorsOptions: ({ query }: { query?: { query: string } }) => ({
    queryKey: ["connectors", query?.query],
    queryFn: async () =>
      [
        { id: "builtin", name: "系统记忆", scope: "built_in" },
        { id: "a", name: "项目工具", scope: "public" },
      ]
        .filter((item) => item.name.includes(query?.query ?? ""))
        .map((connector) => ({
          connector: {
            ...connector,
            transport: "stdio",
            testStatus: "untested",
          },
          enabled: true,
          affectedWakerCount: 2,
          compatibility: [],
        })),
  }),
  setWakerConnectorEnabledMutation: () => ({ mutationFn: vi.fn() }),
  invalidateVisibleConnectorQueries: async () => undefined,
}));
it("伙伴列表使用目录行和搜索，新建进入页面并保留返回上下文", async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/wakers/owner/connectors?search=项目"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/connectors"
            element={<WakerConnectorsPage />}
          />
          <Route
            path="/wakers/:wakerId/connectors/new"
            element={<p>连接器新建页</p>}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await screen.findByRole("link", { name: "查看连接器 项目工具" });
  expect(screen.getByText("1 个连接器")).toBeVisible();
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索连接器" }), {
    target: { value: "" },
  });
  expect(await screen.findByText("2 个连接器")).toBeVisible();
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索连接器" }), {
    target: { value: "项目" },
  });
  expect(await screen.findByText("1 个连接器")).toBeVisible();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(screen.getByRole("searchbox", { name: "搜索连接器" })).toHaveValue(
    "项目",
  );
  fireEvent.click(screen.getByRole("button", { name: "添加连接器" }));
  expect(screen.getByText("连接器新建页")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
