import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { delay, http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import type { NewChatComposer } from "@/modules/chat";
import { listWakersOptions } from "@/modules/waker";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { initializeApi } from "@/shared/api/runtime";
import { configurePlatform, platform } from "@/shared/platform/provider";
import { WorkBoardIntro } from "./work-board-intro";

// 以公开输入区边界观察实际接收草稿的伙伴，完整首发由看板集成测试覆盖。
vi.mock("@/modules/chat", () => ({
  NewChatComposer: ({
    partner,
    renderPartnerPicker,
  }: Parameters<typeof NewChatComposer>[0]) => (
    <>
      <output aria-label="草稿伙伴">{partner?.id ?? "无"}</output>
      <textarea aria-label="消息" />
      {renderPartnerPicker(false)}
    </>
  ),
}));
vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: () => null,
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: () => () => {},
}));

const origin = "http://work-board-intro.test";
const original = { id: "a".repeat(32), name: "当前伙伴", isDefault: true };
const another = { id: "b".repeat(32), name: "设计伙伴", isDefault: true };
const server = setupServer();

beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(async () => {
  configurePlatform({
    ...platform(),
    getConnection: async () => ({ baseUrl: origin }),
  });
  await initializeApi();
  vi.stubGlobal("matchMedia", () => ({
    matches: true,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
});
afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
});
afterAll(() => server.close());

function partnerPage(
  resources: (typeof original)[],
  totalSize = resources.length,
) {
  return HttpResponse.json({ resources, pageNum: 1, pageSize: 24, totalSize });
}

function mount() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkBoardIntro active />
    </QueryClientProvider>,
  );
  return queryClient;
}

it("伙伴列表重排保留草稿收件人，原伙伴删除后要求重新选择", async () => {
  let partners = [original, another];
  const reads: string[] = [];
  server.use(
    http.get(`${origin}/api/v1/wakers`, () => {
      reads.push(partners[0]?.id ?? "");
      return partnerPage(partners);
    }),
    http.get(`${origin}/api/v1/wakers/${original.id}`, () =>
      partners.includes(original)
        ? HttpResponse.json(original)
        : new HttpResponse(null, { status: 404 }),
    ),
  );
  const queryClient = mount();
  const user = userEvent.setup();
  const recipient = screen.getByLabelText("草稿伙伴");
  await waitFor(() => expect(recipient).toHaveTextContent(original.id));
  await user.type(screen.getByRole("textbox", { name: "消息" }), "草稿内容");

  partners = [another, original];
  await act(() =>
    queryClient.invalidateQueries({ queryKey: listWakersOptions().queryKey }),
  );
  expect(reads.at(-1)).toBe(another.id);
  await user.click(screen.getByLabelText("选择伙伴"));
  const options = await screen.findAllByRole("button", {
    name: /^(设计伙伴|当前伙伴)$/,
  });
  expect(options[0]).toHaveAccessibleName(another.name);
  expect(recipient).toHaveTextContent(original.id);
  await user.keyboard("{Escape}");

  partners = [another];
  await act(() =>
    queryClient.invalidateQueries({
      queryKey: getWakerOptions({ path: { wakerId: original.id } }).queryKey,
    }),
  );
  await waitFor(() => expect(recipient).toHaveTextContent("无"));
  expect(screen.getByRole("textbox", { name: "消息" })).toHaveValue("草稿内容");
  expect(screen.getByRole("button", { name: "选择伙伴" })).toBeEnabled();
});

it("键盘可以搜索后续页伙伴，打开选择器只读取当前页", async () => {
  const requests: URLSearchParams[] = [];
  server.use(
    http.get(`${origin}/api/v1/wakers`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      requests.push(params);
      return params.get("query") === another.name
        ? partnerPage([another])
        : partnerPage([original], 301);
    }),
    http.get(`${origin}/api/v1/wakers/${original.id}`, () =>
      HttpResponse.json(original),
    ),
    http.get(`${origin}/api/v1/wakers/${another.id}`, () =>
      HttpResponse.json(another),
    ),
  );
  mount();
  const user = userEvent.setup();
  const trigger = screen.getByLabelText("选择伙伴");
  await waitFor(() => expect(trigger).toBeEnabled());
  trigger.focus();
  await user.keyboard("{ArrowDown}");
  const search = await screen.findByRole("searchbox", { name: "搜索伙伴" });
  expect(search).toHaveFocus();
  expect(requests.every((params) => params.get("pageNum") === "1")).toBe(true);
  await user.type(search, another.name);
  const option = await screen.findByRole("button", { name: another.name });
  expect(requests.at(-1)?.get("query")).toBe(another.name);
  expect(requests.at(-1)?.get("pageSize")).toBe("24");
  await user.keyboard("{ArrowDown}");
  expect(option).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(screen.getByLabelText("草稿伙伴")).toHaveTextContent(another.id);
  await waitFor(() => expect(trigger).toHaveFocus());
});

it("首次读取伙伴时在可输入的草稿旁展示加载状态", async () => {
  server.use(
    http.get(`${origin}/api/v1/wakers`, async () => {
      await delay(100);
      return partnerPage([]);
    }),
  );
  mount();
  expect(screen.getByRole("textbox", { name: "消息" })).toBeEnabled();
  expect(screen.getByRole("status", { name: "正在读取伙伴" })).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(await screen.findByText("暂无可选伙伴")).toBeInTheDocument();
});
