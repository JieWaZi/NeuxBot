import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as RuntimeReasoningOption } from "@/shared/api/generated/types.gen";

/** 最低档使用模型真实选项，优先关闭及已知强度；未知档位沿目录顺序。 */
export function minimumReasoning(options: RuntimeReasoningOption[]) {
  for (const id of [
    "none",
    "off",
    "disabled",
    "minimal",
    "min",
    "low",
    "medium",
    "high",
    "max",
    "xhigh",
  ]) {
    const option = options.find((item) => item.id?.toLowerCase() === id);
    if (option?.id) return option.id;
  }
  return (
    options.find(
      (item) => item.id && !["auto", "default"].includes(item.id.toLowerCase()),
    )?.id ?? ""
  );
}

const unrankedReasoningIds = new Set([
  "enabled",
  "disabled",
  "on",
  "off",
  "default",
  "auto",
]);

/** 最高档只取具体强度目录；开关、自动和模型默认值不参与强度排序。 */
export function isMaximumReasoning(
  options: RuntimeReasoningOption[] | undefined,
  reasoning: string,
  defaultReasoning = "",
) {
  const available = (options ?? []).filter(
    (option) => option.id && !unrankedReasoningIds.has(option.id.toLowerCase()),
  );
  return (
    available.length > 0 &&
    available.at(-1)?.id === (reasoning || defaultReasoning)
  );
}
