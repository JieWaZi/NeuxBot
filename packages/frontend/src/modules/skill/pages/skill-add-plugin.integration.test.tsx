import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
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
import { toApiError } from "@/shared/api/errors";
import type { PluginDetail } from "@/modules/plugin";
import { SkillAddPage } from "./skill-add-page";

vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => (
    <main>{children}</main>
  ),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: () => () => undefined,
}));

const base = "http://skill-plugin.test/api/v1";
const server = setupServer();
const clients: QueryClient[] = [];
const createdBodies: unknown[] = [];
const installedBodies: unknown[] = [];
let independentImports = 0;
let errorInterceptor: ReturnType<typeof client.interceptors.error.use>;
const detail: PluginDetail = {
  plugin: { id: "created-plugin", name: "研发组合" },
  resolvedVersionId: "created-version",
  versions: [
    {
      id: "created-version",
      package: {
        contractVersion: "neuxbot-plugin/1",
        manifest: { name: "研发组合" },
        skills: [
          { key: "skills/review", name: "review" },
          { key: "skills/writing", name: "writing" },
        ],
      },
    },
  ],
  installations: [],
  configurationRevision: 7,
  accountGeneration: 9,
};
const candidates = [
  { id: "skills/review", name: "review", description: "代码审查" },
  { id: "skills/writing", name: "writing", description: "技术写作" },
  { id: "skills/unused", name: "unused", description: "未选择的技能" },
];
function failure(message: string) {
  return HttpResponse.json(
    { key: "PluginContentInvalid", message, messageSource: "public" },
    { status: 400 },
  );
}
beforeAll(() => {
  client.setConfig({ baseUrl: "http://skill-plugin.test" });
  errorInterceptor = client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  server.use(
    http.post(`${base}/skills/import/scan`, () =>
      HttpResponse.json({ token: "scan-token", candidates }),
    ),
    http.post(`${base}/skills/import`, () => {
      independentImports++;
      return HttpResponse.json([]);
    }),
    http.post(`${base}/plugins/from-skills`, async ({ request }) => {
      createdBodies.push(await request.json());
      return HttpResponse.json(detail);
    }),
    http.get(`${base}/wakers/waker-1`, () =>
      HttpResponse.json({ id: "waker-1", name: "当前伙伴" }),
    ),
    http.get(`${base}/plugins/created-plugin`, () => HttpResponse.json(detail)),
    http.get(`${base}/plugins/created-plugin/configuration`, () =>
      HttpResponse.json({
        versionId: "created-version",
        fields: [],
        values: {},
        configuredSecrets: [],
        ready: true,
        configurationRevision: 7,
        accountGeneration: 9,
      }),
    ),
    http.post(
      `${base}/plugins/created-plugin/installations`,
      async ({ request }) => {
        installedBodies.push(await request.json());
        return HttpResponse.json({
          results: [{ wakerId: "waker-1", installed: true }],
        });
      },
    ),
  );
});
afterEach(async () => {
  cleanup();
  await waitFor(() =>
    expect(
      clients
        .flatMap((queryClient) => queryClient.getMutationCache().getAll())
        .filter((mutation) => mutation.state.status === "pending"),
    ).toHaveLength(0),
  );
  clients.forEach((queryClient) => queryClient.clear());
  clients.length = 0;
  createdBodies.length = 0;
  installedBodies.length = 0;
  independentImports = 0;
  server.resetHandlers();
  vi.unstubAllGlobals();
});
afterAll(() => {
  client.interceptors.error.eject(errorInterceptor);
  server.close();
});
function Destination() {
  const location = useLocation();
  return (
    <>
      <output aria-label="目标页面">
        {location.pathname + location.search}
      </output>
      <output aria-label="返回列表">{location.state?.resourceBack}</output>
    </>
  );
}
function show(partner = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(queryClient);
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[
          partner
            ? "/wakers/waker-1/resources/skills/new"
            : "/resources/skills/new",
        ]}
      >
        <Routes>
          <Route path="/resources/skills/new" element={<SkillAddPage />} />
          <Route
            path="/wakers/:wakerId/resources/skills/new"
            element={<SkillAddPage />}
          />
          <Route path="*" element={<Destination />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
async function namePlugin() {
  fireEvent.change(screen.getByRole("textbox", { name: "GitHub 地址" }), {
    target: { value: "https://github.com/example/skills.git" },
  });
  fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "选择 unused" }));
  fireEvent.click(screen.getByRole("button", { name: "添加为插件" }));
  fireEvent.change(screen.getByRole("textbox", { name: "插件名称" }), {
    target: { value: "  研发组合  " },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "说明（可选）" }), {
    target: { value: "  审查与写作  " },
  });
}
async function expectPluginDetail() {
  expect(await screen.findByLabelText("目标页面")).toHaveTextContent(
    "/resources/plugins/created-plugin?version=created-version",
  );
  expect(screen.getByLabelText("返回列表")).toHaveTextContent(
    "/resources?category=plugins",
  );
}

it("creates only the selected skill IDs through the generated client and opens the real plugin identity", async () => {
  show();
  await namePlugin();
  expect(
    within(screen.getByRole("region", { name: "所选技能" })).queryByText(
      "unused",
    ),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  await expectPluginDetail();
  expect(createdBodies).toEqual([
    {
      token: "scan-token",
      candidateIds: ["skills/review", "skills/writing"],
      name: "研发组合",
      description: "审查与写作",
    },
  ]);
  expect(independentImports).toBe(0);
  expect(installedBodies).toHaveLength(0);
});

it("retains the naming draft and scan selection after a creation failure, then retries the same request", async () => {
  let attempts = 0;
  server.use(
    http.post(`${base}/plugins/from-skills`, async ({ request }) => {
      createdBodies.push(await request.json());
      return ++attempts === 1
        ? failure("所选技能无法组合")
        : HttpResponse.json(detail);
    }),
  );
  show();
  await namePlugin();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "所选技能无法组合",
  );
  expect(screen.getByRole("textbox", { name: "插件名称" })).toHaveValue(
    "  研发组合  ",
  );
  expect(screen.getByRole("textbox", { name: "说明（可选）" })).toHaveValue(
    "  审查与写作  ",
  );
  expect(
    within(screen.getByRole("region", { name: "所选技能" })).getByText(
      "writing",
    ),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  await expectPluginDetail();
  expect(createdBodies).toHaveLength(2);
  expect(createdBodies[0]).toEqual(createdBodies[1]);
  expect(independentImports).toBe(0);
});

it("keeps submission and cancellation mutually exclusive while creation is pending", async () => {
  let finish: (() => void) | undefined;
  server.use(
    http.post(`${base}/plugins/from-skills`, async ({ request }) => {
      createdBodies.push(await request.json());
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
      return HttpResponse.json(detail);
    }),
  );
  show();
  await namePlugin();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  await waitFor(() => expect(finish).toBeDefined());
  expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
  expect(screen.getByRole("textbox", { name: "插件名称" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "正在创建插件…" })).toBeDisabled();
  await act(async () => finish?.());
  await expectPluginDetail();
  expect(createdBodies).toHaveLength(1);
});

it("keeps the created plugin when partner installation is cancelled without importing independent skills", async () => {
  show(true);
  await namePlugin();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  const dialog = await screen.findByRole("dialog", { name: "安装 研发组合" });
  expect(installedBodies).toHaveLength(0);
  fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  await expectPluginDetail();
  expect(createdBodies).toHaveLength(1);
  expect(installedBodies).toHaveLength(0);
  expect(independentImports).toBe(0);
});

it("retries explicit partner installation using the existing dialog without creating the plugin again", async () => {
  let attempts = 0;
  server.use(
    http.post(
      `${base}/plugins/created-plugin/installations`,
      async ({ request }) => {
        installedBodies.push(await request.json());
        return ++attempts === 1
          ? failure("安装暂时失败")
          : HttpResponse.json({
              results: [{ wakerId: "waker-1", installed: true }],
            });
      },
    ),
  );
  show(true);
  await namePlugin();
  fireEvent.click(screen.getByRole("button", { name: "创建插件" }));
  const dialog = await screen.findByRole("dialog", { name: "安装 研发组合" });
  const submit = within(dialog).getByRole("button", { name: "继续" });
  await waitFor(() => expect(submit).toBeEnabled());
  fireEvent.click(submit);
  expect(await within(dialog).findByRole("alert")).toHaveTextContent(
    "安装暂时失败",
  );
  fireEvent.click(submit);
  await expectPluginDetail();
  expect(createdBodies).toHaveLength(1);
  expect(installedBodies).toEqual([
    {
      versionId: "created-version",
      wakerIds: ["waker-1"],
      expectedConfigurationRevision: 7,
      expectedAccountGeneration: 9,
    },
    {
      versionId: "created-version",
      wakerIds: ["waker-1"],
      expectedConfigurationRevision: 7,
      expectedAccountGeneration: 9,
    },
  ]);
  expect(independentImports).toBe(0);
});
