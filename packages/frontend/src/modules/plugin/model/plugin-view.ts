import { i18n } from "@/shared/i18n";
import type {
  GithubComJieWaZiNeuxBotServerApisPluginsPluginDetailResponse as PluginDetail,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsPluginVersion as PluginVersion,
  GithubComJieWaZiNeuxBotServerInternalPluginsCatalogEntry as PluginCatalogEntry,
} from "@/shared/api/generated/types.gen";
export type { PluginDetail, PluginVersion, PluginCatalogEntry };
export type ResourceCategory = "plugins" | "skills" | "connectors";

/** 三种详情和列表共用同一简介来源，作者与名称不再占用简介位置。 */
export function pluginSummary(
  pkg: PluginCatalogEntry["package"],
  fallback?: string,
  _sourceKey?: string,
  locale = pluginLocale(),
): string {
  // 来源身份不参与替换包内的标准简介。
  void _sourceKey;
  return (
    pkg?.presentation?.localizedDescriptions?.[pluginLocale(locale)]?.trim() ||
    pkg?.presentation?.localizedDescriptions?.en?.trim() ||
    pkg?.presentation?.shortDescription?.trim() ||
    pkg?.manifest?.description?.trim() ||
    fallback?.trim() ||
    ""
  );
}

export function pluginExplanation(
  pkg: PluginCatalogEntry["package"],
  _sourceKey?: string,
): string {
  // 保留调用入口的来源参数，正文始终来自当前包。
  void _sourceKey;
  return (
    pkg?.readme ||
    pkg?.presentation?.longDescription?.trim() ||
    pluginSummary(pkg)
  );
}
export function resourceCategory(value: string | null): ResourceCategory {
  return value === "skills" || value === "connectors" ? value : "plugins";
}
/** 扩展只影响客户端设置，不重写标准组件。 */
export function pluginProvider(version: PluginVersion | undefined): string {
  const extension = version?.package?.manifest?.extensions?.["com.ally"];
  return extension &&
    typeof extension === "object" &&
    "provider" in extension &&
    typeof extension.provider === "string"
    ? extension.provider
    : "";
}

/** 新包按授权声明接入，旧包沿原有扩展读取，不维护供应商名单。 */
export function pluginHasAuthorization(
  pkg: PluginCatalogEntry["package"],
): boolean {
  return Boolean(
    pkg?.integration?.authentication?.strategy ||
    pluginProvider({ package: pkg }),
  );
}

/** 仅已存在实际安装且共享身份明确失效时，目录操作才是重新授权。 */
export function pluginNeedsAuthorization(
  _pkg: PluginCatalogEntry["package"],
  detail?: PluginDetail,
): boolean {
  return Boolean(
    detail?.installations?.length &&
    Boolean(detail.account?.checkedAt) &&
    detail.account?.available === false,
  );
}

/** 展示名来自插件声明；用户重命名始终优先。 */
export function pluginDisplayName(
  pkg: PluginCatalogEntry["package"],
  name?: string,
  locale = pluginLocale(),
): string {
  // 准备包时保存的名称可能来自任一语言，不能把已声明译名误判为用户重命名。
  const authoredNames = [
    pkg?.manifest?.name,
    pkg?.presentation?.displayName,
    ...Object.values(pkg?.presentation?.displayNames ?? {}),
  ];
  if (name && !authoredNames.includes(name)) return name;
  const localized =
    pkg?.presentation?.displayNames?.[pluginLocale(locale)]?.trim() ||
    pkg?.presentation?.displayNames?.en?.trim();
  if (localized) return localized;
  if (pkg?.presentation?.displayName) return pkg.presentation.displayName;
  const extension = pkg?.manifest?.extensions?.["com.ally"];
  return extension &&
    typeof extension === "object" &&
    "displayName" in extension &&
    typeof extension.displayName === "string"
    ? extension.displayName
    : (name ?? pkg?.manifest?.name ?? "");
}

/** 展示与正文请求共用同一语言选择，未知界面语言回退英文。 */
export function pluginLocale(
  locale = i18n.resolvedLanguage ?? i18n.language,
): "en" | "zh-CN" {
  return locale.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

/** 授权交互优先级只取包内通用声明，不按供应商猜测。 */
export function pluginPreferredAuthAction(
  pkg: PluginCatalogEntry["package"],
): "show-qr" | "open-url" | undefined {
  const action = pkg?.integration?.authentication?.preferredAction;
  return action === "show-qr" || action === "open-url" ? action : undefined;
}

/** 显示已解析安装版本；准备的新内容不能覆盖伙伴仍在使用的快照。 */
export function pluginInstalledVersion(
  detail: PluginDetail | undefined,
  wakerId = "",
) {
  if (!detail) return undefined;
  const relation = detail.installations?.find(
    (item) => item.wakerId === wakerId,
  );
  const selected = wakerId ? relation?.versionId : detail.resolvedVersionId;
  return (
    detail.versions?.find((item) => item.id === selected) ??
    detail.versions?.find((item) =>
      detail.installations?.some((relation) => relation.versionId === item.id),
    ) ??
    detail.versions?.[0]
  );
}
