import type { IMBinding } from "../types";

/** 已保存完整响应目标时直接授权；工作范围留空和默认模型都是有效配置。 */
export function canAuthorizeWithoutConfiguration(binding: IMBinding) {
  const hasTarget = Boolean(binding.wakerId) !== Boolean(binding.channelId);
  const hasExecution = Boolean(binding.channelId || binding.options?.runtimeId);
  const hasControllers =
    binding.kind !== "group" ||
    Boolean(binding.controllerIds?.some((value) => value.trim()));
  return hasTarget && hasExecution && hasControllers;
}
