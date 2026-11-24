import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  MemoryRouter,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";

import { client } from "@/shared/api/generated/client.gen";
import { getWakerReviewSettingsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { isPartnerReviewDetail } from "@/shared/lib/partner-review-settings";
import { RuntimeEnvironmentsSection } from "./runtime-environments-section";
import "@/shared/i18n";

vi.mock(import("@/shared/api/global-events"), async (load) => ({
  ...(await load()),
  subscribeGlobalEvents: () => () => undefined,
}));

const base = "http://127.0.0.1:8080";
const api = `${base}/api/v1`;
const review = `${api}/review`;
const partners = `${review}/wakers/settings`;
const secondPartner = `${review}/wakers/waker-2`;
const server = setupServer(
  http.get(`${api}/runtimes`, () => HttpResponse.json({ resources: [] })),
  http.get(`${review}/settings`, () =>
    HttpResponse.json({ source: "partner" }),
  ),
);
const initialRows = [
  {
    wakerId: "waker-1",
    name: "第一位",
    avatar: "",
    memoryEnabled: false,
    skillMode: "off",
  },
  {
    wakerId: "waker-2",
    name: "第二位",
    avatar: "",
    memoryEnabled: false,
    skillMode: "off",
  },
];

beforeAll(() => {
  client.setConfig({ baseUrl: base });
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.scrollIntoView = () => undefined;
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function RoutedSettings() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <RuntimeEnvironmentsSection
        partnerReviewDetail={isPartnerReviewDetail(params)}
        onOpenRuntime={() => undefined}
        onBack={() => undefined}
        showSceneChrome={false}
      />
      <output aria-label="当前设置地址">
        {location.pathname}
        {location.search}
      </output>
      <button type="button" onClick={() => navigate(-1)}>
        浏览器返回
      </button>
    </>
  );
}

function show(
  href = "/settings/agents",
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  }),
) {
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[href]}>
        <TooltipProvider>
          <RoutedSettings />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return userEvent.setup();
}

it("loads the partner list once, saves each feature independently and restores the previous tab on back", async () => {
  const rows = structuredClone(initialRows);
  const second = rows[1];
  if (!second) throw new Error("missing partner fixture");
  const requests = vi.fn();
  const list = vi.fn();
  const detailReads = vi.fn();
  server.use(
    http.get(partners, () => {
      list();
      return HttpResponse.json({ resources: rows, totalSize: 2 });
    }),
    http.get(secondPartner, () => {
      detailReads();
      return HttpResponse.json({ ...rows[1], available: true, status: "idle" });
    }),
    http.put(secondPartner, async ({ request }) => {
      const body = (await request.json()) as {
        memoryEnabled: boolean;
        skillMode: string;
      };
      requests(body);
      Object.assign(second, body);
      return HttpResponse.json({ ...body, available: true, status: "idle" });
    }),
  );
  const user = show();
  expect(screen.queryByRole("switch", { name: "第二位的自动记忆" })).toBeNull();
  await user.click(await screen.findByRole("button", { name: "配置伙伴" }));
  const address = screen.getByLabelText("当前设置地址");
  expect(address).toHaveTextContent(
    "/settings/agents?detail=partner-review&tab=memory",
  );
  expect(
    await screen.findByRole("switch", { name: "第一位的自动记忆" }),
  ).not.toBeChecked();
  const memory = screen.getByRole("switch", { name: "第二位的自动记忆" });
  expect(memory).not.toBeChecked();
  await user.click(memory);
  await waitFor(() => expect(memory).toBeChecked());
  expect(requests).toHaveBeenNthCalledWith(1, {
    memoryEnabled: true,
    skillMode: "off",
  });
  await user.click(screen.getByRole("radio", { name: "技能学习" }));
  expect(address).toHaveTextContent("tab=skill");
  const skill = screen.getByRole("switch", { name: "第二位的技能学习" });
  expect(skill).not.toBeChecked();
  expect(
    screen.queryByRole("combobox", { name: "第二位的技能保存方式" }),
  ).toBeNull();
  await user.click(skill);
  const mode = await screen.findByRole("combobox", {
    name: "第二位的技能保存方式",
  });
  expect(mode).toHaveTextContent("先询问");
  expect(mode.parentElement).toBe(skill.parentElement);
  expect(mode.parentElement).toHaveClass("flex-nowrap", "justify-end");
  expect(mode.nextElementSibling).toBe(skill);
  expect(mode).not.toHaveClass("w-full");
  await waitFor(() => expect(mode).toBeEnabled());
  await user.click(mode);
  await user.click(screen.getByRole("option", { name: "自动保存" }));
  await waitFor(() => expect(mode).toHaveTextContent("自动保存"));
  expect(requests).toHaveBeenNthCalledWith(2, {
    memoryEnabled: true,
    skillMode: "ask",
  });
  expect(requests).toHaveBeenNthCalledWith(3, {
    memoryEnabled: true,
    skillMode: "automatic",
  });
  expect(detailReads).not.toHaveBeenCalled();
  expect(list).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "浏览器返回" }));
  expect(
    screen.getByRole("switch", { name: "第二位的自动记忆" }),
  ).toBeChecked();
  await user.click(screen.getByRole("button", { name: "浏览器返回" }));
  expect(await screen.findByRole("button", { name: "配置伙伴" })).toBeVisible();
  expect(address).toHaveTextContent(/^\/settings\/agents$/);
});

it("locates a linked partner outside the first page and retries only that partner", async () => {
  const profile = { id: "waker-120", name: "第 120 位", avatar: "" };
  const endpoint = `${review}/wakers/${profile.id}`;
  let failed = true;
  server.use(
    http.get(partners, () =>
      HttpResponse.json({ resources: initialRows, totalSize: 120 }),
    ),
    http.get(`${api}/wakers/${profile.id}`, () => HttpResponse.json(profile)),
    http.get(endpoint, () =>
      HttpResponse.json({
        available: true,
        status: failed ? "failed" : "idle",
        memoryEnabled: true,
        skillMode: "automatic",
      }),
    ),
    http.post(`${endpoint}/retry`, () => {
      failed = false;
      return HttpResponse.json({});
    }),
  );
  const user = show(
    "/settings/agents?detail=partner-review&tab=skill&wakerId=waker-120",
  );
  expect(
    await screen.findByRole("switch", { name: "第 120 位的技能学习" }),
  ).toBeChecked();
  expect(
    screen.getByRole("combobox", { name: "第 120 位的技能保存方式" }),
  ).toHaveTextContent("自动保存");
  const notice = await screen.findByRole("alert");
  expect(notice).toHaveTextContent("有对话处理失败，可以重试。");
  await user.click(screen.getByRole("button", { name: "重试复盘" }));
  await waitFor(() => expect(notice).not.toBeInTheDocument());
});

it("loads and filters server pages without per-row diagnostic requests", async () => {
  const load = vi.fn();
  const all = Array.from({ length: 58 }, (_, index) => ({
    wakerId: String(index + 1),
    name: index === 57 ? "尾页测试伙伴" : `伙伴 ${index + 1}`,
    memoryEnabled: false,
    skillMode: "off",
  }));
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(partners, async ({ request }) => {
      const params = new URL(request.url).searchParams;
      const page = Number(params.get("pageNum"));
      const size = Number(params.get("pageSize"));
      const q = params.get("query") || "";
      load(page, size, q);
      await gate;
      const found = all.filter(({ name }) => name.includes(q));
      return HttpResponse.json({
        resources: found.slice((page - 1) * size, page * size),
        totalSize: found.length,
      });
    }),
  );
  const user = show("/settings/agents?wakerId=");
  expect(
    await screen.findByRole("status", { name: "正在载入配置…" }),
  ).toBeVisible();
  release();
  expect(
    await screen.findByRole("switch", { name: "伙伴 1的自动记忆" }),
  ).not.toBeChecked();
  expect(
    screen.queryByRole("switch", { name: "尾页测试伙伴的自动记忆" }),
  ).toBeNull();
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(
    await screen.findByRole("switch", { name: "尾页测试伙伴的自动记忆" }),
  ).toBeVisible();
  expect(load).toHaveBeenCalledWith(2, 50, "");
  const search = screen.getByRole("searchbox", { name: "搜索伙伴" });
  await user.click(search);
  await user.paste("尾页");
  expect(
    await screen.findByRole("switch", { name: "尾页测试伙伴的自动记忆" }),
  ).toBeVisible();
  expect(load).toHaveBeenCalledWith(1, 50, "尾页");
  expect(screen.queryByRole("button", { name: "下一页" })).toBeNull();
});

it("saves only the review frequency and displays the confirmed value", async () => {
  const save = vi.fn();
  server.use(
    http.put(`${review}/settings`, async ({ request }) => {
      const body = await request.json();
      save(body);
      return HttpResponse.json({ source: "partner", everyTurns: 10 });
    }),
  );
  const user = show();
  const interval = await screen.findByRole("combobox", { name: "整理频率" });
  expect(interval).toHaveTextContent("每 20 轮");
  await user.click(interval);
  for (const turns of [5, 10, 20, 30, 50]) {
    expect(
      screen.getByRole("option", { name: `每 ${turns} 轮` }),
    ).toBeVisible();
  }
  await user.click(screen.getByRole("option", { name: "每 10 轮" }));
  await waitFor(() => expect(interval).toHaveTextContent("每 10 轮"));
  expect(save).toHaveBeenCalledExactlyOnceWith({ everyTurns: 10 });
  expect(
    screen.getByRole("combobox", { name: "使用的智能体" }),
  ).toHaveTextContent("跟随伙伴");
});

it("opens semantic search inline from its URL and collapses it on cancel", async () => {
  server.use(
    http.get(`${api}/memory/settings`, () =>
      HttpResponse.json({ baseUrl: "", model: "", apiKeyConfigured: false }),
    ),
    http.get(`${api}/memory/status`, () =>
      HttpResponse.json({ configured: false }),
    ),
  );
  const user = show("/settings/agents?semanticSearch=1");
  expect(
    await screen.findByRole("textbox", { name: "服务地址" }),
  ).toBeVisible();
  expect(screen.queryByRole("dialog")).toBeNull();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("textbox", { name: "服务地址" })).toBeNull();
  expect(screen.getByLabelText("当前设置地址")).toHaveTextContent(
    /^\/settings\/agents$/,
  );
});

it("uses the shared runtime catalog to select and save a dedicated Codex configuration", async () => {
  let saved: unknown;
  server.use(
    http.get(`${api}/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            installed: true,
            state: "ready",
            defaultAccountId: "system",
            defaultModel: "gpt-6.2-sol",
          },
          {
            id: "claude",
            name: "Claude",
            enabled: true,
            installed: true,
            state: "ready",
            defaultAccountId: "system",
            defaultModel: "sonnet",
          },
        ],
      }),
    ),
    http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
    http.get(`${api}/runtimes/claude/models`, () =>
      HttpResponse.json({
        state: "fresh",
        defaultModel: "sonnet",
        models: [{ id: "sonnet", name: "Claude Sonnet" }],
      }),
    ),
    http.get(`${api}/runtimes/codex/models`, () =>
      HttpResponse.json({
        state: "fresh",
        defaultModel: "gpt-6.2-sol",
        models: [{ id: "gpt-6.2-sol", name: "GPT-6.2 Sol" }],
      }),
    ),
    http.put(`${review}/settings`, async ({ request }) => {
      saved = await request.json();
      return HttpResponse.json({
        source: "global",
        runtimeId: "codex",
        accountId: "system",
        model: "gpt-6.2-sol",
      });
    }),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const cachedKeys = initialRows.map(({ wakerId }) =>
    getWakerReviewSettingsQueryKey({ path: { wakerId } }),
  );
  for (const key of cachedKeys)
    queryClient.setQueryData(key, { source: "partner" });
  const user = show(undefined, queryClient);
  await user.click(
    await screen.findByRole("combobox", { name: "使用的智能体" }),
  );
  await user.click(screen.getByRole("option", { name: "指定智能体" }));
  await user.click(screen.getByRole("combobox", { name: "智能体" }));
  expect(await screen.findByRole("option", { name: "Claude" })).toBeVisible();
  await user.click(screen.getByRole("option", { name: "Codex" }));
  const save = screen.getByRole("button", { name: "保存" });
  await waitFor(() => expect(save).toBeEnabled());
  await user.click(save);

  await waitFor(() =>
    expect(saved).toEqual({
      source: "global",
      runtimeId: "codex",
      accountId: "system",
      model: "gpt-6.2-sol",
    }),
  );
  const source = screen.getByRole("combobox", { name: "使用的智能体" });
  const configure = await screen.findByRole("button", { name: "配置" });
  expect(source).not.toHaveClass("w-full");
  expect(configure.parentElement).toBe(source.parentElement);
  expect(source.parentElement).toHaveClass("items-center");
  expect(screen.queryByText("复盘 CLI")).toBeNull();
  for (const key of cachedKeys) {
    expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
  }
});
