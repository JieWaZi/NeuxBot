import { useQuery } from "@tanstack/react-query";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  SkillIcon,
  SkillInstructionsPanel,
  SkillMetadataCard,
} from "@/modules/skill";
import { WakerDetailLayout } from "@/modules/waker";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { PageHeader } from "@/shared/ui/page-header";
import { QueryState } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { getPluginOptions, getPluginSkillOptions } from "../api/plugin-queries";
import { pluginDisplayName } from "../model/plugin-view";

/** 资源列表中的插件技能使用独立只读页，正文始终读取所选插件版本。 */
export function PluginSkillDetailPage() {
  const { t } = useTranslation("skill");
  const { pluginId = "", skillKey = "", wakerId = "" } = useParams();
  const [params] = useSearchParams();
  const versionId = params.get("version") ?? "";
  const active = useRouteSceneActive();
  const navigate = useNavigate();
  const location = useLocation();
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId
      ? `/wakers/${wakerId}/resources?category=skills`
      : "/resources?category=skills");
  const options = getPluginSkillOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { versionId, key: skillKey, wakerId: wakerId || undefined },
    }),
  );
  const query = useQuery({
    ...options,
    enabled: active && Boolean(pluginId && skillKey && versionId),
  });
  const parentOptions = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { wakerId, versionId },
    }),
  );
  const parent = useQuery({
    ...parentOptions,
    enabled: active && Boolean(pluginId && versionId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey, parentOptions.queryKey],
    active,
  });
  const version = parent.data?.versions?.find((item) => item.id === versionId);
  const source = pluginDisplayName(version?.package, parent.data?.plugin?.name);
  const skill = query.data?.skill;
  return (
    <WakerDetailLayout active="skills">
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          visual={<SkillIcon />}
          title={skill?.name ?? t("title")}
          backAction={{
            label: t("detail.backToList"),
            onClick: () => navigate(back),
          }}
        />
        {query.data ? (
          <>
            <SkillMetadataCard
              name={skill?.name ?? ""}
              description={skill?.description ?? ""}
              source={source}
            />
            <section className="mt-8">
              <SkillInstructionsPanel content={query.data.content ?? ""} />
            </section>
          </>
        ) : (
          <QueryState
            loadingLayout="detail"
            pending={query.isPending && Boolean(versionId)}
            error={query.error}
            emptyLabel={t("detail.notFound")}
            onRetry={() => void query.refetch()}
          />
        )}
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
