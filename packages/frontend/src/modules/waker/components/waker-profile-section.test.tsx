import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type * as WakerQueries from "../api/waker-queries";
import { WakerProfileSection } from "./waker-profile-section";

const fixture = vi.hoisted(() => ({
  content: "# 协作说明\n\n先确认 **交付目标**，再给出建议。",
  observers: new Set<GlobalEventObserver>(),
  start: vi.fn(),
  save: vi.fn(),
}));

vi.mock("../api/waker-queries", async (importOriginal) => ({
  ...(await importOriginal<typeof WakerQueries>()),
  getWakerDocumentOptions: ({
    path,
  }: {
    path: { wakerId: string; kind: string };
  }) => ({
    queryKey: ["waker-document", path],
    queryFn: async () => ({
      content: fixture.content,
      fileName: "AGENTS.md",
      kind: path.kind,
      editable: true,
    }),
  }),
  getWakerDocumentQueryKey: ({
    path,
  }: {
    path: { wakerId: string; kind: string };
  }) => ["waker-document", path],
  listWakerDocumentsOptions: () => ({
    queryKey: ["legacy-profile-files"],
    queryFn: async () => [
      { fileName: "IDENTITY.md", kind: "identity", editable: true },
    ],
  }),
  updateWakerDocumentMutation: () => ({ mutationFn: fixture.save }),
  startWakerOnboardingMutation: () => ({ mutationFn: fixture.start }),
}));

vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.observers.add(observer);
    return () => fixture.observers.delete(observer);
  },
}));

function LocationProbe() {
  const { pathname, search } = useLocation();
  return (
    <output aria-label="当前位置">
      {pathname}
      {search}
    </output>
  );
}

function renderProfile(pending = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter>
          <LocationProbe />
          <WakerProfileSection
            waker={{
              id: "partner",
              ...(pending
                ? {
                    onboardingStatus: "pending",
                    onboardingChatId: "reserved-chat",
                  }
                : {}),
            }}
          />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  fixture.content = "# 协作说明\n\n先确认 **交付目标**，再给出建议。";
  fixture.start.mockReset();
  fixture.save.mockReset();
  fixture.observers.clear();
});

it("在伙伴档案直接渲染 Markdown，编辑动作仅保存正文", async () => {
  renderProfile();
  const user = userEvent.setup();
  expect(
    await screen.findByRole("heading", { name: "协作说明" }),
  ).toBeVisible();
  expect(screen.getByText("交付目标")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: /IDENTITY.md|下载伙伴档案/ }),
  ).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "编辑伙伴档案" }));
  const editor = screen.getByRole("textbox", { name: "伙伴档案 Markdown" });
  expect(editor).toHaveValue(fixture.content);
  fixture.save.mockImplementationOnce(
    async ({ body }: { body: { content: string } }) => {
      fixture.content = body.content;
    },
  );
  await user.clear(editor);
  await user.type(editor, "# 新的协作说明");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(
    await screen.findByRole("heading", { name: "新的协作说明" }),
  ).toBeVisible();
  expect(fixture.save).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { wakerId: "partner", kind: "agents" },
      body: { content: "# 新的协作说明" },
    }),
    expect.anything(),
  );
});

it("建档启动失败后继续在同一伙伴重试，进入服务器返回的对话", async () => {
  fixture.content = "";
  fixture.start
    .mockResolvedValueOnce({
      wakerId: "partner",
      chatId: "onboarding-chat",
      startError: "模型暂不可用",
    })
    .mockResolvedValueOnce({ wakerId: "partner", chatId: "onboarding-chat" });
  renderProfile(true);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "继续设置" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("模型暂不可用");
  expect(screen.getByRole("link", { name: "打开对话" })).toHaveAttribute(
    "href",
    "/chats/onboarding-chat?wakerId=partner",
  );
  await user.click(screen.getByRole("button", { name: "继续设置" }));
  await waitFor(() =>
    expect(screen.getByLabelText("当前位置")).toHaveTextContent(
      "/chats/onboarding-chat?wakerId=partner",
    ),
  );
  expect(fixture.start).toHaveBeenNthCalledWith(
    2,
    expect.objectContaining({ path: { wakerId: "partner" } }),
    expect.anything(),
  );
});

it("伙伴资源事件刷新已经打开的档案正文", async () => {
  renderProfile();
  expect(
    await screen.findByRole("heading", { name: "协作说明" }),
  ).toBeVisible();
  fixture.content = "# 已确认的档案";
  act(() => {
    for (const observer of fixture.observers) {
      observer.onEvent({
        protocolVersion: 1,
        epoch: "core",
        connectionId: "test",
        deliverySequence: 1,
        eventId: "event",
        topic: "resources",
        type: "resources.changed",
        resource: { kind: "waker", id: "partner" },
        payload: { kind: "waker", id: "partner" },
      });
    }
  });
  expect(
    await screen.findByRole("heading", { name: "已确认的档案" }),
  ).toBeVisible();
});
