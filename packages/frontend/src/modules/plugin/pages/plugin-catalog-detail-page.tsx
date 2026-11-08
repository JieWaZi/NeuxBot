import { PluginPresentation } from "../components/plugin-presentation";
import { WakerDetailLayout } from "@/modules/waker";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate, useParams, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ResourcePageSurface, PageAction } from "@/shared/ui/resource-page";
import { ResourceCatalogStatus } from "@/shared/ui/resource-catalog";
import { Plus, Pencil } from "lucide-react";
import { PluginDetailHeader } from "../components/plugin-detail-header";
import { PluginSettingsDialog } from "../components/deferred-plugin-dialogs";
import { PluginOverview } from "../components/deferred-plugin-overview";
import {
  pluginNeedsAuthorization,
  pluginHasAuthorization,
  pluginHasConfiguration,
  pluginLocale,
  pluginDisplayName,
} from "../model/plugin-view";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  getCatalogPluginOptions,
  listPluginsOptions,
} from "../api/plugin-queries";
import { PluginRemovalDialog } from "../components/plugin-removal-dialog";
import { PluginInstallDialog } from "../components/deferred-plugin-dialogs";
export function PluginCatalogDetailPage() {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const [removingPartner, setRemovingPartner] = useState("");
  const { catalogKey = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    "/resources";
  const breadcrumbBack = resourceListBack(back, "/resources");
  const [install, setInstall] = useState(false);
  const [editing, setEditing] = useState(false);
  const catalogOptions = getCatalogPluginOptions(
    withLocalErrorSurface({ path: { key: catalogKey }, query: { locale } }),
  );
  const active = useRouteSceneActive();
  const query = useQuery({ ...catalogOptions, enabled: active });
  const entry = query.data;
  const pkg = entry?.package;
  const sourceKey = entry?.sourceKey ?? `catalog:${catalogKey}`;
  const installedOptions = listPluginsOptions(
    withLocalErrorSurface({
      query: { query: sourceKey, pageNum: 1, pageSize: 100 },
    }),
  );
  const installed = useQuery({
    ...installedOptions,
    enabled: active && Boolean(entry),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [catalogOptions.queryKey, installedOptions.queryKey],
    active,
  });
  const detail = installed.data?.resources?.find(
    (item) => item.plugin?.sourceKey === sourceKey,
  );

  return (
    <WakerDetailLayout
      active="resources"
      breadcrumbItems={[
        { label: t("plugins"), href: breadcrumbBack },
        { label: pluginDisplayName(pkg) || t("plugins") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <PluginDetailHeader
          pkg={pkg}
          sourceKey={sourceKey}
          backAction={{ label: t("back"), onClick: () => navigate(back) }}
          status={
            entry &&
            installed.data && (
              <ResourceCatalogStatus
                pill
                data-status={
                  !detail?.installations?.length ? "unavailable" : undefined
                }
                tone={
                  pluginNeedsAuthorization(pkg, detail) ? "warning" : "neutral"
                }
              >
                {t(
                  pluginNeedsAuthorization(pkg, detail)
                    ? "requiresAuthorization"
                    : detail?.installations?.length
                      ? "installed"
                      : "notInstalledStatus",
                )}
              </ResourceCatalogStatus>
            )
          }
          actions={
            entry && (
              <>
                {detail?.plugin?.id &&
                  !pluginHasAuthorization(pkg) &&
                  !pluginHasConfiguration(pkg) && (
                    <PageAction
                      appearance="catalog"
                      intent="edit"
                      label={t("edit")}
                      onClick={() => setEditing(true)}
                    >
                      <Pencil />
                    </PageAction>
                  )}
                <PageAction
                  appearance="catalog"
                  intent="primary"
                  disabled={installed.isPending || installed.isError}
                  onClick={() => setInstall(true)}
                >
                  <Plus />
                  {t(
                    pluginNeedsAuthorization(pkg, detail)
                      ? "authorize"
                      : "installToPartners",
                  )}
                </PageAction>
              </>
            )
          }
        />
        <div className="mt-6 space-y-6">
          {entry && query.isError && (
            <QueryRefreshError
              error={query.error}
              pending={query.isFetching}
              onRetry={() => void query.refetch()}
            />
          )}
          {entry && installed.data && installed.isError && (
            <QueryRefreshError
              error={installed.error}
              pending={installed.isFetching}
              onRetry={() => void installed.refetch()}
            />
          )}
          {!entry ? (
            <QueryState
              loadingLayout="catalog-detail"
              pending={query.isPending}
              error={query.error}
              emptyLabel={t("noResults")}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <>
              <PluginPresentation pkg={pkg} detail={detail} catalog={entry} />
              <PluginOverview
                pkg={pkg}
                sourceKey={sourceKey}
                readmeSource={{
                  locale,
                  catalogKey,
                  sourceCommit: entry.sourceCommit,
                  archiveSHA256: entry.archiveSHA256,
                }}
                skillContext={{
                  catalogKey,
                  sourceCommit: entry.sourceCommit,
                  archiveSHA256: entry.archiveSHA256,
                }}
                detail={detail}
                onEdit={() => setEditing(true)}
                onUninstall={setRemovingPartner}
                installationStatus={
                  !installed.data ? (
                    <QueryState
                      loadingLayout="rows"
                      pending={installed.isPending}
                      error={installed.error}
                      onRetry={() => void installed.refetch()}
                    />
                  ) : undefined
                }
              />
            </>
          )}
        </div>
        {editing && detail && (
          <PluginSettingsDialog
            key={`${detail.plugin?.id}:${detail.resolvedVersionId}`}
            detail={detail}
            versionId={detail.resolvedVersionId}
            onClose={() => setEditing(false)}
          />
        )}
        {removingPartner && detail && (
          <PluginRemovalDialog
            detail={detail}
            mode="installation"
            wakerId={removingPartner}
            onClose={() => setRemovingPartner("")}
          />
        )}
        {install && entry && (
          <PluginInstallDialog
            catalog={entry}
            detail={detail}
            authorizeOnly={pluginNeedsAuthorization(pkg, detail)}
            onClose={() => setInstall(false)}
          />
        )}
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
