import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  configure,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { WorkflowListPage } from "./workflow-list-page";

vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));

const origin = "http://workflow-list.test";
const server = setupServer();
let definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;
let deleted = false;

beforeAll(() => {
  configure({ asyncUtilTimeout: 5000 });
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  deleted = false;
  definition = {
    id: "flow",
    title: "周会准备流程",
    description: "生成通用议程和材料清单",
    currentRevisionId: "revision-1",
    ordinal: 1,
  };
  server.use(
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ items: deleted ? [] : [definition] }),
    ),
  );
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderList() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/workflows"]}>
        <WorkflowListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return queryClient;
}

it("后台刷新失败保留已加载卡片，并可就近重试", async () => {
  const user = userEvent.setup();
  const queryClient = renderList();
  const card = (
    await screen.findByRole("heading", { name: "周会准备流程" })
  ).closest("article");
  server.use(
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ message: "目录暂时不可用" }, { status: 503 }),
    ),
  );
  await act(async () => {
    await queryClient.invalidateQueries();
  });
  await screen.findByRole("alert");
  expect(
    screen.getByRole("heading", { name: "周会准备流程" }).closest("article"),
  ).toBe(card);
  server.use(
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({ items: [definition] }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("heading", { name: "周会准备流程" }).closest("article"),
  ).toBe(card);
});

it("从卡片编辑标题和简介，失败保留输入，保存后刷新列表及版本", async () => {
  const user = userEvent.setup();
  const requests: unknown[] = [];
  server.use(
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        requests.push(await request.json());
        if (requests.length === 1)
          return HttpResponse.json({ message: "conflict" }, { status: 409 });
        definition = {
          ...definition,
          title: "新的周会流程",
          description: "整理材料并生成行动清单",
          currentRevisionId: "revision-2",
          ordinal: 2,
        };
        return HttpResponse.json({ revision: { id: "revision-2" } });
      },
    ),
  );
  renderList();
  await user.click(
    await screen.findByRole("button", { name: "周会准备流程 的更多操作" }),
  );
  await user.click(screen.getByRole("menuitem", { name: "编辑" }));
  const dialog = await screen.findByRole("dialog", { name: "编辑基本信息" });
  const title = within(dialog).getByRole("textbox", { name: "标题" });
  const description = within(dialog).getByRole("textbox", { name: "描述" });
  expect(title).toHaveValue("周会准备流程");
  expect(description).toHaveValue("生成通用议程和材料清单");
  await user.clear(title);
  expect(within(dialog).getByRole("button", { name: "保存" })).toBeDisabled();
  await user.type(title, "新的周会流程");
  await user.clear(description);
  await user.type(description, "整理材料并生成行动清单");
  await user.click(within(dialog).getByRole("button", { name: "保存" }));
  await within(dialog).findByRole("alert");
  expect(title).toHaveValue("新的周会流程");
  expect(description).toHaveValue("整理材料并生成行动清单");
  await user.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    await screen.findByRole("heading", { name: "新的周会流程" }),
  ).toBeInTheDocument();
  expect(screen.getByText("整理材料并生成行动清单")).toBeInTheDocument();
  expect(screen.getByText("版本 2")).toBeInTheDocument();
  expect(requests[0]).toEqual({
    requestId: expect.any(String),
    expectedRevisionId: "revision-1",
    edit: {
      target: "meta",
      fields: { title: "新的周会流程", description: "整理材料并生成行动清单" },
    },
  });
  expect(requests[1]).toEqual(requests[0]);
}, 15000);

it("卡片删除需要确认，失败留在列表，重试成功移除卡片", async () => {
  const user = userEvent.setup();
  const requests: unknown[] = [];
  server.use(
    http.delete(`${origin}/api/v1/workflows/flow`, async ({ request }) => {
      requests.push(await request.json());
      if (requests.length === 1)
        return HttpResponse.json({ message: "busy" }, { status: 409 });
      deleted = true;
      return HttpResponse.json({});
    }),
  );
  renderList();
  const more = await screen.findByRole("button", {
    name: "周会准备流程 的更多操作",
  });
  await user.click(more);
  await user.click(screen.getByRole("menuitem", { name: "删除工作流" }));
  let dialog = await screen.findByRole("dialog", { name: "删除工作流" });
  expect(requests).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(requests).toHaveLength(0);
  await user.click(more);
  await user.click(screen.getByRole("menuitem", { name: "删除工作流" }));
  dialog = await screen.findByRole("dialog", { name: "删除工作流" });
  await user.click(within(dialog).getByRole("button", { name: "删除工作流" }));
  await within(dialog).findByRole("alert");
  expect(
    screen.getByRole("heading", { name: "周会准备流程", hidden: true }),
  ).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "删除工作流" }));
  await screen.findByText("创建你的第一个工作流");
  expect(
    screen.queryByRole("heading", { name: "周会准备流程" }),
  ).not.toBeInTheDocument();
  expect(requests).toEqual([
    { expectedRevisionId: "revision-1" },
    { expectedRevisionId: "revision-1" },
  ]);
}, 15000);

it("尚未保存版本的工作流可删除，编辑与详情页一样不可用", async () => {
  const user = userEvent.setup();
  definition = { ...definition, currentRevisionId: "", ordinal: 0 };
  renderList();
  await user.click(
    await screen.findByRole("button", { name: "周会准备流程 的更多操作" }),
  );
  expect(screen.getByRole("menuitem", { name: "编辑" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(
    screen.getByRole("menuitem", { name: "删除工作流" }),
  ).not.toHaveAttribute("aria-disabled");
});
