import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { ReactNode } from "react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

import { client } from "@/shared/api/generated/client.gen";
import {
  useExecutionSelection,
  useNativeExecutionSelections,
  type ExecutionSelection,
} from "./use-execution-selection";

const origin = "http://execution-selection.test";
const requests: string[] = [];
const server = setupServer(
  http.get(`${origin}/api/v1/runtimes`, () =>
    HttpResponse.json({
      resources: [
        {
          id: "codex",
          baseRuntimeId: "codex",
          enabled: true,
          state: "ready",
          isDefault: true,
          defaultModel: "native",
          defaultReasoning: "high",
          defaultPermissionLevel: "auto",
          models: [
            {
              id: "native",
              permissionLevels: ["default", "auto", "full-access"],
              reasoningOptions: [{ id: "high" }, { id: "low" }],
            },
            {
              id: "limited",
              permissionLevels: ["default"],
              reasoningOptions: [{ id: "low" }],
            },
          ],
        },
      ],
    }),
  ),
  http.get(`${origin}/api/v1/ai-accounts`, () =>
    HttpResponse.json({
      resources: [
        {
          id: "work",
          name: "Work",
          authMode: "api-key",
          hasCredential: true,
          endpoint: { protocol: "openai-responses" },
          selectedModels: ["work-model"],
          defaultModel: "work-model",
          defaultReasoning: "max",
        },
        {
          id: "subscription",
          name: "Subscription",
          authMode: "subscription",
          hasCredential: true,
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://api.openai.com/v1",
          },
        },
        {
          id: "anthropic",
          authMode: "api-key",
          hasCredential: true,
          authGeneration: 7,
          endpoint: { protocol: "anthropic-messages" },
        },
      ],
    }),
  ),
  http.get(
    `${origin}/api/v1/runtimes/:runtimeId/models`,
    ({ request, params }) => {
      requests.push(request.url);
      return catalogResponse(
        String(params.runtimeId),
        new URL(request.url).searchParams.get("accountId") || "system",
      );
    },
  ),
);
function catalogResponse(runtimeId: string, accountId: string) {
  const model = (
    id: string,
    defaultReasoning: string,
    efforts: string[],
    permissionLevels = ["default", "auto", "full-access"],
    fastSupported = false,
  ) => ({
    id,
    name: id,
    defaultReasoning,

    permissionLevels,
    fastSupported,
    reasoningOptions: efforts.map((id) => ({ id })),
  });
  const models =
    accountId === "work"
      ? [model("work-model", "max", ["max"])]
      : accountId === "subscription"
        ? [model("subscription-model", "", [])]
        : [
            model("native", "high", ["high", "low"], undefined, true),
            model("limited", "low", ["low"], ["default"]),
          ];
  return HttpResponse.json({
    runtimeId,
    accountId,
    state: "fresh",
    defaultModel: models[0]?.id,
    models,
  });
}

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  requests.length = 0;
});
afterAll(() => server.close());

function selection(value: ExecutionSelection, reasoningDefault?: "minimum") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return renderHook(
    ({ value }) =>
      useExecutionSelection({
        value,
        reasoningDefault,
      }),
    {
      wrapper,
      initialProps: { value },
    },
  );
}

it.each([
  ["codex", ["default", "auto", "full-access"], ["work", "subscription"]],
  ["claude", ["default", "full-access"], ["anthropic"]],
  ["cursor", ["default", "full-access"], []],
] as const)(
  "%s 使用原生账号目录的配置，并保留 Fast 三态",
  async (runtimeId, permissions, compatibleAccounts) => {
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: [
            {
              id: runtimeId,
              enabled: true,
              state: "ready",
              defaultPermissionLevel: "auto",
              models: [{ id: "summary-only", permissionLevels: ["auto"] }],
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/runtimes/:runtimeId/models`, ({ request }) => {
        requests.push(request.url);
        return HttpResponse.json({
          runtimeId,
          accountId: "system",
          state: "fresh",
          defaultModel: "account-model",
          models: [
            {
              id: "account-model",

              reasoningOptions: [{ id: "native-effort" }],
              defaultReasoning: "native-effort",
              fastSupported: true,
              permissionLevels: permissions,
            },
          ],
        });
      }),
    );
    const { result, rerender } = selection({ runtimeId, accountId: "system" });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.models.map((item) => item.id)).toEqual([
      "account-model",
    ]);
    expect(result.current.accounts.map((item) => item.id)).toEqual(
      compatibleAccounts,
    );
    expect([...result.current.supportedPermissions]).toEqual(permissions);
    expect(result.current.selection).toMatchObject({
      model: "account-model",
      reasoning: "native-effort",
      permissionLevel: "auto",
      fast: undefined,
    });
    rerender({ value: { runtimeId, accountId: "system", fast: false } });
    expect(result.current.selection.fast).toBe(false);
    rerender({ value: { runtimeId, accountId: "system", fast: true } });
    expect(result.current.selection.fast).toBe(true);
    expect(requests).toContain(
      `${origin}/api/v1/runtimes/${runtimeId}/models?accountId=system`,
    );

    let release!: () => void;
    const refreshing = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get(
        `${origin}/api/v1/runtimes/:runtimeId/models`,
        async ({ request }) => {
          requests.push(request.url);
          await refreshing;
          return HttpResponse.json(
            { code: "AI_ACCOUNT_MODELS_UNAVAILABLE" },
            { status: 503 },
          );
        },
      ),
    );
    let refresh: Promise<unknown> | undefined;
    act(() => {
      refresh = result.current.refreshCatalog();
    });
    try {
      await waitFor(() => expect(result.current.catalogPending).toBe(true));
      expect(result.current.valid).toBe(true);
      expect(result.current.models[0]?.id).toBe("account-model");
    } finally {
      await act(async () => {
        release();
        await refresh;
      });
    }
    expect(new URL(requests.at(-1) ?? origin).searchParams.get("refresh")).toBe(
      "true",
    );
    await waitFor(() => expect(result.current.catalogState).toBe("stale"));
    expect(result.current.models.map((item) => item.id)).toEqual([
      "account-model",
    ]);
    expect(result.current.valid).toBe(true);
  },
);

describe("统一执行配置选择", () => {
  it("只提供协议兼容账号，切换账号后查询对应模型的推理能力", async () => {
    const { result, rerender } = selection({ accountId: "system" });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.accounts.map((item) => item.id)).toEqual([
      "work",
      "subscription",
    ]);
    expect(result.current.selection).toMatchObject({
      model: "native",
      reasoning: "high",
    });
    rerender({ value: { runtimeId: "codex", accountId: "work" } });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.models.map((item) => item.id)).toEqual([
      "work-model",
    ]);
    expect(result.current.selection).toMatchObject({
      model: "work-model",
      reasoning: "max",
    });
    expect(
      requests.some(
        (url) => new URL(url).searchParams.get("accountId") === "work",
      ),
    ).toBe(true);
    rerender({ value: { runtimeId: "codex", accountId: "system" } });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection).toMatchObject({
      model: "native",
      reasoning: "high",
    });
  });

  it("订阅账号使用原生目录，并以账号目录默认模型读取推理选项", async () => {
    const { result } = selection({ accountId: "subscription" });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection.model).toBe("subscription-model");
    expect(result.current.models.map((item) => item.id)).toEqual([
      "subscription-model",
    ]);
    expect(result.current.reasoningOptions).toEqual([]);
    expect(
      requests.some(
        (url) => new URL(url).searchParams.get("accountId") === "subscription",
      ),
    ).toBe(true);
  });

  it("旧配置的空账号继承智能体默认账号，显式当前登录账号保持原生目录", async () => {
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "codex",
              enabled: true,
              state: "ready",
              defaultAccountId: "work",
              defaultModel: "native",
              models: [{ id: "native", permissionLevels: ["default"] }],
            },
          ],
        }),
      ),
    );
    const { result, rerender } = selection({ accountId: "" });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection).toMatchObject({
      accountId: "work",
      model: "native",
    });
    rerender({ value: { accountId: "system" } });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection).toMatchObject({
      accountId: "system",
      model: "native",
    });
  });

  it("目录未列出已保存模型时仍允许原生尝试并保留选择", async () => {
    const { result } = selection({
      accountId: "system",
      model: "removed",
      reasoning: "high",
    });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.selection.model).toBe("removed");
    expect(result.current.valid).toBe(true);
  });

  it("目录限制只决定权限菜单，明确权限交给原生执行确认", async () => {
    const { result, rerender } = selection({
      accountId: "system",
      model: "limited",
    });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.selection.permissionLevel).toBe("auto");
    expect([...result.current.supportedPermissions]).toEqual(["default"]);
    rerender({
      value: {
        accountId: "system",
        model: "limited",
        permissionLevel: "full-access",
      },
    });
    expect(result.current.valid).toBe(true);
  });

  it("设置默认值保留明确模型和推理选择", async () => {
    const { result } = selection({
      accountId: "system",
      model: "limited",
      reasoning: "high",
    });
    await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection).toMatchObject({
      model: "limited",
      reasoning: "high",
      permissionLevel: "auto",
    });
  });
});

it("知识模型默认选择真实最低档，显式高档保留，切换模型后重新选择最低档", async () => {
  const { result, rerender } = selection(
    { accountId: "system", model: "native" },
    "minimum",
  );
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  expect(result.current.selection.reasoning).toBe("low");
  rerender({
    value: { accountId: "system", model: "native", reasoning: "high" },
  });
  expect(result.current.selection.reasoning).toBe("high");
  rerender({ value: { accountId: "system", model: "limited", reasoning: "" } });
  expect(result.current.selection.reasoning).toBe("low");
});

it("账号切换隔离迟到目录，显式关闭 Fast 保持 false", async () => {
  let release!: () => void;
  let entered = false;
  const slow = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(
      `${origin}/api/v1/runtimes/:runtimeId/models`,
      async ({ request, params }) => {
        const accountId =
          new URL(request.url).searchParams.get("accountId") || "system";
        if (accountId === "system") {
          entered = true;
          await slow;
        }
        return catalogResponse(String(params.runtimeId), accountId);
      },
    ),
  );
  const { result, rerender } = selection({ accountId: "system", fast: false });
  await waitFor(() => expect(entered).toBe(true));
  rerender({ value: { accountId: "subscription", fast: false } });
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  rerender({ value: { accountId: "subscription" } });
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  expect(result.current.selection).toMatchObject({
    model: "subscription-model",
    fast: undefined,
  });
  release();
  expect(result.current.models.map((item) => item.id)).toEqual([
    "subscription-model",
  ]);
  rerender({ value: { accountId: "system", fast: false } });
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  expect(result.current.selection).toMatchObject({
    model: "native",
    fast: false,
  });
});

it("Claude 切换到受管 API 账号时不会显示系统账号的迟到目录", async () => {
  let release!: () => void;
  let entered = false;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [{ id: "claude", enabled: true, state: "ready" }],
      }),
    ),
    http.get(`${origin}/api/v1/runtimes/claude/models`, async ({ request }) => {
      const accountId = new URL(request.url).searchParams.get("accountId");
      if (accountId === "system") {
        entered = true;
        await pending;
      }
      return HttpResponse.json({
        runtimeId: "claude",
        accountId,
        authGeneration: accountId === "anthropic" ? 7 : undefined,
        state: "fresh",
        models: [
          {
            id: accountId,

            permissionLevels: ["default"],
          },
        ],
      });
    }),
  );
  const { result, rerender } = selection({
    runtimeId: "claude",
    accountId: "system",
  });
  await waitFor(() => expect(entered).toBe(true));
  rerender({ value: { runtimeId: "claude", accountId: "anthropic" } });
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  await act(async () => release());
  expect(result.current.models.map((item) => item.id)).toEqual(["anthropic"]);
});

it.each([
  { models: [], issue: "catalogEmpty" },
  {
    models: [{ id: "listed", permissionLevels: ["default"] }],
    issue: "catalogModelUnavailable",
  },
])(
  "原生目录区分可重试的空列表和未列出的模型 $issue",
  async ({ models, issue }) => {
    server.use(
      http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
        HttpResponse.json({
          runtimeId: "codex",
          accountId: "system",
          state: "fresh",
          models,
        }),
      ),
    );
    const { result } = selection({
      runtimeId: "codex",
      accountId: "system",
      model: "missing",
    });
    await waitFor(() => expect(result.current.catalogIssue).toBe(issue));
    expect(result.current.valid).toBe(true);
    expect(result.current.catalogPending).toBe(false);
  },
);

it("WorkBuddy 允许已列出但可选配置未知的模型使用原生默认值", async () => {
  const model = "DeepSeek-V4.1-Flash";
  server.use(
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [{ id: "workbuddy", enabled: true, state: "ready" }],
      }),
    ),
    http.get(`${origin}/api/v1/runtimes/workbuddy/models`, () =>
      HttpResponse.json({
        runtimeId: "workbuddy",
        accountId: "system",
        state: "fresh",
        defaultModel: "opening-model",
        models: [
          {
            id: "opening-model",

            reasoningOptions: [{ id: "high" }],
            permissionLevels: ["default"],
          },
          {
            id: model,

            reasoningOptions: [],
            fastSupported: false,
            permissionLevels: ["default"],
          },
        ],
      }),
    ),
  );
  const value = { runtimeId: "workbuddy", accountId: "system", model };
  const { result, rerender } = selection(value);
  await waitFor(() => expect(result.current.catalogState).toBe("fresh"));
  expect(result.current.valid).toBe(true);
  expect(result.current.catalogIssue).toBeUndefined();
  expect(result.current.selection).toMatchObject({
    model,
    permissionLevel: "default",
    reasoning: "",
    fast: undefined,
  });
  expect(result.current.reasoningOptions).toEqual([]);
  expect(result.current.fastSupported).toBe(false);

  for (const override of [
    { reasoning: "high" },
    { fast: false },
    { permissionLevel: "full-access" },
  ] as const) {
    rerender({ value: { ...value, ...override } });
    expect(result.current.valid).toBe(true);
  }
});

it.each([
  { state: "ready", enabled: true },
  { state: "ready", enabled: false },
  { state: "unavailable", enabled: true },
])(
  "默认模型保持已保存选择，执行只要求配置启用 ($state / $enabled)",
  async ({ state, enabled }) => {
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "claude",
              enabled,
              state,
              installed: true,
              defaultAccountId: "anthropic",
              defaultModel: "retired-runtime-model",
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/ai-accounts`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "anthropic",
              authMode: "api-key",
              hasCredential: true,
              defaultModel: "retired-account-model",
              endpoint: { protocol: "anthropic-messages" },
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/runtimes/claude/models`, () =>
        HttpResponse.json({
          runtimeId: "claude",
          accountId: "anthropic",
          state: "fresh",
          defaultModel: "native-default",
          models: ["first", "native-default"].map((id) => ({
            id,

            permissionLevels: ["default"],
          })),
        }),
      ),
    );
    const { result } = selection({
      runtimeId: "claude",
      accountId: "anthropic",
      model: "retired-runtime-model",
    });
    await waitFor(() => expect(result.current.configurationValid).toBe(true));
    expect(result.current.valid).toBe(enabled);
    expect(result.current.selection.model).toBe("retired-runtime-model");
  },
);

it.each(["stale", "unavailable", "signed_out", "error"])(
  "探测失败不阻止执行或清除明确选择：%s",
  async (state) => {
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "codex",
              enabled: true,
              state: "unavailable",
              installed: false,
              isDefault: true,
              defaultModel: "saved-model",
              defaultReasoning: "high",
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
        state === "error"
          ? HttpResponse.json(
              { code: "AI_ACCOUNT_MODELS_UNAVAILABLE" },
              { status: 503 },
            )
          : HttpResponse.json({
              runtimeId: "codex",
              accountId: "system",
              state,
              models: [],
            }),
      ),
    );
    const value = {
      runtimeId: "codex",
      accountId: "system",
      model: "chosen-model",
      reasoning: "high",
      fast: false,
      contextWindow: "1m",
      permissionLevel: "full-access",
    };
    const { result } = selection(value);
    await waitFor(() =>
      expect(result.current.runtimesQuery.isSuccess).toBe(true),
    );
    await waitFor(() =>
      expect(result.current.accountsQuery.isSuccess).toBe(true),
    );
    await waitFor(() => expect(result.current.catalogPending).toBe(false));
    expect(result.current.valid).toBe(true);
    expect(result.current.selection).toMatchObject(value);
    expect(result.current.runtimes.map((runtime) => runtime.id)).toEqual([
      "codex",
    ]);
  },
);

it("批量配置在目录不可用时保留配置并只校验身份与权限枚举", async () => {
  server.use(
    http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
      HttpResponse.json({
        runtimeId: "codex",
        accountId: "system",
        state: "stale",
        models: [],
      }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const value = {
    runtimeId: "codex",
    accountId: "system",
    model: "saved",
    reasoning: "high",
    fast: true,
    contextWindow: "1m",
    permissionLevel: "full-access",
  };
  const runtimes = [
    { id: "codex", enabled: true, state: "unavailable", installed: true },
  ];
  const { result } = renderHook(
    () =>
      useNativeExecutionSelections(
        [
          value,
          { ...value, accountId: "missing" },
          { ...value, permissionLevel: "invalid" },
        ],
        runtimes,
        [],
      ),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    },
  );
  await waitFor(() => expect(result.current[0]?.pending).toBe(false));
  expect(result.current[0]).toMatchObject({ valid: true, selection: value });
  expect(result.current[1]?.valid).toBe(false);
  expect(result.current[2]?.valid).toBe(false);
});

it.each([
  { runtimeId: "codex", accountId: "missing" },
  { runtimeId: "codex", accountId: "anthropic" },
  { runtimeId: "codex", accountId: "work", permissionLevel: "invalid" },
])("目录故障不绕过真实账号和权限枚举约束 %j", async (value) => {
  server.use(
    http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
      HttpResponse.error(),
    ),
  );
  const { result } = selection(value);
  await waitFor(() =>
    expect(result.current.accountsQuery.isSuccess).toBe(true),
  );
  expect(result.current.valid).toBe(false);
});

it("缺少凭据的兼容账号仍不能执行", async () => {
  server.use(
    http.get(`${origin}/api/v1/ai-accounts`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "work",
            authMode: "api-key",
            hasCredential: false,
            endpoint: { protocol: "openai-responses" },
          },
        ],
      }),
    ),
  );
  const { result } = selection({ runtimeId: "codex", accountId: "work" });
  await waitFor(() =>
    expect(result.current.accountsQuery.isSuccess).toBe(true),
  );
  expect(result.current.valid).toBe(false);
});
