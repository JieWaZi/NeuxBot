import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { ThemeProvider } from "@/shared/theme";
import { i18n } from "@/shared/i18n";
import { KnowledgePage } from "./knowledge-page";

vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<object>()),
  listAvailableLibraries: vi.fn(async () => ({
    data: { items: [], total: 0 },
  })),
}));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: vi.fn() }));

it("知识库入口先挂载搜索与操作，表单外壳同步打开且取消后舍弃未提交草稿", async () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/knowledge"]}>
          <KnowledgePage />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("searchbox", { name: "搜索知识库…" }),
  ).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "新建知识库" }));
  expect(
    screen.getByRole("dialog", { name: "新建知识库" }),
  ).toBeInTheDocument();
  const initialDialog = screen.getByRole("dialog", { name: "新建知识库" });
  const user = userEvent.setup();
  await user.type(
    await screen.findByRole("textbox", { name: "名称" }, { timeout: 5000 }),
    "待创建草稿",
  );
  expect(screen.getByRole("dialog", { name: "新建知识库" })).toBe(
    initialDialog,
  );
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("button", { name: "新建知识库" }));
  expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue("");
  await i18n.changeLanguage("en-US");
  expect(
    await screen.findByRole("dialog", { name: "New knowledge base" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("");
  await i18n.changeLanguage("zh-CN");
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);
