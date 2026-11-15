import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";

import type * as GeneratedQueries from "@/shared/api/generated/@tanstack/react-query.gen";
import type * as RuntimeQueries from "@/shared/api/runtime-queries";
import type { Waker, WakerDefaultModel } from "../model/types";
import { WakerDetailPage } from "./waker-detail-page";

type ModelRequest = {
  path: { wakerId: string };
  body: { defaultModel: WakerDefaultModel };
};

const fixture = vi.hoisted(() => {
  const waker: Waker = {};
  return {
    waker,
    save: vi.fn<(request: ModelRequest) => Promise<object>>(),
    basicSave: vi.fn(),
    runtimes: vi.fn(),
    accounts: vi.fn(),
    errorToast: vi.fn(),
  };
});

vi.mock("../api/waker-queries", () => ({
  getWakerOptions: ({ path }: { path: { wakerId: string } }) => ({
    queryKey: ["partner", path.wakerId],
    queryFn: async () => fixture.waker,
  }),
  getWakerQueryKey: ({ path }: { path: { wakerId: string } }) => [
    "partner",
    path.wakerId,
  ],
  listWakersQueryKey: () => ["wakers"],
  getWakerWorkRecordOptions: () => ({
    queryKey: ["work-record"],
    queryFn: async () => ({ activeToday: false }),
  }),
  listWakerSkillsOptions: () => ({
    queryKey: ["skills"],
    queryFn: async () => [
      { skill: { id: "skill", name: "Example" }, enabled: true },
    ],
  }),
  listWakerConnectorsOptions: () => ({
    queryKey: ["connectors"],
    queryFn: async () => [],
  }),
  updateWakerMutation: () => ({ mutationFn: fixture.basicSave }),
  updateWakerDefaultModelMutation: () => ({ mutationFn: fixture.save }),
}));

vi.mock("@/shared/api/runtime-queries", async (importOriginal) => ({
  ...(await importOriginal<typeof RuntimeQueries>()),
  listRuntimesOptions: () => ({
    queryKey: ["runtime-fixture"],
    queryFn: async () => {
      fixture.runtimes();
      return {
        resources: [
          {
            id: "fixture-runtime",
            enabled: true,
            installed: true,
            isDefault: true,
            defaultModel: "global-model",
            models: [
              { id: "old-model", name: "Old Model" },
              { id: "partner-model", name: "Partner Model" },
              { id: "global-model", name: "Global Model" },
            ],
          },
        ],
      };
    },
  }),
}));

vi.mock(
  "@/shared/api/generated/@tanstack/react-query.gen",
  async (importOriginal) => ({
    ...(await importOriginal<typeof GeneratedQueries>()),
    listAiAccountsOptions: () => ({
      queryKey: ["account-fixture"],
      queryFn: async () => {
        fixture.accounts();
        return { resources: [] };
      },
    }),
  }),
);

vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
vi.mock("@/shared/ui/avatar", () => ({ Avatar: () => null }));
vi.mock("@/shared/ui/sonner", () => ({
  toast: { error: fixture.errorToast },
}));
vi.mock("@/modules/automatic-task", () => ({
  AutomaticTaskAttentionSection: () => null,
}));
vi.mock("../components/waker-profile-section", () => ({
  WakerProfileSection: () => null,
}));
vi.mock("../components/waker-work-record-section", () => ({
  WakerWorkRecordSection: () => null,
}));

function openDetail() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/wakers/partner"]}>
        <Routes>
          <Route path="/wakers/:wakerId" element={<WakerDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

beforeEach(() => {
  fixture.waker = {
    id: "partner",
    name: "Ada",
    roleName: "方案设计师",
    bio: "维护交付边界。",
    defaultModel: {
      runtimeId: "fixture-runtime",
      accountId: "system",
      model: "old-model",
    },
  };
  fixture.save.mockReset();
  fixture.basicSave.mockReset();
  fixture.runtimes.mockClear();
  fixture.accounts.mockClear();
  fixture.errorToast.mockReset();
  fixture.save.mockImplementation(async ({ body }) => {
    fixture.waker = {
      ...fixture.waker,
      defaultModel: body.defaultModel.model ? body.defaultModel : undefined,
    };
    return {};
  });
});

it("places the default model between editing and chat and saves directly from its shared picker", async () => {
  const user = userEvent.setup();
  const client = openDetail();
  const heading = await screen.findByRole("heading", { name: /Ada/ });
  const header = heading.closest("header");
  expect(header).not.toBeNull();
  expect(
    [...(header?.querySelectorAll("button,a") ?? [])].map((action) =>
      action.getAttribute("aria-label"),
    ),
  ).toEqual(["编辑", "默认模型", "会话"]);
  expect(
    screen.getByRole("heading", { name: "技能 (1/1)" }),
  ).toBeInTheDocument();
  expect(fixture.runtimes).not.toHaveBeenCalled();
  expect(fixture.accounts).not.toHaveBeenCalled();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const model = screen.getByRole("button", { name: "默认模型" });
  await user.click(model);
  const picker = screen.getByRole("dialog", { name: "选择模型" });
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(
    await within(picker).findByRole("radio", {
      name: "Old Model",
      checked: true,
    }),
  ).toBeInTheDocument();
  await user.click(
    within(picker).getByRole("radio", { name: "Partner Model" }),
  );
  await waitFor(() => expect(fixture.save).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toHaveProperty("body", {
    defaultModel: {
      runtimeId: "fixture-runtime",
      accountId: "system",
      model: "partner-model",
    },
  });
  expect(fixture.basicSave).not.toHaveBeenCalled();
  await waitFor(() =>
    expect(client.getQueryData(["partner", "partner"])).toMatchObject({
      name: "Ada",
      roleName: "方案设计师",
      bio: "维护交付边界。",
      defaultModel: { model: "partner-model" },
    }),
  );
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ["partner", "partner"] });
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ["wakers"] });
  await waitFor(() => expect(model).not.toHaveAttribute("aria-busy"));
  await user.keyboard("{Escape}");
  expect(model).toHaveFocus();
});

it("resets the partner model to the global default through the same narrow update", async () => {
  const user = userEvent.setup();
  const client = openDetail();
  await user.click(await screen.findByRole("button", { name: "默认模型" }));
  const reset = await screen.findByRole("button", { name: "跟随全局默认" });
  await waitFor(() => expect(reset).toBeEnabled());
  await user.click(reset);
  await waitFor(() => expect(fixture.save).toHaveBeenCalledOnce());
  expect(fixture.save.mock.calls[0]?.[0]).toHaveProperty("body", {
    defaultModel: {},
  });
  await waitFor(() =>
    expect(client.getQueryData(["partner", "partner"])).toMatchObject({
      name: "Ada",
      defaultModel: undefined,
    }),
  );
  expect(fixture.basicSave).not.toHaveBeenCalled();
});

it("keeps the saved model and reports a failed change without saving basic profile fields", async () => {
  fixture.save.mockRejectedValueOnce(new Error("write failed"));
  const user = userEvent.setup();
  const client = openDetail();
  await user.click(await screen.findByRole("button", { name: "默认模型" }));
  await user.click(await screen.findByRole("radio", { name: "Partner Model" }));
  await waitFor(() =>
    expect(fixture.errorToast).toHaveBeenCalledWith(
      "暂时无法保存伙伴，请重试。",
    ),
  );
  expect(client.getQueryData(["partner", "partner"])).toMatchObject({
    name: "Ada",
    defaultModel: { model: "old-model" },
  });
  expect(fixture.basicSave).not.toHaveBeenCalled();
});
