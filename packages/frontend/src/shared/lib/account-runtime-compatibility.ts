import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as AIAccount } from "@/shared/api/generated/types.gen";

const compatibleProtocols: Record<string, readonly string[]> = {
  opencode: [
    "openai-responses",
    "openai-completions",
    "anthropic-messages",
    "google-generative-ai",
  ],
  gemini: ["google-generative-ai"],
  grok: ["openai-responses", "openai-completions"],
  codex: ["openai-responses"],
  claude: ["anthropic-messages"],
  kimi: ["openai-responses", "openai-completions", "anthropic-messages"],
  pi: ["openai-responses", "openai-completions", "anthropic-messages"],
};

export function isKimiSubscription(account?: AIAccount): boolean {
  return Boolean(
    account?.authMode === "subscription" &&
    account.endpoint?.protocol === "openai-completions" &&
    account.endpoint?.baseUrl === "https://api.kimi.com/coding/v1",
  );
}

// 受管账号只可交给原生支持该协议的智能体；订阅还必须匹配发行方。
export function accountSupportsRuntime(
  account: AIAccount,
  runtimeId: string,
): boolean {
  const endpoint = account.endpoint;
  if (!endpoint?.protocol) return false;
  if (account.authMode === "subscription") {
    return (
      (runtimeId === "codex" &&
        endpoint.protocol === "openai-responses" &&
        endpoint.baseUrl === "https://api.openai.com/v1") ||
      (runtimeId === "claude" &&
        endpoint.protocol === "anthropic-messages" &&
        endpoint.baseUrl === "https://api.anthropic.com") ||
      (runtimeId === "kimi" && isKimiSubscription(account))
    );
  }
  return compatibleProtocols[runtimeId]?.includes(endpoint.protocol) ?? false;
}
