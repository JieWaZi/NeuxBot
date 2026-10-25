import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Plus, Trash2, Pencil, RefreshCw } from "lucide-react";
import { Switch } from "@/shared/ui/switch";
import { ResourcePageSurface, PageAction } from "@/shared/ui/resource-page";
import { QueryState } from "@/shared/ui/query-state";
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
import { pluginLocale } from "../model/plugin-view";
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
  const relation = detail?.installations?.find(
    (item) => item.wakerId === wakerId,
  );
  const version =
    detail?.versions?.find((item) => item.id === detail.resolvedVersionId) ??
    detail?.versions?.[0];
  const pkg = version?.package;
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId ? `/wakers/${wakerId}/resources` : "/resources?view=mine");
  return (
    <main
      className="h-full min-w-0 overflow-hidden"
      data-partner-section={wakerId ? "resources" : undefined}
    >
      <ResourcePageSurface>
        <PluginDetailHeader
          pkg={pkg}
          name={resource?.name}
          description={resource?.description}
          sourceKey={resource?.sourceKey}
          backAction={{ label: t("back"), onClick: () => navigate(back) }}
          actions={
            resource && (
              <div className="grid max-w-full justify-items-end gap-2">
                <div className="flex max-w-full flex-wrap items-center gap-2">
                  {relation && (
                    <>
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
                      <span className="text-content-secondary type-body">
                        {t(relation.enabled ? "enabled" : "disabled")}
                      </span>
                    </>
                  )}
                  {!wakerId && (
                    <PageAction
                      intent="edit"
                      label={t("edit")}
                      onClick={() => setEditing(true)}
                    >
                      <Pencil />
                    </PageAction>
                  )}
                  {relation ? (
                    <PageAction
                      intent="delete"
                      label={t("uninstall")}
                      onClick={() =>
                        setRemoving({ mode: "installation", wakerId })
                      }
                    >
                      <Trash2 />
                    </PageAction>
                  ) : (
                    (!wakerId || resource.ownerWakerId === wakerId) && (
                      <PageAction
                        intent="delete"
                        label={t("remove")}
                        onClick={() =>
                          setRemoving({ mode: "resource", wakerId })
                        }
                      >
                        <Trash2 />
                      </PageAction>
                    )
                  )}
                  {!wakerId && (
                    <PageAction
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
        <div className="mt-8 space-y-6">
          {!detail ? (
            <QueryState
              loadingLayout="detail"
              pending={query.isPending}
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <div className="space-y-6">
              {query.isError && (
                <QueryState
                  error={query.error}
                  onRetry={() => void query.refetch()}
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
                skillContext={{ pluginId, versionId: version?.id, wakerId }}
                onUninstall={(id) =>
                  setRemoving({ mode: "installation", wakerId: id })
                }
              />
            </div>
          )}
        </div>
        {editing && detail && (
          <PluginSettingsDialog
            key={`${pluginId}:${version?.id}`}
            detail={detail}
            versionId={version?.id}
            onClose={() => setEditing(false)}
          />
        )}
        {updating && detail && (
          <PluginUpdateDialog
            detail={detail}
            wakerId={wakerId}
            onClose={() => setUpdating(false)}
          />
        )}
        {install && detail && (
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
    </main>
  );
}
