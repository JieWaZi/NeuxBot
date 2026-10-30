import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { ReactNode } from "react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

import { client } from "@/shared/api/generated/client.gen";
import {
  useExecutionSelection,
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
          endpoints: [{ protocol: "openai-responses" }],
          selectedModels: ["work-model"],
          defaultModel: "work-model",
          defaultReasoning: "max",
        },
        {
          id: "subscription",
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
          authMode: "api-key",
          endpoints: [{ protocol: "anthropic-messages" }],
        },
      ],
    }),
  ),
  http.get(
    `${origin}/api/v1/ai-accounts/:accountId/models/native`,
    ({ request }) => {
      requests.push(request.url);
      return HttpResponse.json({
        models: ["subscription-model"],
        defaultModel: "subscription-model",
      });
    },
  ),
  http.get(
    `${origin}/api/v1/ai-accounts/:accountId/thinking`,
    ({ request }) => {
      requests.push(request.url);
      return HttpResponse.json({
        available: true,
        options: [{ id: "max", name: "Max" }],
      });
    },
  ),
);
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

describe("统一执行配置选择", () => {
  it("只提供协议兼容账号，切换账号后查询对应模型的推理能力", async () => {
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
        (url) => new URL(url).searchParams.get("model") === "work-model",
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
    expect(
      requests.some((url) => url.includes("/models/native?runtimeId=codex")),
    ).toBe(true);
    expect(
      requests.some(
        (url) =>
          new URL(url).searchParams.get("model") === "subscription-model",
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
