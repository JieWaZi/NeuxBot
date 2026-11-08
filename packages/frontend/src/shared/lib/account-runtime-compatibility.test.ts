import { expect, it } from "vitest";

import { accountSupportsRuntime } from "./account-runtime-compatibility";

it("Claude 官方订阅可进入模型选择，代理端点和其他智能体不能借用该订阅", () => {
  const account = {
    authMode: "subscription" as const,
    endpoints: [
      {
        protocol: "anthropic-messages" as const,
        baseUrl: "https://api.anthropic.com",
      },
    ],
  };
  expect(accountSupportsRuntime(account, "claude")).toBe(true);
  expect(accountSupportsRuntime(account, "codex")).toBe(false);
  expect(accountSupportsRuntime(account, "opencode")).toBe(false);
  expect(
    accountSupportsRuntime(
      {
        ...account,
        endpoints: [
          {
            protocol: "anthropic-messages",
            baseUrl: "https://proxy.example.test",
          },
        ],
      },
      "claude",
    ),
  ).toBe(false);
});
