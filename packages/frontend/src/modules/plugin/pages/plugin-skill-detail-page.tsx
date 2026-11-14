import { resourceListBack } from "@/shared/lib/resource-navigation";
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
  SkillDetailHeader,
  SkillPresentation,
  SkillInstructionsPanel,
  SkillMetadataCard,
} from "@/modules/skill";
import { WakerDetailLayout } from "@/modules/waker";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  getPluginOptions,
  getPluginSkillOptions,
  getCatalogPluginSkillOptions,
} from "../api/plugin-queries";
import { pluginDisplayName } from "../model/plugin-view";

/** 两个入口均读取明确的插件快照，不从当前安装版本推断技能正文。 */
export function PluginSkillDetailPage() {
  const { t } = useTranslation("skill");
  const {
    pluginId = "",
    catalogKey = "",
    skillKey = "",
    wakerId = "",
  } = useParams();
  const [params] = useSearchParams();
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
  const listPath = wakerId
    ? `/wakers/${wakerId}/resources?category=plugins`
    : "/resources?category=plugins";
  const base = wakerId
    ? `/wakers/${encodeURIComponent(wakerId)}`
    : "/resources";
  const parentPath = catalogKey
    ? `/resources/plugins/catalog/${encodeURIComponent(catalogKey)}`
    : `${base}/plugins/${encodeURIComponent(pluginId)}`;
  const parentURL = `${parentPath}${versionId ? `?version=${encodeURIComponent(versionId)}` : ""}`;
  const listBack = resourceListBack(
    state?.pluginResourceBack ?? state?.resourceBack,
    listPath,
  );
  const back =
    resourceListBack(state?.resourceBack, parentURL) === state?.resourceBack
      ? state.resourceBack
      : listBack;
  const installedOptions = getPluginSkillOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { versionId, key: skillKey, wakerId: wakerId || undefined },
    }),
  );
  const installed = useQuery({
    ...installedOptions,
    enabled:
      active && !catalogKey && Boolean(pluginId && skillKey && versionId),
  });
  const catalogOptions = getCatalogPluginSkillOptions(
    withLocalErrorSurface({
      path: { key: catalogKey },
      query: { skillKey, sourceCommit, archiveSHA256 },
    }),
  );
  const catalog = useQuery({
    ...catalogOptions,
    enabled:
      active &&
      Boolean(catalogKey && skillKey && sourceCommit && archiveSHA256),
  });
  const query = catalogKey ? catalog : installed;
  const parentOptions = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { wakerId, versionId },
    }),
  );
  const parent = useQuery({
    ...parentOptions,
    enabled: active && !catalogKey && Boolean(pluginId && versionId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [
      installedOptions.queryKey,
      catalogOptions.queryKey,
      parentOptions.queryKey,
    ],
    active,
  });
  const version = parent.data?.versions?.find((item) => item.id === versionId);
  const source = catalogKey
    ? state?.sourceName || catalogKey
    : pluginDisplayName(version?.package, parent.data?.plugin?.name);
  const sourceVersion =
    query.data?.version?.version ||
    version?.version ||
    sourceCommit.slice(0, 12);
  const skill = query.data?.skill;
  const hasSnapshot = catalogKey
    ? Boolean(sourceCommit && archiveSHA256)
    : Boolean(versionId);
  return (
    <WakerDetailLayout
      active="plugins"
      breadcrumbItems={[
        { label: t("plugin:plugins"), href: listBack },
        {
          label: source || t("plugin:plugins"),
          href: parentURL,
          state: { resourceBack: state?.pluginResourceBack ?? listBack },
        },
        { label: skill?.name || t("title") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <SkillDetailHeader
          title={skill?.name ?? t("title")}
          description={skill?.description}
          backAction={{
            label: t("detail.backToList"),
            onClick: () =>
              navigate(back, {
                state: { resourceBack: state?.pluginResourceBack ?? listBack },
              }),
          }}
        />
        <main className="mt-6 min-w-0 space-y-6">
          {query.data ? (
            <>
              {query.isError && (
                <QueryRefreshError
                  error={query.error}
                  pending={query.isFetching}
                  onRetry={() => void query.refetch()}
                />
              )}
              <SkillPresentation
                name={skill?.name ?? ""}
                context={
                  catalogKey
                    ? { kind: "preview" }
                    : { kind: "plugin", pluginId, versionId }
                }
                wakerId={wakerId}
                usable={Boolean(
                  wakerId &&
                  !parent.isError &&
                  !query.isError &&
                  parent.data?.installations?.some(
                    (item) =>
                      item.wakerId === wakerId &&
                      item.versionId === versionId &&
                      item.enabled,
                  ),
                )}
              />
              <SkillInstructionsPanel
                content={query.data.content ?? ""}
                name={skill?.name}
              />
              <SkillMetadataCard
                description={skill?.description}
                source={
                  <Link
                    className="focus-visible:ring-ring rounded-xs outline-none hover:underline focus-visible:ring-2"
                    to={parentURL}
                    state={{
                      resourceBack: state?.pluginResourceBack ?? listBack,
                    }}
                  >
                    {source}
                  </Link>
                }
                version={sourceVersion}
              />
            </>
          ) : (
            <QueryState
              loadingLayout="catalog-detail"
              pending={query.isPending && hasSnapshot}
              error={query.error}
              emptyLabel={t("detail.notFound")}
              onRetry={() => void query.refetch()}
            />
          )}
        </main>
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
