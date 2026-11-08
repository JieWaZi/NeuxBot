import { PluginPresentation } from "../components/plugin-presentation";
import { WakerDetailLayout } from "@/modules/waker";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Pencil, Plus, RefreshCw } from "lucide-react";
import { DetailSection } from "@/shared/ui/detail-section";
import { Button } from "@/shared/ui/button";
import {
  ResourceCatalogStatus,
  ResourceCatalogRow,
} from "@/shared/ui/resource-catalog";
import { StatusNotice } from "@/shared/ui/status-notice";
import { Switch } from "@/shared/ui/switch";
import { ResourcePageSurface, PageAction } from "@/shared/ui/resource-page";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  getPluginOptions,
  setWakerPluginEnabledMutation,
  invalidatePluginQueries,
} from "../api/plugin-queries";
import { PluginUpdateDialog } from "../components/deferred-plugin-dialogs";
import { PluginInstallDialog } from "../components/deferred-plugin-dialogs";
import { PluginRemovalDialog } from "../components/plugin-removal-dialog";
import { PluginOverview } from "../components/deferred-plugin-overview";
import { PluginDetailHeader } from "../components/plugin-detail-header";
import {
  pluginLocale,
  pluginDisplayName,
  pluginNeedsAuthorization,
  pluginHasAuthorization,
  pluginHasConfiguration,
} from "../model/plugin-view";
import { PluginSettingsDialog } from "../components/deferred-plugin-dialogs";

export function PluginDetailPage() {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const { pluginId = "", wakerId = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [install, setInstall] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [editing, setEditing] = useState(false);
  const [removing, setRemoving] = useState<{
    mode: "resource" | "installation";
    wakerId: string;
  } | null>(null);
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: {
        locale,
        wakerId,
        versionId: wakerId ? undefined : (params.get("version") ?? ""),
      },
    }),
  );
  const query = useQuery({ ...options, enabled: active && Boolean(pluginId) });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
  });
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerPluginEnabledMutation()),
    onSuccess: () => invalidatePluginQueries(client),
  });
  const detail = query.data;
  const resource = detail?.plugin;
  const unsupported = detail?.packageStatus === "unsupported";
  const relation = detail?.installations?.find(
    (item) => item.wakerId === wakerId,
  );
  const version =
    detail?.versions?.find((item) => item.id === detail.resolvedVersionId) ??
    detail?.versions?.[0];
  const pkg = version?.package;
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId ? `/wakers/${wakerId}/resources` : "/resources");
  const breadcrumbBack = resourceListBack(
    back,
    wakerId ? `/wakers/${wakerId}/resources` : "/resources",
  );
  return (
    <WakerDetailLayout
      active="resources"
      breadcrumbItems={[
        {
          label: t("plugins"),
          href: breadcrumbBack,
        },
        { label: pluginDisplayName(pkg, resource?.name) || t("plugins") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <PluginDetailHeader
          pkg={unsupported ? undefined : pkg}
          name={resource?.name}
          description={resource?.description}
          sourceKey={resource?.sourceKey}
          backAction={{ label: t("back"), onClick: () => navigate(back) }}
          status={
            resource &&
            (unsupported ? (
              <ResourceCatalogStatus
                pill
                tone="warning"
                data-status="unavailable"
              >
                {t("packageUnsupportedStatus")}
              </ResourceCatalogStatus>
            ) : relation ? (
              <ResourceCatalogStatus
                pill
                data-status={
                  !detail?.installations?.length ? "unavailable" : undefined
                }
                tone={relation.enabled ? "success" : "neutral"}
              >
                {t(relation.enabled ? "enabled" : "disabled")}
              </ResourceCatalogStatus>
            ) : (
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
            ))
          }
          actions={
            resource &&
            !unsupported && (
              <div className="grid max-w-full justify-items-end gap-2">
                <div className="flex max-w-full flex-wrap items-center gap-2">
                  {relation && (
                    <Switch
                      hitArea="comfortable"
                      checked={relation.enabled ?? false}
                      disabled={toggle.isPending}
                      aria-label={t(relation.enabled ? "disable" : "enable")}
                      onClick={() =>
                        toggle.mutate({
                          path: { wakerId, pluginId },
                          body: { enabled: !relation.enabled },
                        })
                      }
                    />
                  )}
                  {!wakerId &&
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
                  {!wakerId && (
                    <PageAction
                      appearance="catalog"
                      intent={
                        detail?.update?.available ? "secondary" : "primary"
                      }
                      onClick={() => setInstall(true)}
                    >
                      <Plus />
                      {t("installToPartners")}
                    </PageAction>
                  )}
                  {detail?.update?.available && (
                    <PageAction
                      appearance="catalog"
                      intent="primary"
                      onClick={() => setUpdating(true)}
                    >
                      <RefreshCw />
                      {t("updateAvailable")}
                    </PageAction>
                  )}
                </div>
                {toggle.error && (
                  <p role="alert" className="type-body text-destructive">
                    {localApiErrorMessage(toggle.error, t("failed"))}
                  </p>
                )}
              </div>
            )
          }
        />
        <div className="mt-6 space-y-6">
          {!detail ? (
            <QueryState
              loadingLayout="catalog-detail"
              pending={query.isPending}
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <div className="space-y-6">
              {query.isError && (
                <QueryRefreshError
                  error={query.error}
                  pending={query.isFetching}
                  onRetry={() => void query.refetch()}
                />
              )}
              {unsupported && (
                <StatusNotice role="alert">
                  {t("packageUnsupported")}
                </StatusNotice>
              )}
              {!unsupported && (
                <PluginPresentation
                  pkg={pkg}
                  detail={detail}
                  wakerId={wakerId}
                />
              )}
              <PluginOverview
                pkg={pkg}
                detail={detail}
                readmeSource={
                  version?.id
                    ? { pluginId, versionId: version.id, wakerId, locale }
                    : undefined
                }
                scope={wakerId ? "partner" : "global"}
                onEdit={
                  !wakerId && !unsupported ? () => setEditing(true) : undefined
                }
                skillContext={{ pluginId, versionId: version?.id, wakerId }}
                onUninstall={(id) =>
                  setRemoving({ mode: "installation", wakerId: id })
                }
              />
              {(relation || !wakerId) && (
                <DetailSection appearance="catalog" title={t("dangerZone")}>
                  <ResourceCatalogRow className="flex-wrap">
                    <div className="min-w-0 flex-1 basis-60 space-y-1">
                      <h3 className="type-resource-body text-resource-content font-medium">
                        {t(relation ? "uninstallTitle" : "deleteTitle")}
                      </h3>
                      <p className="text-resource-muted type-resource-caption">
                        {relation
                          ? t("uninstallHelp")
                          : t("deleteHelp", {
                              count: detail.installations?.length ?? 0,
                            })}
                      </p>
                    </div>
                    <Button
                      size="catalog"
                      variant="catalog-danger"
                      onClick={() =>
                        setRemoving({
                          mode: relation ? "installation" : "resource",
                          wakerId,
                        })
                      }
                    >
                      {t(relation ? "uninstall" : "remove")}
                    </Button>
                  </ResourceCatalogRow>
                </DetailSection>
              )}
            </div>
          )}
        </div>
        {editing && detail && !unsupported && (
          <PluginSettingsDialog
            key={`${pluginId}:${version?.id}`}
            detail={detail}
            versionId={version?.id}
            onClose={() => setEditing(false)}
          />
        )}
        {updating && detail && !unsupported && (
          <PluginUpdateDialog
            detail={detail}
            wakerId={wakerId}
            onClose={() => setUpdating(false)}
          />
        )}
        {install && detail && !unsupported && (
          <PluginInstallDialog
            detail={detail}
            onClose={() => setInstall(false)}
          />
        )}
        {removing && detail && (
          <PluginRemovalDialog
            detail={detail}
            {...removing}
            onClose={() => setRemoving(null)}
            onRemoved={() => {
              if (removing.mode === "resource" || removing.wakerId === wakerId)
                navigate(back);
            }}
          />
        )}
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
