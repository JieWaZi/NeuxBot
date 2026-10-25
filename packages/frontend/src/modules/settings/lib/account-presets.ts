import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccountEndpoint as Endpoint } from "@/shared/api/generated/types.gen";

export const protocols = [
  "google-generative-ai",
  "openai-responses",
  "anthropic-messages",
  "openai-completions",
] as const;
export const officialEndpoints: Record<(typeof protocols)[number], string> = {
  "google-generative-ai": "https://generativelanguage.googleapis.com/v1beta",
  "openai-responses": "https://api.openai.com/v1",
  "openai-completions": "https://api.openai.com/v1",
  "anthropic-messages": "https://api.anthropic.com",
};
// 服务商协议与默认地址。
// 直连配置与套餐独立维护；Chat 转 Responses 的代理能力不属于原生协议。
export const accountPresets = {
  google: [
    {
      protocol: "google-generative-ai",
      baseUrl: "https://generativelanguage.googleapis.com/v1beta",
    },
  ],
  openai: [
    {
      protocol: "openai-responses",
      baseUrl: "https://api.openai.com/v1",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://api.openai.com/v1",
    },
  ],
  anthropic: [
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.anthropic.com",
    },
  ],
  opencodeGo: [
    {
      protocol: "openai-completions",
      baseUrl: "https://opencode.ai/zen/go/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://opencode.ai/zen/go",
    },
  ],
  deepseek: [
    {
      protocol: "openai-responses",
      baseUrl: "https://api.deepseek.com",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.deepseek.com/anthropic",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://api.deepseek.com/v1",
    },
  ],
  zhipu: [
    {
      protocol: "openai-responses",
      baseUrl: "https://open.bigmodel.cn/api/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://open.bigmodel.cn/api/anthropic",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://open.bigmodel.cn/api/coding/paas/v4",
    },
  ],
  moonshot: [
    {
      protocol: "openai-completions",
      baseUrl: "https://api.moonshot.cn/v1",
    },
    {
      protocol: "openai-responses",
      baseUrl: "https://api.moonshot.cn/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.moonshot.cn/anthropic",
    },
  ],
  kimi: [
    {
      protocol: "openai-completions",
      baseUrl: "https://api.kimi.com/coding/v1",
    },
    {
      protocol: "openai-responses",
      baseUrl: "https://api.kimi.com/coding/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.kimi.com/coding",
    },
  ],
  minimax: [
    {
      protocol: "openai-responses",
      baseUrl: "https://api.minimax.cn/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.minimax.cn/anthropic",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://api.minimax.cn/v1",
    },
  ],
  qwen: [
    {
      protocol: "openai-responses",
      baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://dashscope.aliyuncs.com/apps/anthropic",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    },
  ],
  volcengine: [
    {
      protocol: "openai-responses",
      baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
    },
  ],
  volcengineCoding: [
    {
      protocol: "openai-responses",
      baseUrl: "https://ark.cn-beijing.volces.com/api/coding/v3",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://ark.cn-beijing.volces.com/api/coding",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://ark.cn-beijing.volces.com/api/coding/v3",
    },
  ],
  volcengineAgent: [
    {
      protocol: "openai-responses",
      baseUrl: "https://ark.cn-beijing.volces.com/api/plan/v3",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://ark.cn-beijing.volces.com/api/plan",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://ark.cn-beijing.volces.com/api/plan/v3",
    },
  ],
  siliconflow: [
    {
      protocol: "openai-completions",
      baseUrl: "https://api.siliconflow.cn/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://api.siliconflow.cn",
    },
  ],
  openrouter: [
    {
      protocol: "openai-responses",
      baseUrl: "https://openrouter.ai/api/v1",
    },
    {
      protocol: "anthropic-messages",
      baseUrl: "https://openrouter.ai/api",
    },
    {
      protocol: "openai-completions",
      baseUrl: "https://openrouter.ai/api/v1",
    },
  ],
  custom: [
    {
      protocol: "openai-responses",
      baseUrl: "",
    },
  ],
} satisfies Record<string, Endpoint[]>;

export type AccountPresetID = keyof typeof accountPresets;

export const accountPresetIDs = Object.keys(
  accountPresets,
) as AccountPresetID[];

export const accountProviderIDs = accountPresetIDs.filter(
  (id) =>
    id !== "moonshot" && id !== "volcengineCoding" && id !== "volcengineAgent",
);

export function providerForPreset(id: AccountPresetID): AccountPresetID {
  if (id === "moonshot") return "kimi";
  if (id === "volcengineCoding" || id === "volcengineAgent")
    return "volcengine";
  return id;
}

export const providerWebsites: Record<
  Exclude<AccountPresetID, "custom">,
  string
> = {
  google: "https://aistudio.google.com/",
  openai: "https://platform.openai.com/",
  anthropic: "https://platform.claude.com/",
  opencodeGo: "https://opencode.ai/console",
  deepseek: "https://platform.deepseek.com/",
  zhipu: "https://open.bigmodel.cn/",
  moonshot: "https://platform.kimi.ai/",
  kimi: "https://www.kimi.com/code",
  minimax: "https://platform.minimax.io/",
  qwen: "https://bailian.console.aliyun.com/",
  volcengine: "https://console.volcengine.com/ark/",
  volcengineCoding: "https://console.volcengine.com/ark/",
  volcengineAgent: "https://console.volcengine.com/ark/",
  siliconflow: "https://cloud.siliconflow.cn/",
  openrouter: "https://openrouter.ai/",
};

export function presetForEndpoint(
  endpoint: Endpoint | undefined,
): AccountPresetID {
  if (!endpoint) return "openai";
  for (const id of accountPresetIDs) {
    if (
      id !== "custom" &&
      accountPresets[id].some(
        (item) =>
          item.protocol === endpoint.protocol &&
          item.baseUrl === endpoint.baseUrl,
      )
    ) {
      return id;
    }
  }
  return "custom";
}
