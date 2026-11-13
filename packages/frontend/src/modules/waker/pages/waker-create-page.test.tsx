import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeAll, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type {
  CreateWakerData,
  CreateWakerResponse,
} from "@/shared/api/generated/types.gen";
import type * as GeneratedQueries from "@/shared/api/generated/@tanstack/react-query.gen";
import { WakerCreatePage } from "./waker-create-page";

const create = vi.hoisted(() =>
  vi.fn<(data: CreateWakerData) => Promise<CreateWakerResponse>>(),
);
const directoryReads = vi.hoisted(() => ({
  runtimes: vi.fn(),
  accounts: vi.fn(),
}));
vi.mock(
  "@/shared/api/generated/@tanstack/react-query.gen",
  async (importOriginal) => ({
    ...(await importOriginal<typeof GeneratedQueries>()),
    createWakerMutation: () => ({ mutationFn: create }),
    listRuntimesOptions: () => ({
      queryKey: ["runtime-fixture"],
      queryFn: async () => {
        directoryReads.runtimes();
        return {
          resources: [
            {
              id: "fixture-runtime",
              name: "Fixture Agent",
              enabled: true,
              isDefault: true,
              state: "ready",
              defaultModel: "global-model",
              models: [
                {
                  id: "partner-model",
                  name: "Partner Model",
                  reasoningOptions: [{ id: "high", name: "High" }],
                },
                { id: "global-model", name: "Global Model" },
              ],
            },
          ],
        };
      },
    }),
    listAiAccountsOptions: () => ({
      queryKey: ["accounts-fixture"],
      queryFn: async () => {
        directoryReads.accounts();
        return { resources: [] };
      },
    }),
  }),
);
vi.mock("@/shared/ui/avatar", () => ({
  Avatar: ({ size }: { size: string }) => <span data-testid={size} />,
}));

function CreateScene() {
  const { pathname, search, state } = useLocation();
  return pathname === "/wakers/new" ? (
    <WakerCreatePage />
  ) : (
    <output aria-label="目标 Scene">
      {pathname}
      {search}
      {typeof state?.onboardingStartError === "string"
        ? state.onboardingStartError
        : null}
    </output>
  );
}

function renderCreate() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/wakers/new"]}>
        <CreateScene />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeAll(() => {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.scrollIntoView = () => {};
});
afterEach(() => {
  create.mockReset();
  directoryReads.runtimes.mockClear();
  directoryReads.accounts.mockClear();
});

it("跟随全局默认时仅在打开模型选择器后读取目录", async () => {
  renderCreate();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox", { name: "名称" }), "协作伙伴");
  expect(directoryReads.runtimes).not.toHaveBeenCalled();
  expect(directoryReads.accounts).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "默认模型" }));
  expect(
    await screen.findByRole("radio", { name: "Global Model" }),
  ).toBeVisible();
  expect(directoryReads.runtimes).toHaveBeenCalled();
  expect(directoryReads.accounts).toHaveBeenCalled();
});

it("选择默认模型后携带原请求标识重试，进入已接纳的建档对话", async () => {
  create
    .mockRejectedValueOnce(new Error("connection interrupted"))
    .mockResolvedValueOnce({ wakerId: "new-partner", chatId: "setup-chat" });
  renderCreate();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox", { name: "名称" }), "协作伙伴");
  await user.type(
    screen.getByRole("textbox", { name: "概要" }),
    "整理协作记录",
  );
  expect(screen.getByTestId("hero")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "粒子" }));
  await user.click(screen.getByRole("button", { name: "默认模型" }));
  const picker = screen.getByRole("dialog", { name: "选择模型" });
  await within(picker).findByRole("radio", { name: "Partner Model" });
  await user.type(within(picker).getByRole("searchbox"), "Partner");
  await user.click(
    within(picker).getByRole("radio", { name: "Partner Model" }),
  );
  await user.click(within(picker).getByRole("combobox"));
  await user.click(await screen.findByRole("option", { name: "高" }));
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "默认模型" })).toHaveTextContent(
    "Partner Model",
  );
  expect(
    screen.queryByRole("dialog", { name: "选择模型" }),
  ).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "创建伙伴" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "暂时无法创建伙伴",
  );
  await user.click(screen.getByRole("button", { name: "创建伙伴" }));
  expect(await screen.findByLabelText("目标 Scene")).toHaveTextContent(
    "/chats/setup-chat?wakerId=new-partner",
  );

  const first = create.mock.calls[0]?.[0].body;
  expect(first).toEqual({
    requestId: expect.any(String),
    name: "协作伙伴",
    bio: "整理协作记录",
    avatar: expect.stringMatching(/^bloub:particles:/),
    defaultModel: {
      runtimeId: "fixture-runtime",
      accountId: "system",
      model: "partner-model",
      reasoning: "high",
    },
  });
  expect(create.mock.calls[1]?.[0].body).toEqual(first);
});

it("模型启动失败时进入已持久化的伙伴概览，保留具体原因", async () => {
  create.mockResolvedValueOnce({
    wakerId: "new-partner",
    startError: "账号已失效",
  });
  renderCreate();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox", { name: "名称" }), "协作伙伴");
  await user.click(screen.getByRole("button", { name: "默认模型" }));
  const picker = screen.getByRole("dialog", { name: "选择模型" });
  await user.click(
    await within(picker).findByRole("radio", { name: "Global Model" }),
  );
  await user.click(
    within(picker).getByRole("button", { name: "跟随全局默认" }),
  );
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "创建伙伴" }));
  expect(await screen.findByLabelText("目标 Scene")).toHaveTextContent(
    "/wakers/new-partner账号已失效",
  );
  expect(create.mock.calls[0]?.[0].body.defaultModel).toBeUndefined();
});
