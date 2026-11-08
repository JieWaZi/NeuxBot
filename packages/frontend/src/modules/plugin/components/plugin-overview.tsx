import { PluginInstalledPartners } from "./plugin-installed-partners";
import { useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Cable, ExternalLink, Settings2, Pencil } from "lucide-react";
import { useTranslation } from "react-i18next";
import { openExternalURL } from "@/shared/platform";
import { MarkdownContent } from "@/shared/ui/markdown-content";
import { DetailSection } from "@/shared/ui/detail-section";
import {
  ResourceCatalogRow,
  ResourceCatalogRows,
  ResourceCatalogFields,
  ResourceCatalogField,
} from "@/shared/ui/resource-catalog";
import { PluginReadme } from "./plugin-readme";
import type { ReadmeSource } from "./plugin-readme";
import type { PluginComponentContext } from "../model/plugin-components";
import { Button } from "@/shared/ui/button";
import { Sparkles } from "lucide-react";
import {
  pluginExplanation,
  pluginDisplayName,
  pluginLocale,
  pluginHasAuthorization,
  pluginHasConfiguration,
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
  onEdit,
  uninstalling,
  readmeSource,
}: {
  pkg?: PluginCatalogEntry["package"];
  sourceKey?: string;
  scope?: "global" | "partner";
  detail?: PluginDetail;
  installationStatus?: ReactNode;
  skillContext?: PluginComponentContext;
  onUninstall?: (id: string) => void;
  onEdit?: () => void;
  uninstalling?: boolean;
  readmeSource?: ReadmeSource;
}) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const [linkError, setLinkError] = useState(false);
  const manifest = pkg?.manifest;
  const navigate = useNavigate();
  const location = useLocation();

  const allSkills = pkg?.skills ?? [];
  const allServers = (pkg?.servers ?? []).map((item) => ({
    key: item.key,
    name: item.key,
  }));
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
    [
      t("common:resourceCategory.label"),
      detail?.plugin?.categories?.join("、"),
    ],
    [t("upstreamVersion"), upstreamSource?.tag || upstreamSource?.version],
    [t("license"), upstreamSource?.license || manifest?.license],
  ];
  const repository = upstreamSource?.repository || manifest?.repository;
  const candidates = [
    [t("website"), presentation?.websiteURL || manifest?.homepage],
    [t("support"), presentation?.supportURL],
    [t("repository"), repository],
    [t("privacy"), presentation?.privacyPolicyURL],
    [t("terms"), presentation?.termsOfServiceURL],
  ];
  const sourceTarget = publicLinkTarget(repository);
  const seenURLs = new Set<string>();
  const urls = candidates.filter(([label, url]) => {
    const target = publicLinkTarget(url);
    if (
      !target ||
      seenURLs.has(target) ||
      (target === sourceTarget && label !== t("repository"))
    )
      return false;
    seenURLs.add(target);
    return true;
  });
  const setup = presentation?.setup;
  const instructions =
    setup?.instructions?.[locale]?.trim() || setup?.instructions?.en?.trim();

  const canManage =
    scope === "global" &&
    Boolean(detail?.plugin?.id && onEdit) &&
    (pluginHasAuthorization(pkg) || pluginHasConfiguration(pkg));
  const hasConnections =
    canManage || servers.length > 0 || Boolean(instructions);
  const openComponent = (kind: "skills" | "connectors", key: string) => {
    if (!skillContext || !key) return;
    const base = skillContext.wakerId
      ? `/wakers/${encodeURIComponent(skillContext.wakerId)}`
      : "/resources";
    const query = new URLSearchParams();
    let path: string;
    if (skillContext.catalogKey) {
      path = `/resources/plugins/catalog/${encodeURIComponent(skillContext.catalogKey)}/${kind}/${encodeURIComponent(key)}`;
      query.set("sourceCommit", skillContext.sourceCommit ?? "");
      query.set("archiveSHA256", skillContext.archiveSHA256 ?? "");
      // 只有与目录摘要一致的本地版本能提供显式连接探测。
      if (kind === "connectors") {
        const installedVersion = detail?.versions?.find(
          (version) =>
            version.provenance?.sourceCommit === skillContext.sourceCommit &&
            version.provenance?.archiveSHA256 === skillContext.archiveSHA256,
        );
        if (detail?.plugin?.id && installedVersion?.id) {
          query.set("pluginId", detail.plugin.id);
          query.set("version", installedVersion.id);
        }
      }
    } else {
      path = `${base}/plugins/${encodeURIComponent(skillContext.pluginId ?? "")}/${kind}/${encodeURIComponent(key)}`;
      query.set("version", skillContext.versionId ?? "");
    }
    navigate(`${path}?${query}`, {
      state: {
        resourceBack: location.pathname + location.search,
        pluginResourceBack: (location.state as { resourceBack?: string } | null)
          ?.resourceBack,
        sourceName: pluginDisplayName(pkg, detail?.plugin?.name),
      },
    });
  };
  // 旧快照仍可管理真实安装，但其组件和包内说明不能被当成可用能力。
  if (detail?.packageStatus === "unsupported" && !skillContext?.catalogKey) {
    return scope === "global" &&
      (installationStatus != null || detail.installations?.length) ? (
      <DetailSection appearance="catalog" title={t("installedPartners")}>
        <PluginInstalledPartners
          detail={detail}
          installationStatus={installationStatus}
          onUninstall={onUninstall}
          uninstalling={uninstalling}
        />
      </DetailSection>
    ) : null;
  }
  return (
    <div className="space-y-6">
      {scope === "global" &&
        (installationStatus != null ||
          Boolean(detail?.installations?.length)) && (
          <DetailSection appearance="catalog" title={t("installedPartners")}>
            <PluginInstalledPartners
              detail={detail}
              installationStatus={installationStatus}
              onUninstall={onUninstall}
              uninstalling={uninstalling}
            />
          </DetailSection>
        )}
      {hasConnections && (
        <DetailSection appearance="catalog" title={t("connectionAndSettings")}>
          {(canManage || servers.length > 0) && (
            <ResourceCatalogRows>
              {canManage && (
                <ResourceCatalogRow>
                  <span className="text-resource-secondary size-resource-icon grid shrink-0 place-items-center">
                    <Settings2 className="size-icon-lg" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <h3 className="type-resource-body text-resource-content truncate font-medium">
                      {t(
                        pluginHasAuthorization(pkg)
                          ? "authentication"
                          : "configuration",
                      )}
                    </h3>
                    {pluginHasAuthorization(pkg) && (
                      <p className="type-resource-caption text-resource-muted">
                        {detail?.account?.checkedAt
                          ? detail?.account.available
                            ? t("sharedIdentity", {
                                name:
                                  detail?.account.name || detail?.account.id,
                              })
                            : t("requiresAuthorization")
                          : t("accountUnchecked")}
                      </p>
                    )}
                  </div>
                  <Button
                    size="catalog"
                    variant="catalog-outline"
                    aria-label={t("edit")}
                    onClick={onEdit}
                  >
                    <Pencil />
                    {t("manage")}
                  </Button>
                </ResourceCatalogRow>
              )}
              {servers.map((server) => (
                <ResourceCatalogRow asChild key={server.key}>
                  <button
                    type="button"
                    className="hover:bg-resource-hover focus-visible:ring-ring w-full text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
                    aria-label={t("viewConnector", {
                      name: server.name ?? server.key,
                    })}
                    disabled={!skillContext}
                    onClick={() =>
                      openComponent("connectors", server.key ?? "")
                    }
                  >
                    <span className="text-resource-secondary size-resource-icon grid shrink-0 place-items-center">
                      <Cable className="size-icon-lg" />
                    </span>
                    <div className="min-w-0 flex-1 space-y-1">
                      <h3
                        className="type-resource-body text-resource-content truncate font-medium"
                        title={server.name ?? server.key}
                      >
                        {server.name ?? server.key ?? ""}
                      </h3>
                    </div>
                  </button>
                </ResourceCatalogRow>
              ))}
            </ResourceCatalogRows>
          )}
          {instructions && (
            <div className="grid gap-3 px-4 py-3">
              <MarkdownContent content={instructions} appearance="catalog" />
            </div>
          )}
        </DetailSection>
      )}
      {!!skills.length && (
        <DetailSection appearance="catalog" title={t("skills")}>
          <ResourceCatalogRows>
            {skills.map((item) => (
              <ResourceCatalogRow asChild key={item.key}>
                <button
                  type="button"
                  className="hover:bg-resource-hover focus-visible:ring-ring w-full text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
                  aria-label={t("viewSkill", { name: item.name })}
                  disabled={!skillContext}
                  onClick={() => openComponent("skills", item.key ?? "")}
                >
                  <span className="text-resource-secondary size-resource-icon grid shrink-0 place-items-center">
                    <Sparkles className="size-icon-lg" />
                  </span>
                  <div className="min-w-0 flex-1 space-y-1">
                    <h3
                      className="type-resource-body text-resource-content truncate font-medium"
                      title={item.name}
                    >
                      {item.name}
                    </h3>
                    {item.description && (
                      <p
                        className="type-resource-caption text-resource-muted truncate"
                        title={item.description}
                      >
                        {item.description}
                      </p>
                    )}
                  </div>
                </button>
              </ResourceCatalogRow>
            ))}
          </ResourceCatalogRows>
        </DetailSection>
      )}
      <PluginReadme
        appearance="catalog"
        key={`${locale}:${resourceSource ?? ""}:${readmeVersion?.id ?? manifest?.version ?? ""}:${explanation}`}
        content={explanation}
        status={pkg?.readmeStatus ?? (pkg?.readme ? "available" : "missing")}
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
      >
        {(preview, action) => (
          <DetailSection
            appearance="catalog"
            title={t("information")}
            actions={action}
          >
            <div className="grid gap-3 px-4 py-3">
              {preview}
              {(developer ||
                info.some(([, value]) => value) ||
                urls.some(([, url]) => url && /^https?:\/\//.test(url))) && (
                <ResourceCatalogFields>
                  {developer && (
                    <ResourceCatalogField label={t("developer")}>
                      {developer}
                    </ResourceCatalogField>
                  )}
                  {info
                    .filter(([, value]) => value)
                    .map(([label, value]) => (
                      <ResourceCatalogField key={label} label={label}>
                        {value}
                      </ResourceCatalogField>
                    ))}
                  {urls
                    .filter(([, url]) => url && /^https?:\/\//.test(url))
                    .map(([label, url]) => (
                      <ResourceCatalogField key={label} label={label}>
                        <a
                          href={url}
                          className="text-resource-content focus-visible:ring-ring inline-flex max-w-full items-center gap-1 rounded-xs outline-none hover:underline focus-visible:ring-2"
                          aria-label={label}
                          onClick={(event) => {
                            event.preventDefault();
                            setLinkError(false);
                            void openExternalURL(url ?? "").catch(() =>
                              setLinkError(true),
                            );
                          }}
                        >
                          {publicLinkName(url ?? "")}
                          <ExternalLink className="size-icon-sm shrink-0" />
                        </a>
                      </ResourceCatalogField>
                    ))}
                </ResourceCatalogFields>
              )}
              {linkError && (
                <p role="alert" className="type-resource-body text-destructive">
                  {t("failed")}
                </p>
              )}
            </div>
          </DetailSection>
        )}
      </PluginReadme>
    </div>
  );
}

/** 仓库保留 owner/name，其余链接保留可辨认的公开页面路径。 */
function publicLinkName(value: string): string {
  try {
    const url = new URL(value);
    const path = url.pathname.replace(/\/$/, "").replace(/\.git$/, "");
    if (
      ["github.com", "gitlab.com", "bitbucket.org"].includes(url.hostname) &&
      path
    )
      return path.replace(/^\//, "");
    return `${url.hostname}${path}`;
  } catch {
    return value;
  }
}

/** 公开目标去掉跟踪参数与尾斜线，同一源码只保留仓库字段。 */
function publicLinkTarget(value: string | undefined): string {
  if (!value) return "";
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" && url.protocol !== "http:") return "";
    return `${url.origin}${url.pathname.replace(/\/$/, "").replace(/\.git$/, "")}`;
  } catch {
    return "";
  }
}
