import type {
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeModelOption as RuntimeModel,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as Runtime,
} from "@/shared/api/generated/types.gen";
import {
  permissionLevels,
  supportedPermissionLevels,
  type PermissionLevel,
} from "@/shared/lib/runtime-permissions";

/** 看板的默认意图与模型能力分开；缺少目录时仍允许用户收紧权限。 */
export function resolveNewChatExecution({
  runtime,
  accountId,
  model,
  catalog,
  reasoning,
  partnerDefault,
  permission,
  configuredPermission,
}: {
  runtime?: Runtime;
  accountId: string;
  model?: string;
  catalog: RuntimeModel[];
  reasoning?: string;
  partnerDefault?: { model?: string; reasoning?: string };
  permission?: PermissionLevel;
  configuredPermission: PermissionLevel;
}) {
  const usesRuntimeAccount =
    accountId === (runtime?.defaultAccountId || "system");
  const selectedModel =
    model || (usesRuntimeAccount ? runtime?.defaultModel : undefined) || "";
  const detectedModels = usesRuntimeAccount ? (runtime?.models ?? []) : [];
  const models = catalog.length ? catalog : detectedModels;
  const option = selectedModel
    ? models.find((candidate) => candidate.id === selectedModel)
    : undefined;
  const provider = runtime?.baseRuntimeId || runtime?.id;
  const supported = supportedPermissionLevels(option?.permissionLevels);
  // Codex 的审批和沙箱预设与账号及模型无关。
  const knownPermissions =
    provider === "codex" ? new Set(permissionLevels) : supported;
  const permissionOptions =
    knownPermissions.size || !runtime
      ? knownPermissions
      : new Set<PermissionLevel>([
          "default",
          "full-access",
          configuredPermission,
          ...(permission ? [permission] : []),
        ]);
  const selectedPermission =
    permission ??
    (!knownPermissions.size || knownPermissions.has("full-access")
      ? "full-access"
      : knownPermissions.has(configuredPermission)
        ? configuredPermission
        : (permissionLevels.find((level) => knownPermissions.has(level)) ??
          configuredPermission));

  return {
    model: selectedModel,
    models,
    reasoning:
      reasoning ??
      (selectedModel && selectedModel === partnerDefault?.model
        ? partnerDefault.reasoning
        : undefined) ??
      "",
    reasoningOptions: option?.reasoningOptions ?? [],
    permission: selectedPermission,
    permissionOptions,
    permissionKnown: knownPermissions.size > 0,
    permissionUnsupported:
      knownPermissions.size > 0 && !knownPermissions.has(selectedPermission),
  };
}
