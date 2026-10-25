import type { AccountPresetID } from "./account-presets";

type ModelDefault = {
  displayName: string;
  contextWindow: number;
  reasoningLevels: string[];
};

type ProviderDefaults = {
  codex?: Record<string, ModelDefault>;
  claude?: Partial<Record<"sonnet" | "opus" | "haiku", string>>;
};

// 原生模型目录与配置预设。
// 只在对应协议且模型已被账号选中时应用，避免把第三方别名当成可用模型。
const providerDefaults: Partial<Record<AccountPresetID, ProviderDefaults>> = {
  deepseek: {
    codex: {
      "deepseek-flash": {
        displayName: "DeepSeek V4.1 Flash",
        contextWindow: 1048576,
        reasoningLevels: ["low", "high", "max"],
      },
      "deepseek-v4-pro": {
        displayName: "DeepSeek V4 Pro",
        contextWindow: 1048576,
        reasoningLevels: ["low", "high", "max"],
      },
    },
    claude: {
      sonnet: "deepseek-v4-pro",
      opus: "deepseek-v4-pro",
      haiku: "deepseek-v4-flash",
    },
  },
  moonshot: {
    codex: {
      "kimi-k3": {
        displayName: "Kimi K3",
        contextWindow: 1048576,
        reasoningLevels: ["low", "high", "max"],
      },
      "kimi-k2.7-code": {
        displayName: "Kimi K2.7 Code",
        contextWindow: 262144,
        reasoningLevels: ["high"],
      },
    },
    claude: {
      sonnet: "kimi-k2.7-code",
      opus: "kimi-k2.7-code",
      haiku: "kimi-k2.7-code",
    },
  },
  kimi: {
    codex: {
      "kimi-for-coding": {
        displayName: "Kimi For Coding",
        contextWindow: 1048576,
        reasoningLevels: ["low", "high", "max"],
      },
      "kimi-for-coding-highspeed": {
        displayName: "Kimi For Coding HighSpeed",
        contextWindow: 262144,
        reasoningLevels: ["high"],
      },
    },
    claude: {
      sonnet: "kimi-for-coding",
      opus: "kimi-for-coding",
      haiku: "kimi-for-coding",
    },
  },
  zhipu: {
    codex: {
      "glm-5.3": {
        displayName: "GLM-5.3",
        contextWindow: 1048576,
        reasoningLevels: ["low", "high", "max"],
      },
      "glm-5-turbo": {
        displayName: "GLM-5-Turbo",
        contextWindow: 204800,
        reasoningLevels: ["max"],
      },
    },
    claude: {
      sonnet: "glm-5.1",
      opus: "glm-5.1",
      haiku: "glm-5.1",
    },
  },
  qwen: {
    codex: {
      "qwen3.8-max": {
        displayName: "Qwen3.8 Max",
        contextWindow: 983616,
        reasoningLevels: ["low", "medium", "xhigh"],
      },
      "qwen3.8-2.4t-a95b": {
        displayName: "Qwen3.8 2.4T A95B",
        contextWindow: 983616,
        reasoningLevels: ["low", "medium", "xhigh"],
      },
      "qwen3.8-27b": {
        displayName: "Qwen3.8 27B",
        contextWindow: 983616,
        reasoningLevels: ["low", "medium", "xhigh"],
      },
    },
    claude: {
      sonnet: "qwen3.7-plus",
      opus: "qwen3.8-max",
      haiku: "qwen3.8-flash",
    },
  },
  volcengine: {
    codex: {
      "doubao-seed-2-1-pro-260628": {
        displayName: "Doubao Seed 2.1 Pro",
        contextWindow: 262144,
        reasoningLevels: ["minimal", "low", "medium", "high"],
      },
    },
  },
  volcengineCoding: {
    codex: {
      "ark-code-latest": {
        displayName: "Ark Code Latest",
        contextWindow: 256000,
        reasoningLevels: ["low", "medium", "high"],
      },
    },
  },
  volcengineAgent: {
    codex: {
      "ark-code-latest": {
        displayName: "Ark Code Latest",
        contextWindow: 256000,
        reasoningLevels: ["low", "medium", "high"],
      },
    },
  },
  minimax: {
    claude: {
      sonnet: "MiniMax-M3[1M]",
      opus: "MiniMax-M3[1M]",
      haiku: "MiniMax-M3[1M]",
    },
  },
  opencodeGo: {
    claude: {
      sonnet: "deepseek-v4-flash",
      opus: "deepseek-v4-flash",
      haiku: "deepseek-v4-flash",
    },
  },
  openrouter: {
    claude: {
      sonnet: "anthropic/claude-sonnet-5",
      opus: "anthropic/claude-opus-5",
      haiku: "anthropic/claude-haiku-4.5",
    },
  },
};

export function modelDefaultsFor(preset: AccountPresetID, modelID: string) {
  return providerDefaults[preset]?.codex?.[modelID];
}

export function claudeRoleDefaultsFor(
  preset: AccountPresetID,
  selectedModels: string[],
) {
  const defaults = providerDefaults[preset]?.claude ?? {};
  const roles = Object.fromEntries(
    Object.entries(defaults).filter(([, model]) =>
      selectedModels.includes(model),
    ),
  );
  if (preset === "deepseek" && !roles.haiku) {
    if (selectedModels.includes("deepseek-flash")) {
      roles.haiku = "deepseek-flash";
    }
  }
  if (roles.opus) roles.fable = roles.opus;
  return roles;
}
