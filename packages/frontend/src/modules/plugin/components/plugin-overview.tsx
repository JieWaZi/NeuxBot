import { PluginInstalledPartners } from "./plugin-installed-partners";
import { useState, type ReactNode } from "react";
import { Cable, ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { openExternalURL } from "@/shared/platform";
import { DetailSection } from "@/shared/ui/detail-section";
import { SurfacePatternPanel } from "@/shared/ui/surface-pattern-panel";
import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { PluginReadme } from "./plugin-readme";
import type { ReadmeSource } from "./plugin-readme";
import {
  PluginSkillDialog,
  type PluginSkillContext,
} from "./plugin-skill-dialog";
import { Button } from "@/shared/ui/button";
import { PluginConnectorDialog } from "./plugin-connector-dialog";
import {
  ResourceListItem,
  ResourceItemAction,
} from "@/shared/ui/resource-list-item";
import { SkillIcon } from "@/modules/skill";
import {
  pluginExplanation,
  pluginDisplayName,
  pluginLocale,
} from "../model/plugin-view";
import type { PluginCatalogEntry, PluginDetail } from "../model/plugin-view";

/** 两种插件详情复用同一份包信息与真实安装关系，不复制安装状态。 */
export function PluginOverview({
  pkg,
  sourceKey,
  detail,
  scope = "global",
  installationStatus,
  skillContext,
  onUninstall,
  uninstalling,
  readmeSource,
}: {
  pkg?: PluginCatalogEntry["package"];
  sourceKey?: string;
  scope?: "global" | "partner";
  detail?: PluginDetail;
  installationStatus?: ReactNode;
  skillContext?: PluginSkillContext;
  onUninstall?: (id: string) => void;
  uninstalling?: boolean;
  readmeSource?: ReadmeSource;
}) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const [linkError, setLinkError] = useState(false);
  const [connectorKey, setConnectorKey] = useState<string | null>(null);
  const manifest = pkg?.manifest;
  const [showSkills, setShowSkills] = useState(false);
  const [skill, setSkill] = useState<{
    key: string;
    name: string;
    target?: string;
  } | null>(null);

  // 公开能力优先采用自有声明，再固定选择上游正文来源；不改变运行时集合。
  const upstream = ["codex", "claude"].flatMap((target) => {
    const implementation = pkg?.nativeImplementations?.[target];
    return implementation ? [{ target, implementation }] : [];
  });
  const allSkills = [
    ...(pkg?.skills ?? []).map((item) => ({ ...item, target: "" })),
    ...upstream.flatMap(({ target, implementation }) =>
      (implementation.skills ?? []).map((item) => ({ ...item, target })),
    ),
  ];
  const allServers = [
    ...(pkg?.servers ?? []).map((item) => ({
      key: item.key,
      name: item.key,
      endpoint: publicOrigin(item.url),
      target: "",
    })),
    ...upstream.flatMap(({ target, implementation }) =>
      (implementation.servers ?? []).map((item) => ({ ...item, target })),
    ),
  ];
  const skillKeys = new Set<string>();
  const skills = allSkills.filter((item) => {
    const key = (item.name || item.key || "").trim().toLowerCase();
    if (skillKeys.has(key)) return false;
    skillKeys.add(key);
    return true;
  });
  const serverKeys = new Set<string>();
  const servers = allServers.filter((item) => {
    const key = (item.key ?? "").trim().toLowerCase();
    if (serverKeys.has(key)) return false;
    serverKeys.add(key);
    return true;
  });
  const presentation = pkg?.presentation;
  const upstreamSource = pkg?.upstream;
  const resourceSource = sourceKey ?? detail?.plugin?.sourceKey;
  const readmeVersion = detail?.versions?.find((item) => item.package === pkg);
  const developer = upstreamSource?.author || presentation?.developerName;
  const explanation =
    pluginExplanation(pkg, resourceSource) ||
    detail?.plugin?.description ||
    t("noExplanation");
  const info = [
    [t("packageVersion"), upstreamSource?.tag || upstreamSource?.version],
    [t("license"), upstreamSource?.license || manifest?.license],
  ];
  const urls = [
    [t("website"), presentation?.websiteURL || manifest?.homepage],
    [t("repository"), upstreamSource?.repository || manifest?.repository],
    [t("privacy"), presentation?.privacyPolicyURL],
    [t("terms"), presentation?.termsOfServiceURL],
  ];
  return (
    <div className="@container/plugin-overview space-y-8">
      <SurfacePatternPanel pattern="wash" surface="scene" className="p-5">
        <div className="grid items-start gap-6 @[48rem]/plugin-overview:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          <DetailSection title={t("pluginReadme")}>
            <PluginReadme
              key={`${locale}:${resourceSource ?? ""}:${readmeVersion?.id ?? manifest?.version ?? ""}:${explanation}`}
              content={explanation}
              status={
                pkg?.readmeStatus ?? (pkg?.readme ? "available" : "missing")
              }
              source={
                readmeSource
                  ? { ...readmeSource, locale }
                  : detail?.plugin?.id && readmeVersion?.id
                    ? {
                        pluginId: detail.plugin.id,
                        versionId: readmeVersion.id,
                        locale,
                      }
                    : undefined
              }
            />
          </DetailSection>
          <DetailSection title={t("information")}>
            <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-4">
              {developer && (
                <ReadOnlyField
                  label={t("developer")}
                  layout="row"
                  className="contents [&>dd]:col-span-3"
                >
                  {developer}
                </ReadOnlyField>
              )}
              {info
                .filter(([, value]) => value)
                .map(([label, value]) => (
                  <ReadOnlyField
                    key={label}
                    label={label}
                    layout="row"
                    className="contents"
                  >
                    {value || t("unknown")}
                  </ReadOnlyField>
                ))}
              {urls
                .filter(([, url]) => url && /^https?:\/\//.test(url))
                .map(([label, url]) => (
                  <ReadOnlyField
                    key={label}
                    label={label}
                    layout="row"
                    className="contents"
                  >
                    <ResourceItemAction
                      label={label ?? ""}
                      variant="ghost"
                      onClick={() => {
                        setLinkError(false);
                        void openExternalURL(url ?? "").catch(() =>
                          setLinkError(true),
                        );
                      }}
                    >
                      <ExternalLink />
                    </ResourceItemAction>
                  </ReadOnlyField>
                ))}
            </dl>
            {linkError && (
              <p role="alert" className="type-body text-destructive">
                {t("failed")}
              </p>
            )}
          </DetailSection>
        </div>
      </SurfacePatternPanel>

      {!!skills.length && (
        <DetailSection
          title={
            <>
              {t("skills")}{" "}
              <span className="text-content-caption">{skills.length}</span>
            </>
          }
        >
          <div className="space-y-3">
            <div className="grid grid-cols-1 gap-2 @[36rem]/resource-page:grid-cols-2 @[56rem]/resource-page:grid-cols-3">
              {skills.slice(0, showSkills ? undefined : 6).map((item) => (
                <ResourceListItem
                  key={`${item.target}:${item.key}`}
                  title={`/${item.name}`}
                  description={item.description}
                  descriptionLines={1}
                  headingLevel={3}
                  icon={<SkillIcon />}
                  label={t("viewSkill", { name: item.name })}
                  onClick={
                    skillContext
                      ? () =>
                          setSkill({
                            key: item.key ?? "",
                            name: item.name ?? "",
                            target: item.target || undefined,
                          })
                      : undefined
                  }
                />
              ))}
            </div>
            {skills.length > 6 && (
              <Button
                variant="ghost"
                onClick={() => setShowSkills(!showSkills)}
              >
                {t(showSkills ? "collapseSkills" : "allSkills", {
                  count: skills.length,
                })}
              </Button>
            )}
          </div>
        </DetailSection>
      )}
      {!!servers.length && (
        <DetailSection
          title={
            <>
              {t("connectors")}{" "}
              <span className="text-content-caption">{servers.length}</span>
            </>
          }
        >
          <div className="grid grid-cols-1 gap-2 @[36rem]/resource-page:grid-cols-2 @[56rem]/resource-page:grid-cols-3">
            {servers.map((server) => (
              <ResourceListItem
                key={`${server.target}:${server.key}:${server.endpoint}`}
                title={server.name ?? server.key ?? ""}
                description={server.endpoint}
                descriptionLines={1}
                headingLevel={3}
                icon={
                  <span className="bg-surface-subtle text-content-secondary grid size-10 shrink-0 place-items-center rounded-md">
                    <Cable className="size-icon-lg" />
                  </span>
                }
                label={t("viewConnector", { name: server.name ?? server.key })}
                onClick={() => setConnectorKey(server.key ?? "")}
              />
            ))}
          </div>
        </DetailSection>
      )}
      {scope === "global" && (
        <DetailSection
          title={
            <>
              {t("installedPartners")}
              {installationStatus == null && (
                <>
                  {" "}
                  <span className="text-content-caption">
                    {detail?.installations?.length ?? 0}
                  </span>
                </>
              )}
            </>
          }
        >
          <PluginInstalledPartners
            detail={detail}
            installationStatus={installationStatus}
            onUninstall={onUninstall}
            uninstalling={uninstalling}
          />
        </DetailSection>
      )}
      {connectorKey && skillContext && (
        <PluginConnectorDialog
          key={`${skillContext.pluginId ?? skillContext.catalogKey}:${skillContext.versionId ?? skillContext.archiveSHA256}:${connectorKey}`}
          context={
            skillContext.catalogKey
              ? {
                  ...skillContext,
                  pluginId: detail?.plugin?.id,
                  versionId: detail?.versions?.find(
                    (version) =>
                      version.provenance?.sourceCommit ===
                        skillContext.sourceCommit &&
                      version.provenance?.archiveSHA256 ===
                        skillContext.archiveSHA256,
                  )?.id,
                }
              : skillContext
          }
          connectorKey={connectorKey}
          name={
            servers.find((item) => item.key === connectorKey)?.name ??
            connectorKey
          }
          sourceName={pluginDisplayName(pkg, detail?.plugin?.name)}
          onClose={() => setConnectorKey(null)}
        />
      )}
      {skill && skillContext && (
        <PluginSkillDialog
          sourceName={pluginDisplayName(pkg, detail?.plugin?.name)}
          context={{ ...skillContext, target: skill.target }}
          skillKey={skill.key}
          name={skill.name}
          onClose={() => setSkill(null)}
        />
      )}
    </div>
  );
}

/** 公开连接信息只保留服务源站，不展示命令、路径或URL中的凭据。 */
function publicOrigin(value: string | undefined): string {
  if (!value) return "";
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:"
      ? url.origin
      : "";
  } catch {
    return "";
  }
}
