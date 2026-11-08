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
          runtimeId: "codex",
          name: "Work",
          authMode: "api-key",
          hasCredential: true,
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.example.test/v1",
            },
          ],
          selectedModels: ["work-model"],
          defaultModel: "work-model",
          defaultReasoning: "max",
        },
        {
          id: "subscription",
          runtimeId: "codex",
          name: "Subscription",
          authMode: "subscription",
          hasCredential: true,
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.openai.com/v1",
            },
          ],
        },
        {
          id: "anthropic",
          runtimeId: "claude",
          authMode: "api-key",
          hasCredential: true,
          authGeneration: 7,
          endpoints: [{ protocol: "anthropic-messages" }],
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
    configurationKnown: true,
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

function selection(
  value: ExecutionSelection,
  defaults = false,
  reasoningDefault?: "minimum",
) {
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
        defaults,
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
              configurationKnown: true,
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
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.models.map((item) => item.id)).toEqual([
      "account-model",
    ]);
    await waitFor(() =>
      expect(result.current.accounts.map((item) => item.id)).toEqual(
        compatibleAccounts,
      ),
    );
    expect([...result.current.supportedPermissions]).toEqual(permissions);
    expect(result.current.selection).toMatchObject({
      model: "account-model",
      reasoning: "native-effort",
      permissionLevel: runtimeId === "codex" ? "auto" : "default",
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
      await waitFor(() => expect(result.current.catalogRefreshing).toBe(true));
      expect(result.current.catalogPending).toBe(false);
      expect(result.current.loading).toBe(false);
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
    expect(result.current.valid).toBe(false);
  },
);

describe("统一执行配置选择", () => {
  it("同协议 Profile 隔离账号，旧账号不能读取目标目录，切换后采用目标默认配置", async () => {
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: [
            { id: "codex", enabled: true, state: "ready", isDefault: true },
            {
              id: "profile:work",
              baseRuntimeId: "codex",
              enabled: true,
              state: "ready",
              defaultAccountId: "profile-account",
              defaultModel: "profile-model",
              defaultReasoning: "low",
            },
          ],
        }),
      ),
      http.get(`${origin}/api/v1/ai-accounts`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "work",
              runtimeId: "codex",
              hasCredential: true,
              authMode: "api-key",
              endpoints: [
                {
                  protocol: "openai-responses",
                  baseUrl: "https://api.example.test/v1",
                },
              ],
              defaultModel: "work-model",
            },
            {
              id: "profile-account",
              runtimeId: "profile:work",
              hasCredential: true,
              authMode: "api-key",
              endpoints: [
                {
                  protocol: "openai-responses",
                  baseUrl: "https://api.example.test/v1",
                },
              ],
              defaultModel: "account-model",
            },
          ],
        }),
      ),
      http.get(
        `${origin}/api/v1/runtimes/:runtimeId/models`,
        ({ request, params }) => {
          requests.push(request.url);
          const accountId = new URL(request.url).searchParams.get("accountId");
          return HttpResponse.json({
            runtimeId: params.runtimeId,
            accountId,
            state: "fresh",
            defaultModel: "account-model",
            models: ["work-model", "account-model", "profile-model"].map(
              (id) => ({
                id,
                permissionLevels: ["default"],
                reasoningOptions: [{ id: "low" }, { id: "high" }],
                defaultReasoning: "high",
              }),
            ),
          });
        },
      ),
    );
    const { result, rerender } = selection({
      runtimeId: "codex",
      accountId: "work",
    });
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.accounts.map((account) => account.id)).toEqual([
      "work",
    ]);

    rerender({
      value: {
        runtimeId: "profile:work",
        accountId: "work",
        model: "work-model",
      },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.valid).toBe(false);
    expect(result.current.accounts.map((account) => account.id)).toEqual([
      "profile-account",
    ]);
    expect(result.current.models).toEqual([]);
    expect(requests).toHaveLength(1);

    rerender({ value: { runtimeId: "profile:work" } });
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      accountId: "profile-account",
      model: "profile-model",
      reasoning: "low",
    });
  });

  it("非默认账号采用自己的默认模型和推理，不继承环境默认模型", async () => {
    server.use(
      http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
        HttpResponse.json({
          runtimeId: "codex",
          accountId: "work",
          state: "fresh",
          defaultModel: "catalog-default",
          models: ["catalog-default", "work-model", "native"].map((id) => ({
            id,
            permissionLevels: ["default"],
            reasoningOptions: [{ id: "low" }, { id: "max" }],
            defaultReasoning: "low",
          })),
        }),
      ),
    );
    const { result } = selection({ runtimeId: "codex", accountId: "work" });
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      model: "work-model",
      reasoning: "max",
    });
  });

  it("只提供当前智能体的兼容账号，切换账号后查询对应模型的推理能力", async () => {
    const { result, rerender } = selection({ accountId: "system" });
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.accounts.map((item) => item.id)).toEqual([
      "work",
      "subscription",
    ]);
    expect(result.current.selection).toMatchObject({
      model: "native",
      reasoning: "high",
    });
    rerender({ value: { runtimeId: "codex", accountId: "work" } });
    await waitFor(() => expect(result.current.valid).toBe(true));
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
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      model: "native",
      reasoning: "high",
    });
  });

  it("订阅账号使用原生目录，并以账号目录默认模型读取推理选项", async () => {
    const { result } = selection({ accountId: "subscription" });
    await waitFor(() => expect(result.current.valid).toBe(true));
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
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      accountId: "work",
      model: "work-model",
    });
    rerender({ value: { accountId: "system" } });
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      accountId: "system",
      model: "native",
    });
  });

  it("保存的执行配置失效时阻止提交，不替换用户明确选择的模型", async () => {
    const { result } = selection({
      accountId: "system",
      model: "removed",
      reasoning: "high",
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.selection.model).toBe("removed");
    expect(result.current.valid).toBe(false);
  });

  it("模型限制权限时沿用安全的默认值，明确的不支持权限保持不可提交", async () => {
    const { result, rerender } = selection({
      accountId: "system",
      model: "limited",
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.selection.permissionLevel).toBe("default");
    expect([...result.current.supportedPermissions]).toEqual(["default"]);
    rerender({
      value: {
        accountId: "system",
        model: "limited",
        permissionLevel: "full-access",
      },
    });
    expect(result.current.valid).toBe(false);
  });

  it("设置默认值时将模型和推理规范化为当前目录的有效值", async () => {
    const { result } = selection(
      { accountId: "system", model: "limited", reasoning: "high" },
      true,
    );
    await waitFor(() => expect(result.current.valid).toBe(true));
    expect(result.current.selection).toMatchObject({
      model: "limited",
      reasoning: "low",
      permissionLevel: "default",
    });
  });
});

it("批量执行校验拒绝其他 Profile 的账号，不启动该账号目录查询", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(
    () =>
      useNativeExecutionSelections(
        [
          { runtimeId: "codex", accountId: "work" },
          { runtimeId: "profile:work", accountId: "work" },
        ],
        [
          { id: "codex", enabled: true, state: "ready" },
          {
            id: "profile:work",
            baseRuntimeId: "codex",
            enabled: true,
            state: "ready",
          },
        ],
        [
          {
            id: "work",
            runtimeId: "codex",
            authMode: "api-key",
            hasCredential: true,
            endpoints: [
              {
                protocol: "openai-responses",
                baseUrl: "https://api.example.test/v1",
              },
            ],
          },
        ],
      ),
    { wrapper },
  );
  await waitFor(() => expect(result.current[0]?.valid).toBe(true));
  expect(result.current[1]?.valid).toBe(false);
  expect(requests).toEqual([
    `${origin}/api/v1/runtimes/codex/models?accountId=work`,
  ]);
  queryClient.clear();
});

it("知识模型默认选择真实最低档，显式高档保留，切换模型后重新选择最低档", async () => {
  const { result, rerender } = selection(
    { accountId: "system", model: "native" },
    false,
    "minimum",
  );
  await waitFor(() => expect(result.current.valid).toBe(true));
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
  expect(result.current.valid).toBe(false);
  rerender({ value: { accountId: "subscription" } });
  await waitFor(() => expect(result.current.valid).toBe(true));
  expect(result.current.selection).toMatchObject({
    model: "subscription-model",
    fast: undefined,
  });
  release();
  expect(result.current.models.map((item) => item.id)).toEqual([
    "subscription-model",
  ]);
  rerender({ value: { accountId: "system", fast: false } });
  await waitFor(() => expect(result.current.valid).toBe(true));
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
            configurationKnown: true,
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
  await waitFor(() => expect(result.current.valid).toBe(true));
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
    expect(result.current.valid).toBe(false);
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
            configurationKnown: true,
            reasoningOptions: [{ id: "high" }],
            permissionLevels: ["default"],
          },
          {
            id: model,
            configurationKnown: false,
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
    expect(result.current.valid).toBe(false);
  }
});

it.each([
  { state: "ready", enabled: true },
  { state: "ready", enabled: false },
  { state: "unavailable", enabled: true },
])(
  "默认模型按目录回退，执行仍要求 ready + enabled ($state / $enabled)",
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
              runtimeId: "claude",
              authMode: "api-key",
              hasCredential: true,
              defaultModel: "retired-account-model",
              endpoints: [{ protocol: "anthropic-messages" }],
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
            configurationKnown: true,
            permissionLevels: ["default"],
          })),
        }),
      ),
    );
    const { result } = selection(
      {
        runtimeId: "claude",
        accountId: "anthropic",
        model: "retired-runtime-model",
      },
      true,
    );
    await waitFor(() => expect(result.current.configurationValid).toBe(true));
    expect(result.current.valid).toBe(state === "ready" && enabled);
    expect(result.current.selection.model).toBe("native-default");
  },
);

it("系统原生模型不被无关账号目录的加载或失败锁住", async () => {
  let release!: () => void;
  const accountsPending = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(`${origin}/api/v1/ai-accounts`, async () => {
      await accountsPending;
      return HttpResponse.json({ code: "DATABASE_ERROR" }, { status: 503 });
    }),
  );
  const { result } = selection({ runtimeId: "codex", accountId: "system" });
  try {
    await waitFor(() =>
      expect(result.current.models.length).toBeGreaterThan(0),
    );
    expect(result.current.accountsQuery.isPending).toBe(true);
    expect(result.current.loading).toBe(false);
    expect(result.current.valid).toBe(true);
    expect(result.current.reasoningOptions.map((option) => option.id)).toEqual([
      "high",
      "low",
    ]);
    expect(result.current.fastSupported).toBe(true);
  } finally {
    release();
  }
  await waitFor(() => expect(result.current.accountsQuery.isError).toBe(true));
  expect(result.current.error).toBeNull();
  expect(result.current.loading).toBe(false);
  expect(result.current.valid).toBe(true);
});

it("批量选择在已有有效目录后台刷新时保留模型权限和可用性", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(
    () =>
      useNativeExecutionSelections(
        [{ runtimeId: "codex", accountId: "system" }],
        [
          {
            id: "codex",
            baseRuntimeId: "codex",
            enabled: true,
            state: "ready",
            installed: true,
            defaultPermissionLevel: "auto",
          },
        ],
        [],
      ),
    { wrapper },
  );
  await waitFor(() => expect(result.current[0]?.valid).toBe(true));
  let release!: () => void;
  const refreshPending = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(`${origin}/api/v1/runtimes/codex/models`, async () => {
      await refreshPending;
      return catalogResponse("codex", "system");
    }),
  );
  let refresh: Promise<unknown> | undefined;
  act(() => {
    refresh = queryClient.invalidateQueries({
      queryKey: ["native-model-catalog"],
    });
  });
  try {
    await waitFor(() =>
      expect(
        queryClient.isFetching({ queryKey: ["native-model-catalog"] }),
      ).toBe(1),
    );
    expect(result.current[0]?.valid).toBe(true);
    expect(result.current[0]?.pending).toBe(false);
    expect([...(result.current[0]?.supportedPermissions ?? [])]).toEqual([
      "default",
      "auto",
      "full-access",
    ]);
  } finally {
    await act(async () => {
      release();
      await refresh;
    });
    queryClient.clear();
  }
});
