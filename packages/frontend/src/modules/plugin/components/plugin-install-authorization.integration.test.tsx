import { StrictMode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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
import type {
  GetPluginAuthorizationResponse,
  GetPluginResponse,
  StartPluginAuthorizationResponse,
} from "@/shared/api/generated/types.gen";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { toApiError } from "@/shared/api/errors";
import { PluginInstallDialog } from "./plugin-install-dialog";

const fixture = vi.hoisted(() => ({
  observers: new Set<GlobalEventObserver>(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.observers.add(observer);
    return () => {
      fixture.observers.delete(observer);
    };
  },
}));

const base = "http://plugin-install-auth.test/api/v1";
const server = setupServer();
const requests: { method: string; url: URL; body?: unknown }[] = [];
const clients: QueryClient[] = [];
let session: StartPluginAuthorizationResponse | undefined;
let available = false;
let accountGeneration = 12;
let preferredAction = "open-url";
let errorInterceptor: ReturnType<typeof client.interceptors.error.use>;
const accountContext = {
  expectedConfigurationRevision: 7,
  expectedAccountGeneration: 12,
};

function detail(): GetPluginResponse {
  return {
    plugin: { id: "plugin-1", name: "办公" },
    resolvedVersionId: "version-1",
    versions: [
      {
        id: "version-1",
        package: {
          contractVersion: "neuxbot-plugin/1",
          manifest: { name: "office" },
          integration: {
            authentication: {
              strategy: "plugin-command",
              protocol: "neuxbot-auth/1",
              preferredAction,
            },
          },
        },
      },
    ],
    installations: [],
    account: { available },
  };
}
function account(): GetPluginAuthorizationResponse {
  return {
    versionId: "version-1",
    configurationRevision: 7,
    accountGeneration,
    account: {
      available,
      configured: available,
      checkedAt: available ? Date.now() : 0,
    },
    session: session ? { ...session } : undefined,
    activeUses: [],
    missingScopes: [],
  };
}
function waiting(id = "session-1"): StartPluginAuthorizationResponse {
  return {
    id,
    state: "waiting",
    challengeId: `challenge-${id}`,
    expiresAt: Date.now() + 60_000,
    actions:
      preferredAction === "show-qr"
        ? [{ type: "show-qr", value: "custom://device-challenge" }]
        : [
            { type: "open-url", url: "https://provider.test/device" },
            { type: "copy-code", value: "CODE-1234" },
          ],
  };
}
async function record(request: Request) {
  requests.push({
    method: request.method,
    url: new URL(request.url),
    body: request.method === "GET" ? undefined : await request.json(),
  });
}
const starts = () =>
  requests.filter(
    ({ method, url }) =>
      method === "POST" && url.pathname.endsWith("/authorizations"),
  );
const installs = () =>
  requests.filter(
    ({ method, url }) =>
      method === "POST" && url.pathname.endsWith("/installations"),
  );
const cancellations = () =>
  requests.filter(({ method }) => method === "DELETE");
function failure(message: string) {
  return HttpResponse.json(
    { key: "PluginAuthorizationFailed", message, messageSource: "public" },
    { status: 502 },
  );
}

beforeAll(() => {
  client.setConfig({ baseUrl: "http://plugin-install-auth.test" });
  errorInterceptor = client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  session = undefined;
  available = false;
  accountGeneration = 12;
  preferredAction = "open-url";
  server.use(
    http.get(`${base}/wakers/waker-1`, () =>
      HttpResponse.json({ id: "waker-1", name: "伙伴一" }),
    ),
    http.post(`${base}/plugins/catalog/office/prepare`, async ({ request }) => {
      await record(request);
      return HttpResponse.json(detail());
    }),
    http.get(`${base}/plugins/plugin-1`, async ({ request }) => {
      await record(request);
      return HttpResponse.json(detail());
    }),
    http.get(`${base}/plugins/plugin-1/configuration`, async ({ request }) => {
      await record(request);
      return HttpResponse.json({
        versionId: "version-1",
        fields: [],
        values: {},
        configuredSecrets: [],
        ready: available,
        configurationRevision: 7,
        accountGeneration,
      });
    }),
    http.get(`${base}/plugins/plugin-1/account`, async ({ request }) => {
      await record(request);
      return HttpResponse.json(account());
    }),
    http.get(
      `${base}/plugins/plugin-1/account/authorizations/:sessionId`,
      async ({ request }) => {
        await record(request);
        return HttpResponse.json(session);
      },
    ),
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        session = waiting();
        return HttpResponse.json(session);
      },
    ),
    http.delete(
      `${base}/plugins/plugin-1/account/authorizations/:sessionId`,
      async ({ request }) => {
        await record(request);
        session = { ...session, state: "cancelled", actions: [] };
        return HttpResponse.json({});
      },
    ),
    http.post(`${base}/plugins/plugin-1/installations`, async ({ request }) => {
      await record(request);
      return HttpResponse.json({
        results: [{ wakerId: "waker-1", installed: true }],
      });
    }),
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
  requests.length = 0;
  fixture.observers.clear();
  server.resetHandlers();
});
afterAll(() => {
  client.interceptors.error.eject(errorInterceptor);
  server.close();
});

function show() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(queryClient);
  const close = vi.fn();
  const view = render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <PluginInstallDialog
            catalog={{
              key: "office",
              package: { manifest: { name: "office" } },
            }}
            wakerId="waker-1"
            onClose={close}
          />
        </MemoryRouter>
      </QueryClientProvider>
    </StrictMode>,
  );
  return { ...view, queryClient, close };
}
async function begin() {
  fireEvent.click(await screen.findByRole("button", { name: "继续" }));
  await waitFor(() => expect(starts()).toHaveLength(1));
}
function resync() {
  for (const observer of fixture.observers) observer.onResync?.("test");
}

it.each(["show-qr", "open-url"])(
  "starts first-time %s authorization from an unchecked account and installs only after completion",
  async (action) => {
    preferredAction = action;
    const { close } = show();
    await begin();
    if (action === "show-qr")
      expect(
        await screen.findByRole("button", { name: "刷新二维码" }),
      ).toBeVisible();
    else expect(await screen.findByText("CODE-1234")).toBeVisible();
    expect(starts()).toHaveLength(1);
    expect(
      requests.filter(
        ({ method, url }) =>
          method === "GET" && url.pathname.includes("/account/authorizations/"),
      ),
    ).toHaveLength(0);
    expect(installs()).toHaveLength(0);
    expect(cancellations()).toHaveLength(0);
    expect(starts()[0]?.body).toEqual({
      ...accountContext,
      additionalScopes: [],
    });
    expect(starts()[0]?.url.searchParams.get("versionId")).toBe("version-1");
    expect(starts()[0]?.url.searchParams.get("wakerId")).toBe("waker-1");
    expect(requests.some(({ url }) => url.pathname.endsWith("/check"))).toBe(
      false,
    );
    available = true;
    accountGeneration = 13;
    session = { ...session, state: "ready", actions: [], expiresAt: 0 };
    await act(async () => resync());
    await waitFor(() => expect(installs()).toHaveLength(1));
    expect(installs()[0]?.body).toEqual({
      ...accountContext,
      expectedAccountGeneration: 13,
      versionId: "version-1",
      wakerIds: ["waker-1"],
    });
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    cleanup();
    expect(cancellations()).toHaveLength(0);
  },
);

it("continues the initial authorization when a plugin event replaces its pending account read", async () => {
  preferredAction = "show-qr";
  let finish: (() => void) | undefined;
  let reads = 0;
  server.use(
    http.get(`${base}/plugins/plugin-1/account`, async ({ request }) => {
      await record(request);
      if (++reads === 1)
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
      return HttpResponse.json(account());
    }),
  );
  show();
  fireEvent.click(await screen.findByRole("button", { name: "继续" }));
  await waitFor(() => expect(finish).toBeDefined());
  await act(async () => resync());
  await waitFor(() => expect(reads).toBeGreaterThan(1));
  await act(async () => finish?.());
  await waitFor(() => expect(starts()).toHaveLength(1));
  expect(
    await screen.findByRole("button", { name: "刷新二维码" }),
  ).toBeVisible();
  expect(installs()).toHaveLength(0);
});

it("clears the first start failure after a successful retry", async () => {
  let attempt = 0;
  server.use(
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        if (++attempt === 1) return failure("初次启动失败");
        session = waiting();
        return HttpResponse.json(session);
      },
    ),
  );
  show();
  await begin();
  expect(await screen.findByRole("alert")).toHaveTextContent("初次启动失败");
  fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
  expect(await screen.findByText("CODE-1234")).toBeVisible();
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(starts()).toHaveLength(2);
  expect(installs()).toHaveLength(0);
});

it("presents the initial dependency error without blaming the account or proceeding to installation", async () => {
  server.use(
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        return HttpResponse.json(
          { key: "PluginDependencyUnavailable" },
          { status: 502 },
        );
      },
    ),
  );
  show();
  await begin();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "插件所需程序暂时无法准备，请检查网络后重试",
  );
  expect(
    screen.queryByText("插件认证未完成或外部授权失效"),
  ).not.toBeInTheDocument();
  expect(installs()).toHaveLength(0);
  expect(screen.getByRole("button", { name: "连接账号" })).toBeEnabled();
});

it.each([
  ["PluginDependencyUnavailable", "插件所需程序暂时无法准备，请检查网络后重试"],
  ["50200006503", "插件所需程序暂时无法准备，请检查网络后重试"],
  ["unknown-provider-failure", "认证未完成，可重新发起。"],
])(
  "presents the stable failed-session code %s without reporting authorization or installation success",
  async (errorCode, message) => {
    show();
    await begin();
    expect(await screen.findByText("CODE-1234")).toBeVisible();
    session = {
      ...session,
      state: "failed",
      errorCode,
      actions: [],
      expiresAt: 0,
    };
    await act(async () => resync());
    expect(await screen.findByText(message)).toBeVisible();
    expect(screen.queryByText(errorCode)).not.toBeInTheDocument();
    expect(screen.queryByText("CODE-1234")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新授权" })).toBeEnabled();
    expect(installs()).toHaveLength(0);
  },
);

it("replaces the first error with a failed retry context read", async () => {
  server.use(
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        return failure("初次启动失败");
      },
    ),
  );
  show();
  await begin();
  expect(await screen.findByRole("alert")).toHaveTextContent("初次启动失败");
  server.use(
    http.get(`${base}/plugins/plugin-1/account`, async ({ request }) => {
      await record(request);
      return failure("最新账号读取失败");
    }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
  await waitFor(() =>
    expect(screen.queryByText("初次启动失败")).not.toBeInTheDocument(),
  );
  expect(
    screen
      .getAllByRole("alert")
      .some((alert) => alert.textContent?.includes("最新账号读取失败")),
  ).toBe(true);
  expect(starts()).toHaveLength(1);
});

it("keeps a retry pending while reading its account context and starts only one replacement", async () => {
  server.use(
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        return failure("初次启动失败");
      },
    ),
  );
  show();
  await begin();
  expect(await screen.findByRole("alert")).toHaveTextContent("初次启动失败");
  let finish: (() => void) | undefined;
  let held = false;
  server.use(
    http.get(`${base}/plugins/plugin-1/account`, async ({ request }) => {
      await record(request);
      if (!held) {
        held = true;
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
      }
      return HttpResponse.json(account());
    }),
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        session = waiting();
        return HttpResponse.json(session);
      },
    ),
  );
  const retry = await screen.findByRole("button", { name: "连接账号" });
  fireEvent.click(retry);
  fireEvent.click(retry);
  await waitFor(() => expect(finish).toBeDefined());
  const pendingDuringRead = !screen.queryByRole("button", { name: "连接账号" });
  await act(async () => finish?.());
  expect(await screen.findByText("CODE-1234")).toBeVisible();
  expect(pendingDuringRead).toBe(true);
  expect(starts()).toHaveLength(2);
});

it("compensates a late start after closing with its original version and account context", async () => {
  let finish: (() => void) | undefined;
  server.use(
    http.post(
      `${base}/plugins/plugin-1/account/authorizations`,
      async ({ request }) => {
        await record(request);
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
        session = waiting("late-session");
        return HttpResponse.json(session);
      },
    ),
  );
  const view = show();
  await begin();
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  view.unmount();
  expect(cancellations()).toHaveLength(0);
  await act(async () => finish?.());
  await waitFor(() => expect(cancellations()).toHaveLength(1));
  expect(cancellations()[0]?.url.pathname).toBe(
    "/api/v1/plugins/plugin-1/account/authorizations/late-session",
  );
  expect(cancellations()[0]?.url.searchParams.get("versionId")).toBe(
    "version-1",
  );
  expect(cancellations()[0]?.body).toEqual(accountContext);
  available = true;
  await act(async () => resync());
  expect(installs()).toHaveLength(0);
});
