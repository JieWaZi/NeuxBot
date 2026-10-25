import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type * as RemotionPlayer from "@remotion/player";
import {
  act,
  cleanup,
  configure,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { delay, http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  MemoryRouter,
  Link,
  Outlet,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { useState, type ReactNode } from "react";
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
import { chatWorkspaceHandlers } from "../test/chat-workspace-handlers";
import { installCanvasDOM } from "../test/canvas-dom";
import { client } from "@/shared/api/generated/client.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowEvent as WorkflowEvent } from "@/shared/api/generated/types.gen";
import type { ChatEvent } from "@/shared/api/events";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type * as SceneWorkspace from "@/shared/workspace/app-scene-workspace";
import {
  ChatContributionsProvider,
  ChatQueueProvider,
  WorkbenchProvider,
} from "@/modules/chat";
import { WorkflowResultContent } from "../presentation/node-result";
import { WorkflowDetailPage } from "./workflow-detail-page";
import { WorkflowListPage } from "./workflow-list-page";
import { PartnerNavigationLayout } from "@/app/layout/partner-navigation-layout";
import type { PartnerNavigationPreferences } from "@/app/layout/partner-navigation-preferences";
// 模块转换属于测试准备阶段；发送断言保留真实动画挂载，不计入首次编译耗时。
import "../components/workflow-generation-animation";

configure({ asyncUtilTimeout: 5000 });

const scene = vi.hoisted(() => ({ active: true, width: 660 }));
const chatSubscription = vi.hoisted(() => ({
  onEvent: undefined as ((event: ChatEvent) => void) | undefined,
}));
const workflowSubscriptions = vi.hoisted(
  () => new Map<string, (event?: WorkflowEvent) => void>(),
);
const playerClock = vi.hoisted(() => ({ play: vi.fn(), pause: vi.fn() }));
// 保留真实 Player 和 Composition，只冻结媒体时钟，避免循环 SVG 刷新干扰 Chat 状态断言。
vi.mock("@remotion/player", async (importOriginal) => {
  const module = await importOriginal<typeof RemotionPlayer>();
  const { createElement, forwardRef, useCallback } = await import("react");
  type PlayerRef = RemotionPlayer.PlayerRef;
  type PlayerProps = Parameters<typeof module.Player>[0];
  return {
    ...module,
    Player: forwardRef<PlayerRef, PlayerProps>((props, ref) => {
      const attach = useCallback(
        (instance: PlayerRef | null) => {
          const value = instance
            ? new Proxy(instance, {
                get(target, key, receiver) {
                  if (key === "play") return playerClock.play;
                  if (key === "pause") return playerClock.pause;
                  return Reflect.get(target, key, receiver);
                },
              })
            : null;
          if (typeof ref === "function") ref(value);
          else if (ref) ref.current = value;
        },
        [ref],
      );
      return createElement(module.Player, { ...props, ref: attach });
    }),
  };
});
vi.mock("@/shared/workspace/app-scene-workspace", async (importOriginal) => ({
  ...(await importOriginal<typeof SceneWorkspace>()),
  useRouteSceneActive: () => scene.active,
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));
vi.mock("@/modules/chat/components/chat-terminal-panel", () => ({
  ChatTerminalPanel: () => <div>测试终端内容</div>,
}));
vi.mock("@/shared/api/events", () => ({
  subscribeWorkflow: (
    _id: string,
    options: { runId?: string; onEvent: (event: WorkflowEvent) => void },
  ) => {
    const key = options.runId ?? "definition";
    const callback = (event?: WorkflowEvent) => options.onEvent(event ?? {});
    workflowSubscriptions.set(key, callback);
    return () => {
      if (workflowSubscriptions.get(key) === callback)
        workflowSubscriptions.delete(key);
    };
  },
  subscribeChat: (
    chatId: string,
    options: { onEvent: (event: ChatEvent) => void },
  ) => {
    if (chatId === "creation-chat") chatSubscription.onEvent = options.onEvent;
    return () => {
      if (chatSubscription.onEvent === options.onEvent)
        chatSubscription.onEvent = undefined;
    };
  },
}));

const server = setupServer();
const origin = "http://workflow-scene.test";
const originalScrollTo = HTMLElement.prototype.scrollTo;
let saved = false;

let restoreCanvasDOM = () => {};

beforeAll(() => {
  // jsdom 没有真实可视区域；画布和编辑测试保留头像容器，关闭无关的形象资源加载。
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
  restoreCanvasDOM = installCanvasDOM((element) => {
    const panel = element?.closest("[data-chat-right-panel-host]");
    const layout = panel?.closest<HTMLElement>("[data-chat-layout]");
    return panel && !layout?.dataset.chatFullView
      ? Number.parseFloat(
          layout?.style.getPropertyValue("--chat-right-panel-width") || "320",
        )
      : scene.width;
  });
});
beforeEach(() => {
  localStorage.clear();
  scene.active = true;
  scene.width = 660;
  chatSubscription.onEvent = undefined;
  playerClock.play.mockClear();
  playerClock.pause.mockClear();
  workflowSubscriptions.clear();
  saved = false;
  server.use(
    ...chatWorkspaceHandlers(origin),
    http.get(`${origin}/api/v1/runtime-environments`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${origin}/api/v1/ai-accounts`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${origin}/api/v1/workflows`, () =>
      HttpResponse.json({
        items: [{ id: "flow", title: "审批验收", ordinal: saved ? 1 : 0 }],
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: {
          id: "flow",
          title: "审批验收",
          currentRevisionId: "version-1",
        },
        revision: {
          id: "version-1",
          ordinal: 1,
          manifest: {
            meta: {
              phases: [{ id: "prepare", title: "准备输入" }],
              inputSchema: {
                type: "object",
                properties: {
                  material: {
                    type: "string",
                    title: "材料",
                    description: "粘贴需要整理的会议材料",
                  },
                },
                required: ["material"],
              },
            },
            nodes: [
              {
                id: "prepare-input",
                phaseId: "prepare",
                kind: "functionTask",
                labelTemplate: "生成材料",
              },
            ],
          },
        },
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/revisions`, () =>
      HttpResponse.json({ items: [] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/source`, () =>
      HttpResponse.json({
        revisionId: "version-1",
        source: "export const meta = {};",
        sdkVersion: "ally.workflow/1",
      }),
    ),
  );
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  restoreCanvasDOM();
  server.close();
  HTMLElement.prototype.scrollTo = originalScrollTo;
  vi.unstubAllGlobals();
});

function renderScene(
  path: string,
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  }),
  shellCollapsed?: boolean,
  navigation?: ReactNode,
) {
  const content = () => (
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatContributionsProvider value={{ Content: WorkflowResultContent }}>
          <ChatQueueProvider>
            <TooltipProvider>
              <MemoryRouter initialEntries={[path]}>
                {navigation}
                <Routes>
                  <Route
                    element={
                      shellCollapsed === undefined ? (
                        <Outlet />
                      ) : (
                        <WorkflowShell collapsed={shellCollapsed} />
                      )
                    }
                  >
                    <Route path="/workflows" element={<WorkflowListPage />} />
                    <Route
                      path="/workflows/:workflowId"
                      element={<WorkflowDetailPage />}
                    />
                    <Route
                      path="/workflows/:workflowId/runs/:runId"
                      element={<WorkflowDetailPage />}
                    />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </ChatContributionsProvider>
      </WorkbenchProvider>
    </QueryClientProvider>
  );
  const result = render(content());
  return () => result.rerender(content());
}

/** 使用真实 Shell 导航层，保留此前选中的普通伙伴以复现工作流入口泄漏。 */
function WorkflowShell({ collapsed }: { collapsed: boolean }) {
  const location = useLocation();
  const [preferences, setPreferences] = useState<PartnerNavigationPreferences>({
    collapsedGroups: {},
    contextSidebarWidth: 240,
    currentPartnerId: "ordinary-waker",
    currentRailKey: "waker:ordinary-waker",
    entityMode: "waker",
    globalSidebarCollapsed: collapsed,
    lastTeamThreads: {},
    railExpanded: false,
    railOrder: ["waker:ordinary-waker"],
  });
  return (
    <PartnerNavigationLayout
      channels={[]}
      contextSidebarHidden
      onManageEntities={() => {}}
      onOpenPartnerDetail={() => {}}
      onOpenAutomaticTasks={() => {}}
      onOpenSettings={() => {}}
      onPreferencesChange={setPreferences}
      onSceneNavigationVisibleChange={() => {}}
      onSelectPartner={() => {}}
      onSelectTeam={() => {}}
      preferences={preferences}
      sceneNavigationVisible={false}
      sceneContentFlush
      selectedRailKey="waker:ordinary-waker"
      settingsOpen={false}
      wakers={[{ id: "ordinary-waker", name: "普通伙伴" }]}
    >
      <output data-testid="workflow-route">{location.pathname}</output>
      <Outlet />
    </PartnerNavigationLayout>
  );
}

function readyCreationRuntime() {
  server.use(
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            defaultModel: "native",
            models: [
              {
                id: "native",
                name: "Native",
                permissionLevels: ["default", "auto", "full-access"],
              },
            ],
          },
        ],
      }),
    ),
  );
}

async function clickWorkflowAction(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  const dock = await screen.findByRole("navigation", { name: "工作流" });
  const button = within(dock).queryByRole("button", { name });
  if (button) {
    await user.click(button);
    return;
  }
  const management = name === "编辑" || name === "删除工作流";
  const managementMenu = within(dock).queryByRole("button", {
    name: "编辑与删除",
  });
  await user.click(
    management && managementMenu
      ? managementMenu
      : within(dock).getByRole("button", { name: "工作流操作" }),
  );
  await user.click(await screen.findByRole("menuitem", { name }));
}

it("工作流冷加载保留输入框，消息区与画布分别显示局部 Spinner", async () => {
  let releaseDefinition = () => {};
  let releaseMessages = () => {};
  const messagesRequested = vi.fn();
  const definitionReady = new Promise<void>((resolve) => {
    releaseDefinition = resolve;
  });
  const messagesReady = new Promise<void>((resolve) => {
    releaseMessages = resolve;
  });
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, async () => {
      await definitionReady;
      return HttpResponse.json({
        definition: { id: "flow", title: "审批验收" },
        creationSessions: [{ id: "creation", chatId: "creation-chat" }],
      });
    }),
    http.get(`${origin}/api/v1/chats/creation-chat`, () =>
      HttpResponse.json({ chat: { id: "creation-chat" } }),
    ),
    http.get(`${origin}/api/v1/chats/creation-chat/messages`, async () => {
      messagesRequested();
      await messagesReady;
      return HttpResponse.json({
        resources: [
          {
            id: "request",
            role: "user",
            content: "已有工作流需求",
            revision: 1,
          },
        ],
        latestSequence: 1,
      });
    }),
  );
  renderScene("/workflows/flow");
  fireEvent.click(screen.getByRole("button", { name: "展开对话" }));
  expect(screen.getByRole("textbox", { name: /输入消息/ })).toBeInTheDocument();
  expect(screen.getByLabelText("正在加载对话消息")).toBeInTheDocument();
  expect(
    screen.getByRole("status", { name: "正在读取工作流…" }),
  ).toBeInTheDocument();
  expect(document.querySelector(".animate-pulse")).toBeNull();
  await act(async () => releaseDefinition());
  await waitFor(() => expect(messagesRequested).toHaveBeenCalled());
  expect(screen.getByRole("textbox", { name: /输入消息/ })).toBeInTheDocument();
  expect(screen.getByLabelText("正在加载对话消息")).toBeInTheDocument();
  await act(async () => releaseMessages());
  await screen.findByText("已有工作流需求");
  expect(screen.queryByLabelText("正在加载对话消息")).not.toBeInTheDocument();
});

it.each(["未保存版本", "空 Manifest"])(
  "已有工作流冷加载立即显示双窗格，%s时持续展示流水线",
  async (emptyState) => {
    server.use(
      http.get(`${origin}/api/v1/workflows/flow`, async () => {
        await delay(1000);
        return HttpResponse.json({
          definition: { id: "flow", title: "审批验收" },
          revision:
            emptyState === "空 Manifest"
              ? { id: "version-1", manifest: { meta: {}, nodes: [] } }
              : undefined,
        });
      }),
    );
    renderScene("/workflows/flow?optimizeRun=run-1");
    expect(
      screen.queryByRole("heading", { name: "欢迎使用工作流" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("separator", { name: "调整对话与画布宽度" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "输入消息" }),
    ).toBeInTheDocument();
    const placeholder = await screen.findByRole("img", { name: "工作流轮廓" });
    expect(placeholder).toHaveAttribute("aria-busy", "false");
    expect(placeholder.textContent).toBe("");
    expect(
      placeholder.querySelectorAll("[data-workflow-loading-phase]"),
    ).toHaveLength(3);
    await waitFor(() => expect(playerClock.play).toHaveBeenCalled());
    expect(
      placeholder.querySelector('[data-workflow-generation-playing="true"]'),
    ).toBeInTheDocument();
    expect(screen.queryByText("尚未保存")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "欢迎使用工作流" }),
    ).not.toBeInTheDocument();
  },
);

it("键盘关闭画布节点浮层后将焦点还给原节点", async () => {
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  const node = await screen.findByRole(
    "button",
    { name: "选择 生成材料" },
    { timeout: 5000 },
  );
  node.focus();
  await user.keyboard("{Enter}");
  await screen.findByRole("dialog", { name: "生成材料" });
  expect(
    screen.queryByRole("complementary", { name: "详情" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "查看源码" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "请求修改" }),
  ).not.toBeInTheDocument();
  await user.keyboard("{Escape}");
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(node).toHaveFocus();
});

it("阶段完整输入保留独立草稿，关闭后主聊天草稿与输入实例保持不变", async () => {
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await user.click(await screen.findByRole("button", { name: "展开对话" }));
  const mainInput = screen.getByRole("textbox", { name: /输入消息/ });
  await user.click(mainInput);
  await user.paste("未发送的聊天草稿");
  const title = await screen.findByRole("button", { name: "准备输入" });
  await user.click(title);
  const dialog = await screen.findByRole("dialog", { name: "调整阶段" });
  expect(
    within(dialog).getByRole("textbox", { name: /输入消息/ }),
  ).toHaveFocus();
  expect(
    within(dialog).getByRole("button", { name: "发送消息" }),
  ).toBeDisabled();
  expect(within(dialog).getByRole("textbox", { name: /输入消息/ })).toHaveValue(
    "",
  );
  await user.paste("独立的阶段草稿");
  expect(within(dialog).queryByLabelText("阶段标题")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("complementary", { name: "详情" }),
  ).not.toBeInTheDocument();
  await user.keyboard("{Escape}");
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "调整阶段" }),
    ).not.toBeInTheDocument(),
  );
  expect(title).toHaveFocus();
  expect(screen.getByRole("textbox", { name: /输入消息/ })).toBe(mainInput);
  expect(mainInput).toHaveValue("未发送的聊天草稿");
});

it("离开场景后节点浮层不覆盖其他页面，返回仍保留节点选择", async () => {
  const user = userEvent.setup();
  const refresh = renderScene("/workflows/flow");
  await user.click(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  );
  await screen.findByRole("dialog", { name: "生成材料" });
  scene.active = false;
  refresh();
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  scene.active = true;
  refresh();
  await screen.findByRole("dialog", { name: "生成材料" });
  expect(screen.getByRole("heading", { name: "生成材料" })).toBeInTheDocument();
});

it("消息链接打开右侧工作台后保留主容器内的画布和节点浮层", async () => {
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: {
          id: "flow",
          title: "材料流程",
          currentRevisionId: "version-1",
        },
        creationSessions: [{ id: "session-1", chatId: "creation-chat" }],
        revision: {
          id: "version-1",
          ordinal: 1,
          manifest: {
            meta: { phases: [{ id: "prepare", title: "准备输入" }] },
            nodes: [
              {
                id: "prepare-input",
                phaseId: "prepare",
                kind: "functionTask",
                labelTemplate: "生成材料",
              },
            ],
          },
        },
      }),
    ),
    http.get(`${origin}/api/v1/chats/creation-chat`, () =>
      HttpResponse.json({ chat: { id: "creation-chat" } }),
    ),
    http.get(`${origin}/api/v1/chats/creation-chat/messages`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "answer",
            role: "assistant",
            content: "[示例页面](https://example.com)",
            displayParts: [
              {
                id: "part",
                type: "text",
                text: "[示例页面](https://example.com)",
              },
            ],
            status: "completed",
            revision: 1,
          },
        ],
        latestSequence: 1,
      }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const link = await screen.findByRole("link", { name: "示例页面" });
  await user.click(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  );
  await screen.findByRole("dialog", { name: "生成材料" });
  await user.click(link);
  await screen.findByDisplayValue(/^https:\/\/example\.com\/?$/);
  expect(
    await screen.findByRole("dialog", { name: "生成材料" }),
  ).toBeInTheDocument();
});

it("节点标题保存后保留已打开的模型菜单与编辑浮层", async () => {
  let revision = "version-1";
  let label = "生成议程";
  server.use(
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            models: [
              { id: "model-a", name: "模型 A" },
              { id: "model-b", name: "模型 B" },
            ],
            defaultModel: "model-a",
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/wakers`, () =>
      HttpResponse.json({ resources: [{ id: "ally", name: "NeuxBot" }] }),
    ),
    http.get(`${origin}/api/v1/wakers/ally`, () =>
      HttpResponse.json({ id: "ally", name: "NeuxBot" }),
    ),
    http.get(`${origin}/api/v1/wakers/ally/connectors`, () =>
      HttpResponse.json([]),
    ),
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: {
          id: "flow",
          title: "审批验收",
          currentRevisionId: revision,
        },
        revision: {
          id: revision,
          ordinal: revision === "version-1" ? 1 : 2,
          manifest: {
            meta: { phases: [{ id: "prepare", title: "准备输入" }] },
            nodes: [
              {
                id: "agenda",
                phaseId: "prepare",
                kind: "worker",
                labelTemplate: label,
                promptTemplate: "生成议程",
                options: {
                  resolve: {
                    kind: "waker",
                    wakerId: "ally",
                    runtimeId: "codex",
                    model: "model-a",
                  },
                },
              },
            ],
          },
        },
      }),
    ),
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        const body = (await request.json()) as {
          edit: { fields: { label: string } };
        };
        await delay(100);
        label = body.edit.fields.label;
        revision = "version-2";
        return HttpResponse.json({ revisionId: revision });
      },
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  const canvas = await screen.findByLabelText("画布", {
    selector: "div[data-workflow-motion]",
  });
  const node = await within(canvas).findByRole("button", {
    name: "选择 生成议程",
  });
  await user.click(node);
  const editor = await screen.findByLabelText("生成议程", {
    selector: '[role="dialog"]',
  });
  const title = within(editor).getByRole("textbox", { name: "节点标题" });
  expect(
    within(editor).queryByRole("heading", { name: "连接器" }),
  ).not.toBeInTheDocument();
  await user.clear(title);
  await user.type(title, "周会议程");
  await user.click(
    await within(editor).findByRole("button", {
      name: "模型 A",
    }),
  );
  await screen.findByRole("dialog", { name: "选择模型" });
  await waitFor(() => expect(revision).toBe("version-2"));
  await waitFor(() => expect(editor).toHaveAttribute("aria-label", "周会议程"));
  expect(screen.getByRole("dialog", { name: "选择模型" })).toBeInTheDocument();
  expect(title).toBeInTheDocument();
  expect(title).toHaveValue("周会议程");
});

it("资源页重新激活后读取创作在另一场景保存的版本", async () => {
  const refresh = renderScene("/workflows");
  await screen.findByRole("link", { name: "审批验收" });
  expect(screen.queryByText("版本 1")).not.toBeInTheDocument();
  scene.active = false;
  refresh();
  saved = true;
  scene.active = true;
  refresh();
  await screen.findByText("版本 1");
});

it("已有工作流默认画布铺满主容器，会话展开收起保留共享输入实例", async () => {
  const user = userEvent.setup();
  renderScene("/workflows");
  await user.click(await screen.findByRole("link", { name: "审批验收" }));
  const node = await screen.findByRole("button", { name: "选择 生成材料" });
  expect(node.closest("[data-chat-main-content]")).toHaveAttribute(
    "data-chat-main-content",
    "workflow-canvas",
  );
  expect(node.closest("[data-chat-right-panel-host]")).toBeNull();
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("tab", { name: "工作流" })).not.toBeInTheDocument();
  expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const input = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(input, "保留共享聊天草稿");
  const divider = screen.getByRole("separator", { name: "调整对话与画布宽度" });
  expect(Number(divider.getAttribute("aria-valuenow"))).toBeCloseTo(
    scene.width / 3,
  );
  fireEvent.keyDown(divider, { key: "ArrowRight" });
  expect(Number(divider.getAttribute("aria-valuenow"))).toBeGreaterThan(
    scene.width / 3,
  );
  await user.click(screen.getByRole("button", { name: "收起对话" }));
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  expect(node).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
  expect(input).toHaveTextContent("保留共享聊天草稿");
});

it("点击新建立即打开画布输入框，不等待列表刷新或创建空记录", async () => {
  const create = vi.fn(() => HttpResponse.json({ id: "new-flow" }));
  server.use(http.post(`${origin}/api/v1/workflows`, create));
  const user = userEvent.setup();
  renderScene("/workflows");
  await screen.findByRole("link", { name: "审批验收" });
  await user.click(screen.getByRole("button", { name: "新建工作流" }));
  await screen.findByRole("textbox", { name: "输入消息" });
  expect(
    screen.getByRole("heading", { name: "欢迎使用工作流" }),
  ).toBeInTheDocument();
  const welcome = screen.getByRole("heading", { name: "欢迎使用工作流" });
  expect(welcome.querySelector("[data-typed-phrase]")).toBeInTheDocument();
  expect(
    within(screen.getByTestId("chat-page-layout")).getByRole("button", {
      name: "与 NeuxBot 互动",
    }),
  ).toBeInTheDocument();
  expect(create).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: /帮我创建周会准备流程/ }),
  ).not.toBeInTheDocument();
  expect(screen.getByTestId("chat-page-layout")).toHaveAttribute(
    "data-chat-canvas-input",
    "true",
  );
  expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  expect(
    screen.queryByRole("separator", { name: "调整对话与画布宽度" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("searchbox", { name: "搜索工作流" }),
  ).not.toBeInTheDocument();
});

it.each(["保存版本", "执行结束"])(
  "首次发送立即展开聊天和横向流水线，%s后按实际数据切换画布",
  async (outcome) => {
    readyCreationRuntime();
    let releaseCreate = () => {};
    const creation = new Promise<void>((resolve) => {
      releaseCreate = resolve;
    });
    const create = vi.fn(async () => {
      await creation;
      return HttpResponse.json({ id: "flow" });
    });
    let generated = false;
    let completed = false;
    const content = "生成会议材料工作流";
    server.use(
      http.post(`${origin}/api/v1/workflows`, create),
      http.post(`${origin}/api/v1/workflows/flow/creation/messages`, () =>
        HttpResponse.json({
          id: "creation",
          chatId: "creation-chat",
          acceptedExecutionId: "generation",
          execution: {
            id: "generation",
            userMessageId: "request",
            status: "running",
          },
        }),
      ),
      http.get(`${origin}/api/v1/workflows/flow`, () =>
        HttpResponse.json({
          definition: { id: "flow", title: "会议材料" },
          creationSessions: [{ id: "creation", chatId: "creation-chat" }],
          revision: generated
            ? {
                id: "generated-version",
                manifest: {
                  meta: { phases: [{ id: "prepare", title: "准备会议" }] },
                  nodes: [
                    {
                      id: "materials",
                      phaseId: "prepare",
                      kind: "functionTask",
                      labelTemplate: "生成材料",
                    },
                  ],
                },
              }
            : undefined,
        }),
      ),
      http.get(`${origin}/api/v1/chats/creation-chat`, () =>
        HttpResponse.json({
          chat: {
            id: "creation-chat",
            activeExecutionId: completed ? undefined : "generation",
          },
        }),
      ),
      http.get(`${origin}/api/v1/chats/creation-chat/messages`, () =>
        HttpResponse.json({
          latestSequence: completed ? 3 : 1,
          resources: [
            { id: "request", role: "user", content, revision: 1 },
            {
              id: "generation",
              role: "assistant",
              executionId: "generation",
              content: "正在编排会议步骤",
              displayParts: [
                { id: "text", type: "text", text: "正在编排会议步骤" },
              ],
              status: completed ? "completed" : "running",
              revision: completed ? 2 : 1,
            },
          ],
        }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Infinity },
        mutations: { retry: false },
      },
    });
    const user = userEvent.setup();
    const refresh = renderScene("/workflows/new?draftId=meeting", queryClient);
    const input = await screen.findByRole("textbox", { name: "输入消息" });
    expect(screen.getByTestId("chat-page-layout")).toHaveAttribute(
      "data-chat-canvas-input",
      "true",
    );
    await user.click(input);
    await user.paste(content);
    const send = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(send).toBeEnabled());
    await user.click(send);
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId("chat-page-layout")).not.toHaveAttribute(
      "data-chat-canvas-input",
    );
    expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
    const loading = await screen.findByRole("status", {
      name: "正在生成工作流…",
    });
    expect(
      loading.querySelectorAll("[data-workflow-loading-phase]"),
    ).toHaveLength(3);
    expect(loading.textContent).toBe("");
    expect(loading.querySelector("img")).toBeNull();
    expect(screen.queryByText("欢迎使用工作流")).not.toBeInTheDocument();
    expect(
      screen.getByRole("separator", { name: "调整对话与画布宽度" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
    expect(screen.getByText(content)).toBeInTheDocument();
    await act(async () => releaseCreate());
    await screen.findByText("正在编排会议步骤");
    expect(
      screen.getByRole("status", { name: "正在生成工作流…" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        document.querySelector('[data-workflow-generation-playing="true"]'),
      ).toBeInTheDocument(),
    );
    await waitFor(() => expect(playerClock.play).toHaveBeenCalled());
    playerClock.pause.mockClear();
    scene.active = false;
    refresh();
    await waitFor(() =>
      expect(
        document.querySelector('[data-workflow-generation-playing="false"]'),
      ).toBeInTheDocument(),
    );
    await waitFor(() => expect(playerClock.pause).toHaveBeenCalled());
    playerClock.play.mockClear();
    scene.active = true;
    refresh();
    await waitFor(() =>
      expect(
        document.querySelector('[data-workflow-generation-playing="true"]'),
      ).toBeInTheDocument(),
    );
    await waitFor(() => expect(playerClock.play).toHaveBeenCalled());
    generated = outcome === "保存版本";
    completed = outcome === "执行结束";
    await act(async () => {
      if (completed)
        chatSubscription.onEvent?.({
          sequence: 3,
          type: "execution.completed",
          message: {
            id: "generation",
            role: "assistant",
            executionId: "generation",
            status: "completed",
            revision: 2,
            displayParts: [
              { id: "text", type: "text", text: "正在编排会议步骤" },
            ],
          },
        });
      await queryClient.invalidateQueries();
    });
    if (generated) await screen.findByRole("button", { name: "选择 生成材料" });
    else {
      const placeholder = await screen.findByRole("img", {
        name: "工作流轮廓",
      });
      expect(placeholder).toHaveAttribute("aria-busy", "false");
      expect(
        placeholder.querySelector('[data-workflow-generation-playing="true"]'),
      ).toBeInTheDocument();
    }
    expect(
      screen.queryByRole("status", { name: "正在生成工作流…" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("尚未保存")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
    queryClient.clear();
  },
  30_000,
);

it("首次发送失败时保留流水线轮廓、展开的聊天与重试输入", async () => {
  readyCreationRuntime();
  let releaseSend = () => {};
  const sending = new Promise<void>((resolve) => {
    releaseSend = resolve;
  });
  server.use(
    http.post(`${origin}/api/v1/workflows`, async () => {
      await sending;
      return HttpResponse.json(
        { code: "INTERNAL", message: "发送未接纳" },
        { status: 500 },
      );
    }),
  );
  const user = userEvent.setup();
  renderScene("/workflows/new");
  const input = await screen.findByRole("textbox", { name: "输入消息" });
  expect(
    screen.queryByRole("button", { name: "查看摘要" }),
  ).not.toBeInTheDocument();
  await user.click(input);
  await user.paste("生成会议材料工作流");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await screen.findByRole("status", { name: "正在生成工作流…" });
  await act(async () => releaseSend());
  await waitFor(() =>
    expect(
      screen.queryByRole("status", { name: "正在生成工作流…" }),
    ).not.toBeInTheDocument(),
  );
  const placeholder = await screen.findByRole("img", { name: "工作流轮廓" });
  expect(placeholder).toHaveAttribute("aria-busy", "false");
  await waitFor(() =>
    expect(
      placeholder.querySelector('[data-workflow-generation-playing="true"]'),
    ).toBeInTheDocument(),
  );
  expect(screen.queryByText("尚未保存")).not.toBeInTheDocument();
  expect(screen.queryByText("欢迎使用工作流")).not.toBeInTheDocument();
  expect(screen.getByTestId("chat-page-layout")).not.toHaveAttribute(
    "data-chat-canvas-input",
  );
  expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  await waitFor(() => expect(input).toHaveTextContent("生成会议材料工作流"));
  expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled();
}, 30_000);

it("窄主容器中会话可展开且分隔线支持键盘调整", async () => {
  scene.width = 480;
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await screen.findByRole("button", { name: "选择 生成材料" });
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const divider = screen.getByRole("separator", { name: "调整对话与画布宽度" });
  const before = Number(divider.getAttribute("aria-valuenow"));
  divider.focus();
  await user.keyboard("{ArrowRight}");
  expect(Number(divider.getAttribute("aria-valuenow"))).toBeGreaterThan(before);
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "选择 生成材料" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "收起对话" }));
  expect(
    screen.queryByRole("separator", { name: "调整对话与画布宽度" }),
  ).not.toBeInTheDocument();
});

it("执行画布的文件产出 Card 点击后在当前工作台打开产出 Tab", async () => {
  const reference = {
    workflowId: "a".repeat(32),
    runId: "b".repeat(32),
    instanceId: "c".repeat(32),
    attemptId: "d".repeat(32),
    title: "今日缺陷报告长图",
  };
  const resultRequested = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [{ id: "run-1", status: "succeeded" }] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1`, () =>
      HttpResponse.json({
        run: { id: "run-1", status: "succeeded", version: 1 },
        manifest: {
          meta: {
            phases: [{ id: "report", title: "生成报告" }],
            nodeResultFiles: { write_report: { title: reference.title } },
          },
          nodes: [
            {
              id: "write_report",
              phaseId: "report",
              kind: "worker",
              labelTemplate: "生成长图",
            },
          ],
        },
        nodeCounts: [{ nodeId: "write_report", succeeded: 1, total: 1 }],
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1/node-results`, () =>
      HttpResponse.json({
        total: 1,
        items: [{ reference, status: "succeeded" }],
      }),
    ),
    http.get(
      `${origin}/api/v1/workflows/${reference.workflowId}/runs/${reference.runId}/instances/${reference.instanceId}/results/${reference.attemptId}`,
      () => {
        resultRequested();
        return HttpResponse.json({
          present: true,
          reference,
          fileView: { title: reference.title },
          result: "long-report.png",
          files: [
            {
              id: "image",
              name: "long-report.png",
              field: "",
              mediaType: "image/png",
              available: false,
            },
          ],
        });
      },
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow/runs/run-1");
  const output = await screen.findByRole("button", {
    name: /今日缺陷报告长图.*已生成文件/,
  });
  expect(output).toBeEnabled();
  await user.click(output);
  await waitFor(() => expect(resultRequested).toHaveBeenCalledOnce());
  expect(
    screen.getByRole("tab", { name: reference.title }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: /^long-report.png/ }),
  ).toBeInTheDocument();
});

it("顶部切换工作流与执行过程，Dock 保留操作入口与历史窗格", async () => {
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [{ id: "run-1", status: "succeeded" }] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1`, () =>
      HttpResponse.json({
        run: { id: "run-1", status: "succeeded" },
        manifest: { nodes: [] },
      }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  const parameters = await screen.findByRole("button", { name: "输入参数" });
  expect(
    screen.queryByRole("button", { name: "运行参数" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const input = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(input, "切换时保留未发送内容");
  expect(
    within(parameters).getByText("粘贴需要整理的会议材料"),
  ).toBeInTheDocument();
  await clickWorkflowAction(user, "历史版本");
  expect(
    screen.getByRole("complementary", { name: "历史版本" }).className,
  ).not.toContain("absolute");
  const dock = screen.getByRole("navigation", { name: "工作流" });
  expect(
    within(dock).queryByRole("button", { name: "工作流" }),
  ).not.toBeInTheDocument();
  expect(
    within(dock).queryByRole("button", { name: "执行记录" }),
  ).not.toBeInTheDocument();
  expect(
    within(dock).queryByRole("button", { name: "执行过程" }),
  ).not.toBeInTheDocument();
  const executionTab = screen.getByRole("radio", { name: "执行过程" });
  await waitFor(() => expect(executionTab).toBeEnabled());
  await user.click(executionTab);
  expect(
    screen.queryByRole("navigation", { name: "工作流" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "执行过程" })).toBeChecked();
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
  expect(input).toHaveTextContent("切换时保留未发送内容");
  await user.click(screen.getByRole("button", { name: "收起对话" }));
  fireEvent.resize(window);
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
  expect(
    screen.getByRole("complementary", { name: "执行记录" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("complementary", { name: "历史版本" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("radio", { name: "工作流" }));
  expect(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "工作流" })).toBeChecked();
  expect(
    screen.getByRole("navigation", { name: "工作流" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("complementary", { name: "执行记录" }),
  ).not.toBeInTheDocument();
});

it("输入节点与 Dock 共用运行表单，关闭后保留草稿并在运行后保存输入", async () => {
  const accepted = vi.fn();
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/runs`, async ({ request }) => {
      accepted(await request.json());
      return HttpResponse.json({ id: "run-inputs" });
    }),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-inputs`, () =>
      HttpResponse.json({
        run: {
          id: "run-inputs",
          status: "succeeded",
          args: { material: "新的会议材料" },
        },
        manifest: { meta: { phases: [] }, nodes: [] },
      }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await user.click(await screen.findByRole("button", { name: "输入参数" }));
  const dialog = screen.getByRole("dialog");
  expect(dialog).toHaveAttribute("data-workflow-input-details", "true");
  expect(dialog).not.toHaveClass("fixed");
  expect(document.body).not.toHaveAttribute("data-scroll-locked");
  await user.type(
    within(dialog).getByRole("textbox", { name: "材料" }),
    "新的会议材料",
  );
  expect(
    within(dialog).queryByRole("button", { name: "取消" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "输入参数" })).not.toHaveAttribute(
    "data-state",
    "selected",
  );
  await user.click(within(dialog).getByRole("button", { name: "关闭" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  await clickWorkflowAction(user, "运行");
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(screen.getByRole("dialog")).toHaveAttribute(
    "data-workflow-input-details",
    "true",
  );
  expect(screen.getByRole("textbox", { name: "材料" })).toHaveValue(
    "新的会议材料",
  );
  await user.click(screen.getByRole("button", { name: "输入参数" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("button", { name: "输入参数" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await user.click(screen.getByRole("button", { name: "输入参数" }));
  expect(screen.getByRole("textbox", { name: "材料" })).toHaveValue(
    "新的会议材料",
  );
  await user.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "运行" }),
  );
  await waitFor(() =>
    expect(accepted).toHaveBeenCalledWith(
      expect.objectContaining({
        revisionId: "version-1",
        args: { material: "新的会议材料" },
      }),
    ),
  );
  await waitFor(() =>
    expect(screen.getByRole("radio", { name: "执行过程" })).toBeChecked(),
  );
  cleanup();
  renderScene("/workflows/flow");
  await user.click(await screen.findByRole("button", { name: "输入参数" }));
  expect(screen.getByRole("textbox", { name: "材料" })).toHaveValue(
    "新的会议材料",
  );
});

it("切换执行记录保留画布实例，等待时显示加载轮廓并隔离旧运行操作", async () => {
  let releaseNext = () => {};
  const ready = new Promise<void>((resolve) => {
    releaseNext = resolve;
  });
  const requested = vi.fn();
  const manifest = {
    meta: { phases: [{ id: "prepare", title: "准备输入" }] },
    nodes: [
      {
        id: "prepare-input",
        phaseId: "prepare",
        kind: "functionTask",
        labelTemplate: "生成材料",
      },
    ],
  };
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({
        items: [
          { id: "run-first", status: "running" },
          { id: "run-next", status: "succeeded" },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-first`, () =>
      HttpResponse.json({
        run: {
          id: "run-first",
          status: "running",
          args: { material: "第一次输入" },
        },
        manifest,
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-next`, async () => {
      requested();
      await ready;
      return HttpResponse.json({
        run: {
          id: "run-next",
          status: "succeeded",
          args: { material: "第二次输入" },
        },
        manifest,
      });
    }),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow/runs/run-first");
  const node = await screen.findByRole("button", { name: "选择 生成材料" });
  const inputNode = screen.getByRole("button", { name: "输入参数" });
  expect(within(inputNode).getByText("第一次输入")).toBeInTheDocument();
  const history = screen.getByRole("complementary", { name: "执行记录" });
  const next = await within(history).findByRole("link", {
    name: /第 1 次运行/,
  });
  await user.click(next);
  await waitFor(() => expect(requested).toHaveBeenCalled());
  await screen.findByRole("status", { name: "正在读取工作流…" });
  expect(node).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "停止运行" }),
  ).not.toBeInTheDocument();
  await act(async () => releaseNext());
  expect(await screen.findByRole("button", { name: "选择 生成材料" })).toBe(
    node,
  );
  expect(screen.getByRole("button", { name: "输入参数" })).toBe(inputNode);
  expect(within(inputNode).getByText("第二次输入")).toBeInTheDocument();
  expect(within(inputNode).queryByText("第一次输入")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("status", { name: "正在读取工作流…" }),
  ).not.toBeInTheDocument();
  await user.click(within(history).getByRole("link", { name: /第 2 次运行/ }));
  expect(await screen.findByRole("button", { name: "选择 生成材料" })).toBe(
    node,
  );
  expect(screen.getByRole("button", { name: "停止运行" })).toBeInTheDocument();
  expect(within(inputNode).getByText("第一次输入")).toBeInTheDocument();
});

it("直接打开执行地址仍可从顶部返回工作流画布", async () => {
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [{ id: "run-1", status: "succeeded" }] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1`, () =>
      HttpResponse.json({
        run: { id: "run-1", status: "succeeded" },
        manifest: { nodes: [] },
      }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow/runs/run-1");
  const workflow = await screen.findByRole("radio", { name: "工作流" });
  expect(screen.getByRole("radio", { name: "执行过程" })).toBeChecked();
  expect(
    screen.queryByRole("navigation", { name: "工作流" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  fireEvent.resize(window);
  await user.click(workflow);
  expect(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "工作流" })).toBeChecked();
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "展开对话" })).toBeInTheDocument();
});

it("运行失败事件同步刷新执行记录状态，不继续显示等待处理", async () => {
  let status = "waiting";
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [{ id: "run-1", status }] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1`, () =>
      HttpResponse.json({
        run: { id: "run-1", status },
        manifest: { nodes: [] },
      }),
    ),
  );
  renderScene("/workflows/flow/runs/run-1");
  const history = await screen.findByRole("complementary", {
    name: "执行记录",
  });
  expect(await within(history).findByText("等待处理")).toBeInTheDocument();
  await waitFor(() => expect(workflowSubscriptions.has("run-1")).toBe(true));
  status = "failed";
  act(() => workflowSubscriptions.get("run-1")?.());
  expect(await within(history).findByText("失败")).toBeInTheDocument();
  expect(within(history).queryByText("等待处理")).not.toBeInTheDocument();
});

it.each([false, true])(
  "确认删除当前执行记录后切到下一条或画布（最后一条：%s）",
  async (last) => {
    let deleted = false;
    const remove = vi.fn(() => {
      deleted = true;
      return HttpResponse.json(true);
    });
    server.use(
      http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
        HttpResponse.json({
          items: [
            ...(!deleted
              ? [{ id: "run-1", status: "succeeded", createdAt: 1 }]
              : []),
            ...(!last
              ? [{ id: "run-2", status: "waiting", createdAt: 2 }]
              : []),
          ],
        }),
      ),
      http.get(`${origin}/api/v1/workflows/flow/runs/:runId`, ({ params }) =>
        HttpResponse.json({
          run: {
            id: params.runId,
            status: params.runId === "run-1" ? "succeeded" : "waiting",
          },
          manifest: { nodes: [] },
        }),
      ),
      http.delete(`${origin}/api/v1/workflows/flow/runs/run-1`, remove),
    );
    const user = userEvent.setup();
    renderScene("/workflows/flow/runs/run-1");
    const history = await screen.findByRole("complementary", {
      name: "执行记录",
    });
    await user.click(screen.getByRole("button", { name: "展开对话" }));
    const composer = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(composer, "保留这段草稿");
    expect(
      within(history).getAllByRole("button", { name: /删除第/ }),
    ).toHaveLength(1);
    await user.click(
      await within(history).findByRole("button", {
        name: `删除第 ${last ? 1 : 2} 次执行记录`,
      }),
    );
    expect(remove).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog", { name: "删除执行记录" });
    await user.click(within(dialog).getByRole("button", { name: "删除记录" }));
    await waitFor(() => expect(remove).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    if (last) {
      await waitFor(() =>
        expect(screen.getByRole("radio", { name: "工作流" })).toBeChecked(),
      );
      expect(screen.getByRole("radio", { name: "执行过程" })).toBeDisabled();
    } else {
      const next = await within(history).findByRole("link", {
        name: /第 1 次运行/,
        current: "page",
      });
      expect(next).toHaveAttribute("href", "/workflows/flow/runs/run-2");
      await waitFor(() => expect(next).toHaveAttribute("aria-current", "page"));
      expect(
        within(history).queryByRole("button", { name: /删除第/ }),
      ).not.toBeInTheDocument();
    }
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(composer);
    expect(composer).toHaveValue("保留这段草稿");
  },
);

it("取消删除不提交，删除失败保留执行记录和就地错误", async () => {
  const remove = vi.fn(() =>
    HttpResponse.json({ message: "暂时无法删除" }, { status: 409 }),
  );
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs`, () =>
      HttpResponse.json({ items: [{ id: "run-1", status: "failed" }] }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/runs/run-1`, () =>
      HttpResponse.json({
        run: { id: "run-1", status: "failed" },
        manifest: { nodes: [] },
      }),
    ),
    http.delete(`${origin}/api/v1/workflows/flow/runs/run-1`, remove),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow/runs/run-1");
  const history = await screen.findByRole("complementary", {
    name: "执行记录",
  });
  const entry = await within(history).findByRole("button", {
    name: "删除第 1 次执行记录",
  });
  await user.click(entry);
  await user.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "取消" }),
  );
  expect(remove).not.toHaveBeenCalled();
  await user.click(entry);
  const dialog = screen.getByRole("dialog", { name: "删除执行记录" });
  await user.click(within(dialog).getByRole("button", { name: "删除记录" }));
  expect(await within(dialog).findByRole("alert")).toHaveTextContent(
    "执行记录未删除，请重试。",
  );
  expect(
    within(history).getByRole("link", { name: /第 1 次运行/, hidden: true }),
  ).toHaveAttribute("aria-current", "page");
  expect(remove).toHaveBeenCalledTimes(1);
});

it("没有执行记录时禁用执行过程入口，旧执行列表地址展示工作流画布", async () => {
  const user = userEvent.setup();
  renderScene("/workflows/flow?tab=runs");
  expect(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  ).toBeInTheDocument();
  const executionTab = screen.getByRole("radio", { name: "执行过程" });
  expect(executionTab).toBeDisabled();
  await user.click(executionTab);
  expect(screen.getByRole("radio", { name: "工作流" })).toBeChecked();
  expect(
    screen.getByRole("navigation", { name: "工作流" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("complementary", { name: "执行记录" }),
  ).not.toBeInTheDocument();
});

it("旧脚本地址只展示画布，不请求源码或提供脚本编辑入口", async () => {
  const source = vi.fn(() =>
    HttpResponse.json({ source: "export const meta = {};" }),
  );
  server.use(http.get(`${origin}/api/v1/workflows/flow/source`, source));
  renderScene("/workflows/flow?tab=source");
  expect(
    await screen.findByRole("button", { name: "选择 生成材料" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "工作流" })).toBeChecked();
  expect(screen.queryByRole("radio", { name: "画布" })).not.toBeInTheDocument();
  expect(screen.queryByRole("radio", { name: "脚本" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "保存" }),
  ).not.toBeInTheDocument();
  expect(source).not.toHaveBeenCalled();
});

it.each([660, 400])(
  "从详情页编辑基本信息（宽度 %i）可取消，也可保存输入并关闭弹窗",
  async (width) => {
    const requests: unknown[] = [];
    server.use(
      http.put(
        `${origin}/api/v1/workflows/flow/configuration`,
        async ({ request }) => {
          requests.push(await request.json());
          return HttpResponse.json({ revision: { id: "version-2" } });
        },
      ),
    );
    const user = userEvent.setup();
    scene.width = width;
    renderScene("/workflows/flow");
    await clickWorkflowAction(user, "编辑");
    let dialog = await screen.findByRole("dialog", { name: "编辑基本信息" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    const unsavedTitle = within(dialog).getByRole("textbox", { name: "标题" });
    await user.clear(unsavedTitle);
    await user.type(unsavedTitle, "未保存的标题");
    expect(unsavedTitle).toHaveFocus();
    await user.type(
      within(dialog).getByRole("textbox", { name: "描述" }),
      "未保存的说明",
    );
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(requests).toHaveLength(0);
    expect(document.body.style.pointerEvents).not.toBe("none");

    await clickWorkflowAction(user, "编辑");
    dialog = await screen.findByRole("dialog", { name: "编辑基本信息" });
    expect(within(dialog).getByRole("textbox", { name: "标题" })).toHaveValue(
      "审批验收",
    );
    expect(within(dialog).getByRole("textbox", { name: "描述" })).toHaveValue(
      "",
    );
    await user.click(within(dialog).getByRole("button", { name: "关闭" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    await clickWorkflowAction(user, "编辑");
    dialog = await screen.findByRole("dialog", { name: "编辑基本信息" });
    const title = within(dialog).getByRole("textbox", { name: "标题" });
    await user.clear(title);
    await user.type(title, "新的工作流标题");
    await user.type(
      within(dialog).getByRole("textbox", { name: "描述" }),
      "新的说明",
    );
    await user.click(within(dialog).getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(requests).toEqual([
      {
        requestId: expect.any(String),
        expectedRevisionId: "version-1",
        edit: {
          target: "meta",
          fields: { title: "新的工作流标题", description: "新的说明" },
        },
      },
    ]);
  },
);

it("Dock 中的删除工作流入口先确认，取消不发送删除请求", async () => {
  const remove = vi.fn(() => HttpResponse.json({}));
  server.use(http.delete(`${origin}/api/v1/workflows/flow`, remove));
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await clickWorkflowAction(user, "删除工作流");
  const dialog = await screen.findByRole("dialog", { name: "删除工作流" });
  expect(remove).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(remove).not.toHaveBeenCalled();
});

it.each([false, true])(
  "删除成功后返回列表，不再读取已删除的详情、版本或运行（事件先到=%s）",
  async (eventFirst) => {
    let deleted = false;
    const failedReads: string[] = [];
    server.use(
      http.delete(`${origin}/api/v1/workflows/flow`, async () => {
        deleted = true;
        if (eventFirst) {
          workflowSubscriptions.get("definition")?.({
            type: "workflow.definition.deleted",
            streamId: "flow",
            entityId: "flow",
          });
          await delay(50);
        }
        return HttpResponse.json(true);
      }),
      http.get(`${origin}/api/v1/workflows/flow*`, ({ request }) => {
        if (!deleted) return;
        failedReads.push(new URL(request.url).pathname);
        return HttpResponse.json(
          { key: "WorkflowNotFound", message: "工作流、版本或运行不存在" },
          { status: 404 },
        );
      }),
      http.get(`${origin}/api/v1/workflows`, () =>
        HttpResponse.json({ items: [] }),
      ),
    );
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    renderScene("/workflows/flow", queryClient, false);
    await clickWorkflowAction(user, "删除工作流");
    const dialog = await screen.findByRole("dialog", { name: "删除工作流" });
    await user.click(
      within(dialog).getByRole("button", { name: "删除工作流" }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("workflow-route")).toHaveTextContent(
        /^\/workflows$/,
      ),
    );
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(deleted).toBe(true);
    expect(failedReads).toEqual([]);
  },
);

it("历史版本的恢复入口绑定所查看版本，确认后按当前版本提交恢复", async () => {
  let currentId = "version-2";
  const restored = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, ({ request }) => {
      const revisionId =
        new URL(request.url).searchParams.get("revisionId") || currentId;
      return HttpResponse.json({
        definition: {
          id: "flow",
          title: "审批验收",
          currentRevisionId: currentId,
        },
        revision: {
          id: revisionId,
          ordinal: Number(revisionId.split("-")[1]),
          manifest: { meta: { phases: [] }, nodes: [] },
        },
      });
    }),
    http.get(`${origin}/api/v1/workflows/flow/revisions`, () =>
      HttpResponse.json({
        items: [
          { id: "version-2", ordinal: 2 },
          { id: "version-1", ordinal: 1 },
        ],
      }),
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/restore`,
      async ({ request }) => {
        restored(await request.json());
        currentId = "version-3";
        return HttpResponse.json({ revisionId: currentId });
      },
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow?revision=version-1");
  await clickWorkflowAction(user, "历史版本");
  const history = screen.getByRole("complementary", { name: "历史版本" });
  expect(
    await within(history).findByRole("button", { name: "版本 1" }),
  ).toHaveAttribute("aria-current", "page");
  expect(within(history).getByText("当前版本")).toBeInTheDocument();
  await user.click(
    within(history).getByRole("button", { name: "将版本 1 恢复为新版本" }),
  );
  const dialog = await screen.findByRole("dialog", { name: "恢复为新版本" });
  expect(within(dialog).getByText("版本 1")).toBeInTheDocument();
  expect(restored).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "确认恢复" }));
  await waitFor(() =>
    expect(restored).toHaveBeenCalledWith(
      expect.objectContaining({
        expectedRevisionId: "version-2",
        targetRevisionId: "version-1",
      }),
    ),
  );
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("删除历史版本先确认，删除所查看版本后返回当前版本并刷新列表", async () => {
  let deleted = false;
  const remove = vi.fn(() => {
    deleted = true;
    return HttpResponse.json(true);
  });
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, ({ request }) =>
      HttpResponse.json({
        definition: {
          id: "flow",
          title: "审批验收",
          currentRevisionId: "version-2",
        },
        revision: {
          id:
            new URL(request.url).searchParams.get("revisionId") || "version-2",
          manifest: { meta: { phases: [] }, nodes: [] },
        },
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/revisions`, () =>
      HttpResponse.json({
        items: [
          { id: "version-2", ordinal: 2 },
          ...(!deleted ? [{ id: "version-1", ordinal: 1 }] : []),
        ],
      }),
    ),
    http.delete(`${origin}/api/v1/workflows/flow/revisions/version-1`, remove),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow?revision=version-1");
  await clickWorkflowAction(user, "历史版本");
  const history = screen.getByRole("complementary", { name: "历史版本" });
  await user.click(
    await within(history).findByRole("button", { name: "删除版本 1" }),
  );
  expect(remove).not.toHaveBeenCalled();
  expect(
    within(history).queryByRole("button", { name: "删除版本 2" }),
  ).not.toBeInTheDocument();
  const dialog = screen.getByRole("dialog", { name: "删除历史版本" });
  await user.click(within(dialog).getByRole("button", { name: "删除版本" }));
  await waitFor(() =>
    expect(
      within(history).queryByRole("button", { name: "版本 1" }),
    ).not.toBeInTheDocument(),
  );
  await waitFor(() =>
    expect(
      within(history).getByRole("button", { name: "版本 2" }),
    ).toHaveAttribute("aria-current", "page"),
  );
  expect(remove).toHaveBeenCalledTimes(1);
});

it.each([false, true])(
  "Shell 收起状态 %s 时工作流只展示自身会话，历史加号继续当前工作流",
  async (collapsed) => {
    readyCreationRuntime();
    const creationMessage = vi.fn();
    const titles: Record<string, string> = {
      "chat-a": "当前工作流会话甲",
      "chat-b": "当前工作流会话乙",
      "chat-new": "新工作流会话",
      "ordinary-chat": "普通伙伴会话",
      "other-chat": "其他工作流会话",
    };
    server.use(
      http.get(`${origin}/api/v1/workflows/flow`, () =>
        HttpResponse.json({
          definition: {
            id: "flow",
            title: "审批验收",
            currentRevisionId: "version-1",
          },
          revision: {
            id: "version-1",
            manifest: {
              meta: { phases: [{ id: "prepare", title: "准备输入" }] },
              nodes: [
                {
                  id: "prepare-input",
                  phaseId: "prepare",
                  kind: "functionTask",
                  labelTemplate: "生成材料",
                },
              ],
            },
          },
          creationSessions: [
            { id: "creation-a", chatId: "chat-a" },
            { id: "creation-b", chatId: "chat-b" },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/chats`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "ordinary-chat",
              wakerId: "ordinary-waker",
              title: titles["ordinary-chat"],
              additionalWorkingDirectories: ["/tmp/share"],
            },
            {
              id: "other-chat",
              wakerId: "ordinary-waker",
              title: titles["other-chat"],
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/chats/:chatId`, ({ params }) =>
        HttpResponse.json({
          chat: {
            id: params.chatId,
            title: titles[String(params.chatId)],
            workspaceDirectory: "/tmp/workflow",
            additionalWorkingDirectories: ["/tmp/workflow"],
          },
        }),
      ),
      http.get(`${origin}/api/v1/chats/:chatId/messages`, () =>
        HttpResponse.json({ resources: [], latestSequence: 0 }),
      ),
      http.post(
        `${origin}/api/v1/workflows/flow/creation/messages`,
        async ({ request }) => {
          creationMessage(await request.json());
          return HttpResponse.json({
            id: "creation-new",
            chatId: "chat-new",
            acceptedExecutionId: "accepted-new",
          });
        },
      ),
    );
    const user = userEvent.setup();
    renderScene("/workflows/flow", undefined, collapsed);
    await screen.findByRole("button", { name: "选择 生成材料" });
    expect(
      screen.getAllByRole("button", { name: "查看历史会话" }),
    ).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: "查看摘要" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /打开身份切换器/ }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看历史会话" }));
    const history = screen.getByLabelText("历史会话");
    expect(
      await within(history).findByRole("button", { name: titles["chat-a"] }),
    ).toBeInTheDocument();
    expect(
      within(history).getByRole("button", { name: titles["chat-b"] }),
    ).toBeInTheDocument();
    expect(
      within(history).getByRole("button", { name: "默认" }),
    ).toBeInTheDocument();
    expect(
      within(history).queryByText(titles["ordinary-chat"] ?? ""),
    ).not.toBeInTheDocument();
    expect(
      within(history).queryByText(titles["other-chat"] ?? ""),
    ).not.toBeInTheDocument();
    await user.click(within(history).getByRole("button", { name: "新建会话" }));
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.click(input);
    await user.paste("继续修改当前工作流");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled(),
    );
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() =>
      expect(creationMessage).toHaveBeenCalledWith(
        expect.objectContaining({ content: "继续修改当前工作流" }),
      ),
    );
    expect(creationMessage.mock.calls[0]?.[0]).not.toHaveProperty("sessionId");
    expect(screen.getByTestId("workflow-route")).toHaveTextContent(
      "/workflows/flow",
    );
    expect(
      screen.getByRole("button", { name: "选择 生成材料" }),
    ).toBeInTheDocument();
  },
);

it("工作流收起对话后保留唯一历史入口和输入草稿", async () => {
  const user = userEvent.setup();
  renderScene("/workflows/flow", undefined, true);
  await screen.findByRole("button", { name: "选择 生成材料" });
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const input = screen.getByRole("textbox", { name: "输入消息" });
  await user.click(input);
  await user.paste("浮窗保留内容");
  await user.click(screen.getByRole("button", { name: "收起对话" }));
  expect(
    screen.queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  expect(input).toHaveTextContent("浮窗保留内容");
  expect(screen.getAllByRole("button", { name: "查看历史会话" })).toHaveLength(
    1,
  );
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
  expect(
    screen.queryByRole("button", { name: /打开身份切换器/ }),
  ).not.toBeInTheDocument();
});

it("对话历史复用浮层并显示默认目录，关闭时保留对话输入", async () => {
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: { id: "flow", title: "审批验收" },
        revision: {
          id: "version-1",
          manifest: { meta: { phases: [] }, nodes: [] },
        },
        creationSessions: [
          {
            id: "conversation-1",
            chatId: "chat-1",
            acceptedExecutionId: "accepted-1",
            lastSavedRevisionId: "version-1",
            updatedAt: 1791270100000,
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/revisions`, () =>
      HttpResponse.json({ items: [{ id: "version-1", ordinal: 1 }] }),
    ),
    http.get(`${origin}/api/v1/chats/chat-1`, () =>
      HttpResponse.json({ chat: { id: "chat-1", title: "准备会议材料" } }),
    ),
    http.get(`${origin}/api/v1/chats/chat-1/messages`, () =>
      HttpResponse.json({ resources: [], latestSequence: 0 }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const input = await screen.findByRole("textbox", { name: "输入消息" });
  await user.type(input, "保留未发送内容");
  await user.click(screen.getByRole("button", { name: "查看历史会话" }));
  await screen.findByText("准备会议材料");
  expect(screen.getByRole("button", { name: "默认" })).toBeInTheDocument();
  expect(screen.getByTestId("composer-workspace-control")).toHaveAttribute(
    "aria-label",
    "默认",
  );
  expect(screen.getByTestId("composer-workspace-control").tagName).toBe("SPAN");
  await user.keyboard("{Escape}");
  expect(screen.getByRole("textbox", { name: "输入消息" })).toHaveTextContent(
    "保留未发送内容",
  );
});

it("工作流主容器与工具 Tab 独立，历史切换保留各自输入并显示最新画布，新会话目录只读", async () => {
  readyCreationRuntime();
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: {
          id: "flow",
          title: "审批验收",
          currentRevisionId: "latest",
        },
        revision: {
          id: "latest",
          ordinal: 2,
          manifest: {
            meta: { phases: [{ id: "prepare", title: "最新阶段" }] },
            nodes: [
              {
                id: "latest-node",
                phaseId: "prepare",
                kind: "functionTask",
                labelTemplate: "最新节点",
              },
            ],
          },
        },
        creationSessions: [
          { id: "creation-a", chatId: "chat-a", lastSavedRevisionId: "old" },
          { id: "creation-b", chatId: "chat-b", lastSavedRevisionId: "old" },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId`, ({ params }) =>
      HttpResponse.json({
        chat: {
          id: params.chatId,
          title: params.chatId === "chat-a" ? "会话甲" : "会话乙",
          additionalWorkingDirectories: ["/tmp/workflow"],
          workspaceDirectory: "/tmp/workflow",
        },
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId/messages`, () =>
      HttpResponse.json({ resources: [], latestSequence: 0 }),
    ),
  );
  const user = userEvent.setup();
  renderScene("/workflows/flow");
  await screen.findByRole("button", { name: "选择 最新节点" });
  await user.click(screen.getByRole("button", { name: "展开对话" }));
  const inputA = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(inputA, "甲未发送");
  expect(screen.queryByRole("tab", { name: "工作流" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "打开终端" }));
  expect(
    screen.queryByRole("button", { name: "关闭“工作流”" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "添加工作台标签" }));
  await screen.findByRole("tab", { name: "新标签页" });
  expect(screen.getByRole("button", { name: "终端" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "查看历史会话" }));
  await user.click(await screen.findByRole("button", { name: "会话乙" }));
  const inputB = screen.getByRole("textbox", { name: "输入消息" });
  expect(inputB).not.toBe(inputA);
  expect(inputB).not.toHaveTextContent("甲未发送");
  expect(
    screen.getByRole("button", { name: "选择 最新节点" }),
  ).toBeInTheDocument();
  await user.type(inputB, "乙未发送");
  await user.click(screen.getByRole("button", { name: "查看历史会话" }));
  await user.click(await screen.findByRole("button", { name: "会话甲" }));
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(inputA);
  expect(inputA).toHaveTextContent("甲未发送");
  expect(screen.getByRole("tab", { name: "新标签页" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await user.click(screen.getByRole("button", { name: /关闭.*新标签页/ }));
  expect(
    screen.getByRole("button", { name: "选择 最新节点" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "新建会话" }));
  const directory = screen
    .getAllByTestId("composer-workspace-control")
    .find((item) => !item.closest('[aria-hidden="true"]'));
  expect(directory).toHaveAttribute("aria-label", "默认");
  expect(directory?.tagName).toBe("SPAN");
  expect(
    screen.getByRole("button", { name: "选择 最新节点" }),
  ).toBeInTheDocument();
}, 30_000);

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

it("创作待办通知打开指定历史会话并展开聊天", async () => {
  server.use(
    http.get(`${origin}/api/v1/workflows/flow`, () =>
      HttpResponse.json({
        definition: { id: "flow", title: "创作待办" },
        creationSessions: [
          { id: "latest-session", chatId: "latest-chat" },
          { id: "waiting-session", chatId: "waiting-chat" },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId`, ({ params }) =>
      HttpResponse.json({
        chat: {
          id: params.chatId,
          title: params.chatId === "waiting-chat" ? "等待会话" : "最新会话",
        },
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId/messages`, ({ params }) =>
      HttpResponse.json({
        resources: [
          {
            id: `message-${params.chatId}`,
            role: "assistant",
            content:
              params.chatId === "waiting-chat"
                ? "等待你的确认"
                : "最新会话内容",
            displayParts: [
              {
                id: "text",
                type: "text",
                text:
                  params.chatId === "waiting-chat"
                    ? "等待你的确认"
                    : "最新会话内容",
              },
            ],
            status: "completed",
            revision: 1,
          },
        ],
        latestSequence: 1,
      }),
    ),
  );
  renderScene(
    "/workflows/flow?chat=waiting-chat&request=approval%3Arequest-1",
    undefined,
    undefined,
    <Link to="/workflows/flow?chat=waiting-chat&request=approval%3Arequest-2">
      打开下一条创作待办
    </Link>,
  );
  expect(await screen.findByText("等待你的确认")).toBeVisible();
  expect(screen.queryByText("最新会话内容")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "收起对话" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "收起对话" }));
  await user.click(screen.getByRole("link", { name: "打开下一条创作待办" }));
  expect(screen.getByRole("button", { name: "收起对话" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  await user.click(screen.getByRole("button", { name: "查看历史会话" }));
  const history = screen.getByLabelText("历史会话");
  await user.click(
    await within(history).findByRole("button", { name: "最新会话" }),
  );
  expect(await screen.findByText("最新会话内容")).toBeVisible();
  expect(
    screen.getByText("等待你的确认").closest('[aria-hidden="true"]'),
  ).toHaveClass("invisible");
});
