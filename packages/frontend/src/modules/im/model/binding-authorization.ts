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

/** 授权状态变更保留原有响应配置及版本，不覆盖为表单默认值。 */
export function bindingAuthorizationConfiguration(
  binding: IMBinding,
  state: "approved" | "rejected",
) {
  return {
    name: binding.name || binding.externalChatId || "",
    state,
    wakerId: binding.wakerId,
    channelId: binding.channelId,
    projectId: binding.projectId,
    workingDirectory: binding.workingDirectory,
    options: binding.options,
    controllerIds: binding.controllerIds,
    allowWorkflowDelivery: binding.allowWorkflowDelivery,
    revision: binding.revision,
  };
}
