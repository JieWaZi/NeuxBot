import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate, useParams, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ResourcePageSurface, PageAction } from "@/shared/ui/resource-page";
import { Plus, Pencil } from "lucide-react";
import { PluginDetailHeader } from "../components/plugin-detail-header";
import { PluginSettingsDialog } from "../components/deferred-plugin-dialogs";
import { PluginOverview } from "../components/deferred-plugin-overview";
import { pluginNeedsAuthorization, pluginLocale } from "../model/plugin-view";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { QueryState } from "@/shared/ui/query-state";
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
  const [install, setInstall] = useState(false);
  const [editing, setEditing] = useState(false);
  const catalogOptions = getCatalogPluginOptions(
    withLocalErrorSurface({ path: { key: catalogKey }, query: { locale } }),
  );
  const query = useQuery(catalogOptions);
  const entry = query.data;
  const pkg = entry?.package;
  const sourceKey = entry?.sourceKey ?? `catalog:${catalogKey}`;
  const active = useRouteSceneActive();
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
    <ResourcePageSurface>
      <PluginDetailHeader
        pkg={pkg}
        sourceKey={sourceKey}
        backAction={{ label: t("back"), onClick: () => navigate(back) }}
        actions={
          entry && (
            <>
              {detail?.plugin?.id && (
                <PageAction
                  intent="edit"
                  label={t("edit")}
                  onClick={() => setEditing(true)}
                >
                  <Pencil />
                </PageAction>
              )}
              <PageAction
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
      <div className="mt-8 space-y-6">
        {entry && query.isError && (
          <QueryState
            error={query.error}
            onRetry={() => void query.refetch()}
          />
        )}
        {!entry ? (
          <QueryState
            loadingLayout="detail"
            pending={query.isPending}
            error={query.error}
            emptyLabel={t("noResults")}
            onRetry={() => void query.refetch()}
          />
        ) : (
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
            onUninstall={setRemovingPartner}
            installationStatus={
              installed.isPending || installed.isError ? (
                <QueryState
                  loadingLayout="content"
                  pending={installed.isPending}
                  error={installed.error}
                  onRetry={() => void installed.refetch()}
                />
              ) : undefined
            }
          />
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
  );
}
