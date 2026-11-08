import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption } from "@/shared/api/generated/types.gen";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";

/** 每个设置操作都保留此智能体其他已保存的模型和账号选项。 */
export function runtimeSettingsBody(runtime: RuntimeOption) {
  return {
    enabled: runtime.enabled ?? false,
    isDefault: runtime.isDefault ?? false,
    defaultAccountId: runtime.defaultAccountId ?? "",
    defaultModel: runtime.defaultModel ?? "",
    defaultReasoning: runtime.defaultReasoning ?? "",
    defaultFast: runtime.defaultFast,
    defaultPermissionLevel:
      permissionLevelOf(runtime.defaultPermissionLevel) ?? "auto",
  };
}
