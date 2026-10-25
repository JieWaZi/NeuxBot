import { expect, it } from "vitest";
import { accountSupportsRuntime } from "./account-runtime-compatibility";
import { runtimeDisplayName } from "./runtime-display-name";

it("Google 账号只显示实际支持 GenerateContent 的原生运行时", () => {
  const account = {
    authMode: "api-key" as const,
    endpoints: [
      {
        baseUrl: "https://example.test/v1beta",
        protocol: "google-generative-ai" as const,
      },
    ],
  };
  expect(accountSupportsRuntime(account, "gemini")).toBe(true);
  expect(accountSupportsRuntime(account, "opencode")).toBe(true);
  expect(accountSupportsRuntime(account, "grok")).toBe(false);
  expect(accountSupportsRuntime(account, "codex")).toBe(false);
});

it("Grok 只接受已验证认证路径的 Chat 和 Responses 账号", () => {
  expect(
    accountSupportsRuntime(
      {
        authMode: "api-key",
        endpoints: [
          { protocol: "openai-responses", baseUrl: "https://example.test/v1" },
        ],
      },
      "grok",
    ),
  ).toBe(true);
  expect(
    accountSupportsRuntime(
      {
        authMode: "api-key",
        endpoints: [
          {
            protocol: "openai-completions",
            baseUrl: "https://example.test/v1",
          },
        ],
      },
      "grok",
    ),
  ).toBe(true);
  expect(
    accountSupportsRuntime(
      {
        authMode: "api-key",
        endpoints: [
          {
            protocol: "anthropic-messages",
            baseUrl: "https://example.test/v1",
          },
        ],
      },
      "grok",
    ),
  ).toBe(false);
  expect(runtimeDisplayName({ id: "gemini" })).toBe("Gemini CLI");
  expect(runtimeDisplayName({ id: "grok" })).toBe("Grok Build");
  expect(runtimeDisplayName({ id: "opencode" })).toBe("OpenCode");
});
