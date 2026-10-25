import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { CreateLibrary } from "./knowledge-library-dialogs";

function withQueryClient(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: () => ({
    selection: { runtimeId: "codex", model: "model" },
    runtimes: [],
    accounts: [],
    models: [],
    reasoningOptions: [],
    supportedPermissions: new Set(),
    loading: false,
    valid: true,
    error: null,
    nativeModelsQuery: { isFetching: false },
    thinkingQuery: { isFetching: false },
  }),
}));

it("新建请求等待时阻止重复提交，失败后保留草稿和重新提交入口", async () => {
  const user = userEvent.setup();
  let rejectRequest: ((error: Error) => void) | undefined;
  render(
    withQueryClient(
      <CreateLibrary
        opened
        close={() => {}}
        create={() =>
          new Promise((_, reject) => {
            rejectRequest = reject;
          })
        }
      />,
    ),
  );
  await user.type(
    await screen.findByRole("textbox", { name: "名称" }, { timeout: 5000 }),
    "新库草稿",
  );
  const submit = screen.getByRole("button", { name: "创建知识库" });
  await user.click(submit);
  expect(submit).toBeDisabled();
  rejectRequest?.(new Error("创建失败待重试"));
  await waitFor(() => expect(submit).toBeEnabled());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue("新库草稿");
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);
