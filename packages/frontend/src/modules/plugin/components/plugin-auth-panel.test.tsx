import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginAuthPanel } from "./plugin-auth-panel";
const fixture = vi.hoisted(() => ({
  status: {
    provider: "feishu",
    account: {
      configured: true,
      available: true,
      appId: "cli_app",
      name: "用户",
    },
    session: {
      id: "session",
      state: "waiting",
      userCode: "",
      challengeId: "challenge",
      notice: undefined as { messages: Record<string, string> } | undefined,
      actions: [] as { type: string; url?: string; value?: string }[],
      verificationUrl: "https://example.com/device?code=a%2Fb&x=1+2",
      expiresAt: Date.now() + 60_000,
    },
    activeUses: [] as {
      chatId: string;
      executionId: string;
      wakerId: string;
      title: string;
    }[],
    missingScopes: [] as string[],
  },
  login: vi.fn<(input: unknown) => Promise<unknown>>().mockResolvedValue({}),
  configure: vi
    .fn<(input: unknown) => Promise<unknown>>()
    .mockResolvedValue({}),
  cancel: vi.fn().mockResolvedValue({}),
  open: vi.fn().mockResolvedValue(undefined),
  submit: vi.fn(),
  statusError: null as Error | null,
  statusWait: null as Promise<void> | null,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("@/shared/platform", () => ({ openExternalURL: fixture.open }));
vi.mock("qrcode.react", () => ({
  QRCodeSVG: ({ value }: { value: string }) => (
    <output aria-label="二维码内容">{value}</output>
  ),
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginAuthorizationOptions: () => ({
    queryKey: [{ _id: "getPluginAuthorization" }],
    queryFn: async () => {
      if (fixture.statusWait) await fixture.statusWait;
      if (fixture.statusError) throw fixture.statusError;
      return structuredClone(fixture.status);
    },
  }),
  startPluginAuthorizationMutation: () => ({ mutationFn: fixture.login }),
  configurePluginAuthorizationMutation: () => ({
    mutationFn: fixture.configure,
  }),
  cancelPluginAuthorizationMutation: () => ({ mutationFn: fixture.cancel }),
  logoutPluginAuthorizationMutation: () => ({ mutationFn: vi.fn() }),
  invalidatePluginQueries: async (client: QueryClient) =>
    client.invalidateQueries(),
}));
afterEach(cleanup);
beforeEach(() => {
  fixture.login.mockClear();
  fixture.submit.mockClear();
  fixture.statusError = null;
  fixture.statusWait = null;
  fixture.configure.mockClear();
  fixture.open.mockClear();
  fixture.status.activeUses = [];
  fixture.status.missingScopes = [];
  fixture.status.provider = "feishu";
  fixture.status.account.available = true;
  fixture.status.account.configured = true;
  fixture.status.session.userCode = "";
  fixture.cancel.mockClear();
  fixture.status.session.state = "waiting";
  fixture.status.session.id = "session";
  fixture.status.session.expiresAt = Date.now() + 60_000;
  fixture.status.session.verificationUrl =
    "https://example.com/device?code=a%2Fb&x=1+2";
  fixture.status.session.challengeId = "challenge";
  fixture.status.session.notice = undefined;
  fixture.status.session.actions = [
    { type: "show-qr", value: fixture.status.session.verificationUrl },
    { type: "open-url", url: fixture.status.session.verificationUrl },
  ];
});
function show(
  installation = false,
  onReady = vi.fn(),
  preferredAction: "show-qr" | "open-url" | "" = "",
  onSessionStarted = vi.fn(),
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            fixture.submit();
          }}
        >
          <PluginAuthPanel
            pluginId="p"
            preferredAction={preferredAction}
            installation={installation}
            onReady={onReady}
            onSessionStarted={onSessionStarted}
          />
        </form>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
describe("shared plugin authorization", () => {
  it.each(["expired", "failed", "cancelled", "idle"])(
    "retries %s installation authorization without closing the dialog",
    async (state) => {
      fixture.status.account.available = false;
      fixture.status.session.state = state;
      fixture.status.session.actions = [];
      fixture.login.mockImplementationOnce(async () => {
        fixture.status.session.state = "waiting";
        fixture.status.session.expiresAt = Date.now() + 60_000;
        fixture.status.session.actions = [
          { type: "open-url", url: "https://example.com/retry-device" },
        ];
        return {};
      });
      show(true, vi.fn(), "open-url");
      fireEvent.click(await screen.findByRole("button", { name: "重试" }));
      expect(
        await screen.findByRole("button", { name: "打开授权页面" }),
      ).toBeVisible();
      expect(fixture.submit).not.toHaveBeenCalled();
    },
  );

  it("shows a plugin-declared setup notice only for its current waiting challenge", async () => {
    fixture.status.provider = "declared-provider";
    fixture.status.account.available = false;
    fixture.status.session.notice = {
      messages: {
        en: "After setup, scan again to authorize your account and permissions.",
        "zh-CN": "首次扫码完成应用设置后，还需再次扫码完成账号与权限授权。",
      },
    };
    const client = show(true);
    const notice = await screen.findByText(
      "首次扫码完成应用设置后，还需再次扫码完成账号与权限授权。",
    );
    expect(notice).toHaveClass("bg-status-warning-surface");
    client.setQueryData([{ _id: "getPluginAuthorization" }], {
      ...fixture.status,
      session: {
        ...fixture.status.session,
        challengeId: "account-permissions",
        notice: undefined,
      },
    });
    await waitFor(() => expect(notice).not.toBeInTheDocument());
    expect(screen.getByLabelText("二维码内容")).toBeVisible();
  });

  it("falls back to a declared English notice and hides it after the session ends", async () => {
    fixture.status.session.notice = {
      messages: { en: "Finish setup before the second authorization step." },
    };
    const client = show(true);
    const notice = await screen.findByText(
      "Finish setup before the second authorization step.",
    );
    client.setQueryData([{ _id: "getPluginAuthorization" }], {
      ...fixture.status,
      session: { ...fixture.status.session, state: "cancelled" },
    });
    await waitFor(() => expect(notice).not.toBeInTheDocument());
  });

  it("retries account loading without submitting the surrounding settings form", async () => {
    fixture.statusError = new Error("offline");
    fixture.status.session.state = "ready";
    show();
    const retry = await screen.findByRole("button", { name: "重试" });
    fixture.statusError = null;
    fireEvent.click(retry);
    expect(await screen.findByText("已连接：用户")).toBeVisible();
    expect(fixture.submit).not.toHaveBeenCalled();
  });
  it("notifies completion only after an authoritative ready snapshot", async () => {
    const onReady = vi.fn();
    const client = show(true, onReady);
    await screen.findByLabelText("二维码内容");
    expect(onReady).not.toHaveBeenCalled();
    const completed = {
      ...fixture.status,
      session: {
        ...fixture.status.session,
        state: "ready",
        verificationUrl: "",
      },
    };
    client.setQueryData([{ _id: "getPluginAuthorization" }], completed);
    await waitFor(() => expect(onReady).toHaveBeenCalledOnce());
    client.setQueryData([{ _id: "getPluginAuthorization" }], completed);
    expect(onReady).toHaveBeenCalledOnce();
  });
  it("removes an expired QR without a server push, then waits for cancellation before refreshing", async () => {
    fixture.status.account.available = false;
    fixture.status.session.expiresAt = Date.now() + 250;
    let finishCancel: (() => void) | undefined;
    fixture.cancel.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finishCancel = resolve;
        }),
    );
    fixture.login.mockImplementationOnce(async () => {
      fixture.status.session = {
        ...fixture.status.session,
        id: "new-session",
        verificationUrl: "https://example.com/new-device",
        actions: [{ type: "show-qr", value: "https://example.com/new-device" }],
        expiresAt: Date.now() + 60_000,
      };
      return {};
    });
    show(true);
    await screen.findByLabelText("二维码内容");
    await waitFor(() =>
      expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("status")).toHaveTextContent("授权已过期");
    fireEvent.click(screen.getByRole("button", { name: "刷新二维码" }));
    await waitFor(() => expect(fixture.cancel).toHaveBeenCalledOnce());
    expect(fixture.login).not.toHaveBeenCalled();
    finishCancel?.();
    await waitFor(() => expect(fixture.login).toHaveBeenCalledOnce());
    expect(await screen.findByLabelText("二维码内容")).toHaveTextContent(
      "https://example.com/new-device",
    );
  });
  it("preserves the original URL in QR and external link", async () => {
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "打开授权页面" }),
    );
    expect(fixture.open).toHaveBeenCalledWith(
      fixture.status.session.verificationUrl,
    );
    expect(await screen.findByLabelText("二维码内容")).toHaveTextContent(
      fixture.status.session.verificationUrl,
    );
  });
  it("requests server-reported missing permissions without a manual scope field", async () => {
    fixture.status.session.state = "idle";
    fixture.status.account.available = false;
    fixture.status.missingScopes = ["docx:document", "bitable:app"];
    show();
    fireEvent.click(await screen.findByRole("button", { name: "重新授权" }));
    await waitFor(() =>
      expect(fixture.login.mock.calls[0]?.[0]).toMatchObject({
        body: { additionalScopes: ["docx:document", "bitable:app"] },
      }),
    );
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(fixture.configure).not.toHaveBeenCalled();
    expect(fixture.submit).not.toHaveBeenCalled();
  });
  it("shows a connected account without asking users to connect or enter permissions again", async () => {
    fixture.status.provider = "github";
    fixture.status.session.state = "ready";
    show();
    expect(await screen.findByText("已连接：用户")).toBeVisible();
    expect(screen.getByRole("heading", { name: "账号连接" })).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "连接账号" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "重新授权" }),
    ).not.toBeInTheDocument();
  });
  it("shows account leases as work links and blocks identity changes", async () => {
    fixture.status.session.state = "idle";
    fixture.status.activeUses = [
      {
        chatId: "chat-1",
        executionId: "exec-1",
        wakerId: "partner-1",
        title: "读取会议文档",
      },
    ];
    show();
    expect(
      await screen.findByRole("link", { name: "读取会议文档" }),
    ).toHaveAttribute("href", "/chats/chat-1");
    expect(
      screen.queryByRole("button", { name: "更换飞书应用" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "重新授权" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "断开连接" })).toBeDisabled();
  });
  it("starts first-time Feishu setup through QR authorization without app credentials", async () => {
    fixture.status.account.configured = false;
    fixture.status.account.available = false;
    fixture.status.session.state = "idle";
    show();
    fireEvent.click(await screen.findByRole("button", { name: "连接账号" }));
    await waitFor(() => expect(fixture.login).toHaveBeenCalledTimes(1));
    expect(screen.queryByLabelText("飞书应用 App ID")).not.toBeInTheDocument();
    expect(
      screen.queryByLabelText("飞书应用 App Secret"),
    ).not.toBeInTheDocument();
    expect(fixture.configure).not.toHaveBeenCalled();
  });
});

it("keeps GitHub code and inline QR while waiting, then removes both after ready", async () => {
  fixture.status.provider = "github";
  fixture.status.account.available = false;
  fixture.status.session.userCode = "ABCD-EFGH";
  fixture.status.session.actions.push({
    type: "copy-code",
    value: "ABCD-EFGH",
  });
  const client = show();
  expect(await screen.findByText("ABCD-EFGH")).toBeVisible();
  expect(await screen.findByLabelText("二维码内容")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "显示二维码" }),
  ).not.toBeInTheDocument();
  expect(fixture.cancel).not.toHaveBeenCalled();
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    ...fixture.status,
    account: { ...fixture.status.account, available: true },
    session: {
      ...fixture.status.session,
      state: "ready",
      actions: [],
      expiresAt: 0,
      verificationUrl: "",
      userCode: "",
    },
  });
  await waitFor(() =>
    expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument(),
  );
  expect(screen.queryByText("ABCD-EFGH")).not.toBeInTheDocument();
});

it("copies the GitHub device code and reports clipboard failures locally", async () => {
  const user = userEvent.setup();
  fixture.status.provider = "github";
  fixture.status.session.userCode = "ABCD-EFGH";
  fixture.status.session.actions.push({
    type: "copy-code",
    value: "ABCD-EFGH",
  });
  show();
  await user.click(await screen.findByRole("button", { name: "复制验证码" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "已复制" })).toBeInTheDocument(),
  );
  expect(await navigator.clipboard.readText()).toBe("ABCD-EFGH");
  const write = vi
    .spyOn(navigator.clipboard, "writeText")
    .mockRejectedValue(new Error("denied"));
  await user.click(screen.getByRole("button", { name: "已复制" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "复制未完成，请手动输入验证码。",
  );
  write.mockRestore();
});

it("uses declared actions for an unknown plugin and never turns a browser URL into a QR", async () => {
  fixture.status.provider = "new-provider";
  fixture.status.account.available = false;
  fixture.status.session.actions = [
    {
      type: "open-url",
      url: "https://example.com/oauth?redirect_uri=http://localhost/callback",
    },
  ];
  show(true);
  fireEvent.click(await screen.findByRole("button", { name: "打开授权页面" }));
  expect(fixture.open).toHaveBeenCalledWith(
    "https://example.com/oauth?redirect_uri=http://localhost/callback",
  );
  expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "显示二维码" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/飞书/)).not.toBeInTheDocument();
});

it("replaces a challenge and removes all actions after cancellation", async () => {
  fixture.status.provider = "new-provider";
  fixture.status.account.available = false;
  fixture.status.session.actions = [
    { type: "show-qr", value: "custom://first" },
    { type: "copy-code", value: "ONE-TIME" },
  ];
  const client = show(true);
  expect(await screen.findByLabelText("二维码内容")).toHaveTextContent(
    "custom://first",
  );
  const changed = {
    ...fixture.status,
    session: {
      ...fixture.status.session,
      challengeId: "second",
      actions: [{ type: "show-qr", value: "custom://second" }],
    },
  };
  client.setQueryData([{ _id: "getPluginAuthorization" }], changed);
  await waitFor(() =>
    expect(screen.getByLabelText("二维码内容")).toHaveTextContent(
      "custom://second",
    ),
  );
  expect(screen.queryByText("ONE-TIME")).not.toBeInTheDocument();
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    ...changed,
    session: { ...changed.session, state: "cancelled" },
  });
  await waitFor(() =>
    expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument(),
  );
});

it("shows a QR loading area before a declared QR challenge without rendering zero or missing-scope warnings", async () => {
  fixture.status.account.available = false;
  fixture.status.session.actions = [];
  fixture.status.session.expiresAt = 0;
  fixture.status.missingScopes = ["offline_access"];
  show(true, vi.fn(), "show-qr");
  expect(
    await screen.findByRole("status", { name: "正在加载二维码…" }),
  ).toBeVisible();
  expect(screen.queryByText("0", { exact: true })).not.toBeInTheDocument();
  expect(
    screen.queryByText(
      "当前账号缺少插件所需权限，请重新授权。所需权限会自动申请。",
    ),
  ).not.toBeInTheDocument();
});

it("prefers the inline QR with a refresh icon and does not offer duplicate QR or restart controls", async () => {
  fixture.status.account.available = false;
  show(true);
  expect(await screen.findByLabelText("二维码内容")).toBeVisible();
  expect(screen.getByRole("button", { name: "刷新二维码" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "显示二维码" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重新授权" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重新检查认证" }),
  ).not.toBeInTheDocument();
});

it("replaces the setup QR automatically with the user authorization QR before reporting ready", async () => {
  fixture.status.account.available = false;
  const onReady = vi.fn();
  const client = show(true, onReady, "show-qr");
  await screen.findByLabelText("二维码内容");
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    ...fixture.status,
    account: { ...fixture.status.account, configured: true, available: false },
    session: {
      ...fixture.status.session,
      challengeId: "user-login",
      expiresAt: Date.now() + 60000,
      actions: [{ type: "show-qr", value: "https://example.com/user-auth" }],
    },
  });
  await waitFor(() =>
    expect(screen.getByLabelText("二维码内容")).toHaveTextContent(
      "https://example.com/user-auth",
    ),
  );
  expect(onReady).not.toHaveBeenCalled();
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    ...fixture.status,
    account: { ...fixture.status.account, available: true },
    session: {
      ...fixture.status.session,
      state: "ready",
      actions: [],
      expiresAt: 0,
    },
  });
  await waitFor(() => expect(onReady).toHaveBeenCalledOnce());
  expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument();
});

it("shows the declared QR loading slot even before the first status response", async () => {
  let loaded: (() => void) | undefined;
  fixture.statusWait = new Promise<void>((resolve) => {
    loaded = resolve;
  });
  show(true, vi.fn(), "show-qr");
  const indicator = await screen.findByRole("status", {
    name: "正在加载二维码…",
  });
  expect(indicator).toBeVisible();
  loaded?.();
  expect(await screen.findByLabelText("二维码内容")).toBeVisible();
});

it("keeps an authorization loading slot until declared device actions arrive", async () => {
  fixture.status.account.available = false;
  fixture.status.session.actions = [];
  const client = show(true, vi.fn(), "open-url");
  expect(
    await screen.findByRole("status", { name: "正在准备授权信息…" }),
  ).toHaveAttribute("aria-busy", "true");
  expect(
    screen.queryByRole("button", { name: "重新授权" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "取消本次认证" }),
  ).not.toBeInTheDocument();
  client.setQueryData([{ _id: "getPluginAuthorization" }], {
    ...fixture.status,
    session: {
      ...fixture.status.session,
      actions: [
        { type: "open-url", url: "https://example.com/device" },
        { type: "copy-code", value: "CODE-1234" },
      ],
    },
  });
  expect(await screen.findByText("CODE-1234")).toBeVisible();
  expect(screen.getByRole("button", { name: "打开授权页面" })).toBeVisible();
  expect(
    screen.queryByRole("status", { name: "正在准备授权信息…" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重新授权" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "取消本次认证" }),
  ).not.toBeInTheDocument();
});

it("shows authorization preparation before the first browser-action status returns", async () => {
  let finish: (() => void) | undefined;
  fixture.statusWait = new Promise<void>((resolve) => {
    finish = resolve;
  });
  show(true, vi.fn(), "open-url");
  expect(
    await screen.findByRole("status", { name: "正在准备授权信息…" }),
  ).toBeVisible();
  finish?.();
  expect(
    await screen.findByRole("button", { name: "打开授权页面" }),
  ).toBeVisible();
});

it("keeps reauthorization in settings while installation only presents device actions", async () => {
  fixture.status.account.available = false;
  fixture.status.session.actions = [
    { type: "open-url", url: "https://example.com/device" },
  ];
  show();
  expect(await screen.findByRole("button", { name: "重新授权" })).toBeVisible();
  expect(screen.getByRole("button", { name: "取消本次认证" })).toBeVisible();
});

it("places centered challenges before the full-width authorization action regardless of declaration order", async () => {
  fixture.status.account.available = false;
  fixture.status.session.actions = [
    { type: "open-url", url: "https://example.com/device" },
    { type: "copy-code", value: "CODE-1234" },
    { type: "show-qr", value: "https://example.com/qr" },
  ];
  show(true);
  const open = await screen.findByRole("button", { name: "打开授权页面" });
  const code = screen.getByText("CODE-1234");
  const qr = screen.getByLabelText("二维码内容");
  expect(code).toHaveClass("break-all", "max-w-full");
  expect(
    code.compareDocumentPosition(open) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(
    qr.compareDocumentPosition(open) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(open).toHaveClass("w-full", "bg-success");
  expect(
    screen.getByRole("button", { name: "复制验证码" }).querySelector("svg"),
  ).not.toBeNull();
  const help = screen.getByText("在授权页面输入此一次性验证码：");
  expect(
    code.compareDocumentPosition(help) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(
    qr.compareDocumentPosition(
      screen.getByRole("button", { name: "刷新二维码" }),
    ) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

it.each(["session", "new-session"])(
  "reports only newly started authorization sessions to its owner (%s)",
  async (id) => {
    const onSessionStarted = vi.fn();
    fixture.login.mockResolvedValueOnce({ id, state: "waiting" });
    show(false, vi.fn(), "show-qr", onSessionStarted);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "刷新二维码" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "刷新二维码" }));
    await waitFor(() => expect(fixture.login).toHaveBeenCalledOnce());
    await waitFor(() =>
      expect(screen.queryByText("正在处理…")).not.toBeInTheDocument(),
    );
    if (id === "session") expect(onSessionStarted).not.toHaveBeenCalled();
    else
      expect(onSessionStarted).toHaveBeenCalledWith({
        id: "new-session",
        state: "waiting",
      });
  },
);

it("shows an official inline PNG without encoding its data URL as a QR payload", async () => {
  const value =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";
  fixture.status.account.available = false;
  fixture.status.session.actions = [{ type: "show-qr", value }];
  show(true);
  const image = await screen.findByRole("img");
  expect(image).toHaveAttribute("src", value);
  expect(image).toHaveAttribute("width", "192");
  expect(image).toHaveAttribute("height", "192");
  expect(image).toHaveAccessibleName();
  expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument();
});

it("keeps remote PNG URLs as QR payloads without loading remote images", async () => {
  const value = "https://example.com/official.png";
  fixture.status.account.available = false;
  fixture.status.session.actions = [{ type: "show-qr", value }];
  show(true);
  expect(await screen.findByLabelText("二维码内容")).toHaveTextContent(value);
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
});

it("does not display unsupported inline image formats", async () => {
  fixture.status.account.available = false;
  fixture.status.session.actions = [
    { type: "show-qr", value: "data:image/svg+xml;base64,PHN2Zy8+" },
  ];
  show(true);
  await screen.findByRole("button", { name: "刷新二维码" });
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("二维码内容")).not.toBeInTheDocument();
});
