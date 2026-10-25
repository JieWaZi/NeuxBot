import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as RuntimeReasoningOption } from "@/shared/api/generated/types.gen";

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
