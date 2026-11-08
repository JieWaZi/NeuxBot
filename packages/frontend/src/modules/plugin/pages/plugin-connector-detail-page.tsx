import { useQuery } from "@tanstack/react-query";
import {
  Link,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  ConnectorDetails,
  ConnectorDetailHeader,
  ConnectorPresentation,
  connectorSelectableInComposer,
} from "@/modules/connector";
import { WakerDetailLayout } from "@/modules/waker";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { DetailSection } from "@/shared/ui/detail-section";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { getPluginOptions } from "../api/plugin-queries";
import { PluginConnectorTestAction } from "../components/plugin-connector-test-action";
import { PluginInstalledPartners } from "../components/plugin-installed-partners";
import { usePluginConnector } from "../api/use-plugin-connector";
import { pluginDisplayName } from "../model/plugin-view";

/** 独立页面固定插件版本或目录摘要，组件的维护仍归属于插件。 */
export function PluginConnectorDetailPage() {
  const { t } = useTranslation("connector");
  const {
    pluginId: routePluginId = "",
    catalogKey = "",
    connectorKey = "",
    wakerId = "",
  } = useParams();
  const [params] = useSearchParams();
  const pluginId =
    routePluginId || (catalogKey ? (params.get("pluginId") ?? "") : "");
  const versionId = params.get("version") ?? "";
  const sourceCommit = params.get("sourceCommit") ?? "";
  const archiveSHA256 = params.get("archiveSHA256") ?? "";
  const active = useRouteSceneActive();
  const navigate = useNavigate();
  const location = useLocation();
  const state = location.state as {
    resourceBack?: string;
    pluginResourceBack?: string;
    sourceName?: string;
  } | null;
  const base = wakerId
    ? `/wakers/${encodeURIComponent(wakerId)}`
    : "/resources";
  const parentPath = catalogKey
    ? `/resources/plugins/catalog/${encodeURIComponent(catalogKey)}`
    : `${base}/plugins/${encodeURIComponent(pluginId)}`;
  const parentURL = `${parentPath}${!catalogKey && versionId ? `?version=${encodeURIComponent(versionId)}` : ""}`;
  const resourceList = wakerId ? `${base}/resources` : "/resources";
  const pluginList = `${resourceList}?category=plugins`;
  const fromParent =
    resourceListBack(state?.resourceBack, parentURL) === state?.resourceBack;
  const listBack = resourceListBack(
    state?.pluginResourceBack ?? state?.resourceBack,
    pluginList,
  );
  const back =
    fromParent && state?.resourceBack ? state.resourceBack : listBack;
  const hasSnapshot = catalogKey
    ? Boolean(sourceCommit && archiveSHA256)
    : Boolean(versionId);
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { versionId, wakerId },
    }),
  );
  const parent = useQuery({
    ...options,
    enabled: active && Boolean(pluginId && versionId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
  });
  const version = parent.data?.versions?.find((item) => item.id === versionId);
  const matchingSnapshot =
    !catalogKey ||
    Boolean(
      version?.provenance?.sourceCommit === sourceCommit &&
      version?.provenance?.archiveSHA256 === archiveSHA256,
    );
  const context = {
    pluginId: matchingSnapshot ? pluginId : "",
    catalogKey,
    versionId: matchingSnapshot ? versionId : "",
    sourceCommit,
    archiveSHA256,
    wakerId,
  };
  const query = usePluginConnector(
    context,
    connectorKey,
    active && hasSnapshot,
  );
  const source =
    (matchingSnapshot
      ? pluginDisplayName(version?.package, parent.data?.plugin?.name)
      : "") || (catalogKey ? state?.sourceName || catalogKey : "");
  const sourceVersion =
    (matchingSnapshot ? version?.version : "") ||
    (catalogKey ? sourceCommit.slice(0, 12) : "");
  const supportedSnapshot = Boolean(
    matchingSnapshot &&
    version &&
    parent.data?.packageStatus === "available" &&
    !parent.isError,
  );
  const installations =
    matchingSnapshot && version
      ? (parent.data?.installations ?? []).filter(
          (item) =>
            item.versionId === versionId &&
            (!wakerId || item.wakerId === wakerId),
        )
      : [];
  const currentInstallation = installations.find(
    (item) => item.wakerId === wakerId,
  );
  const presentationIdentity = JSON.stringify([
    pluginId,
    versionId,
    catalogKey,
    connectorKey,
    sourceCommit,
    archiveSHA256,
    wakerId,
  ]);
  const sourceLink = (
    <Link
      to={parentURL}
      state={{ resourceBack: listBack }}
      className="catalog-focus rounded-xs hover:underline"
    >
      {source || t("plugin:plugins")}
    </Link>
  );
  return (
    <WakerDetailLayout
      active="plugins"
      breadcrumbItems={[
        {
          label: t("plugin:plugins"),
          href: listBack,
        },
        {
          label: source || t("plugin:plugins"),
          href: parentURL,
          state: { resourceBack: listBack },
        },
        { label: query.data?.connector?.name || t("title") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <ConnectorDetailHeader
          title={query.data?.connector?.name ?? (connectorKey || t("title"))}
          description={query.data?.connector?.description}
          transport={query.data?.connector?.transport}
          backAction={{
            label: t("common:backToList"),
            onClick: () =>
              navigate(back, { state: { resourceBack: listBack } }),
          }}
        />
        <main className="mt-6 min-w-0 space-y-6">
          {query.data?.connector ? (
            <>
              {query.isError && (
                <QueryRefreshError
                  error={query.error}
                  pending={query.isFetching}
                  onRetry={() => void query.refetch()}
                />
              )}
              <ConnectorPresentation
                key={presentationIdentity}
                name={query.data.connector.name ?? connectorKey}
                context={
                  supportedSnapshot
                    ? { kind: "plugin", pluginId, versionId }
                    : { kind: "preview" }
                }
                wakerId={wakerId}
                usable={Boolean(
                  supportedSnapshot &&
                  currentInstallation?.enabled === true &&
                  !query.isError,
                )}
                systemManaged={
                  !connectorSelectableInComposer(
                    query.data.connector.activationPolicy,
                  )
                }
              />
              {installations.length > 0 && (
                <DetailSection
                  appearance="catalog"
                  title={t(
                    wakerId
                      ? "detail.currentPartner"
                      : "detail.installedPartners",
                  )}
                >
                  <PluginInstalledPartners
                    detail={{ ...parent.data, installations }}
                  />
                </DetailSection>
              )}
              <ConnectorDetails
                connector={query.data.connector}
                testing={query.testing}
                testError={query.testError}
                testAction={<PluginConnectorTestAction probe={query} />}
                source={sourceLink}
                version={sourceVersion}
                managed
              />
            </>
          ) : (
            <QueryState
              loadingLayout="catalog-detail"
              pending={query.isPending && hasSnapshot}
              error={query.error}
              emptyLabel={t("detail.notFound")}
              onRetry={() => {
                if (hasSnapshot) void query.refetch();
              }}
            />
          )}
        </main>
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
