import { useQuery } from "@tanstack/react-query";
import { Cable } from "lucide-react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ConnectorDetails } from "@/modules/connector";
import { WakerDetailLayout } from "@/modules/waker";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { PageHeader } from "@/shared/ui/page-header";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { QueryState } from "@/shared/ui/query-state";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { getPluginOptions } from "../api/plugin-queries";
import { PluginConnectorTestAction } from "../components/plugin-connector-dialog";
import { usePluginConnector } from "../api/use-plugin-connector";
import { pluginDisplayName } from "../model/plugin-view";

/** 插件组件保持固定版本与伙伴范围，维护操作统一回到所属插件。 */
export function PluginConnectorDetailPage() {
  const { t } = useTranslation("connector");
  const { pluginId = "", connectorKey = "", wakerId = "" } = useParams();
  const [params] = useSearchParams();
  const versionId = params.get("version") ?? "";
  const active = useRouteSceneActive();
  const navigate = useNavigate();
  const location = useLocation();
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId
      ? `/wakers/${wakerId}/resources?category=connectors`
      : "/resources?category=connectors");
  const context = { pluginId, versionId, wakerId };
  const query = usePluginConnector(context, connectorKey, active);
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
  const source = pluginDisplayName(version?.package, parent.data?.plugin?.name);
  return (
    <WakerDetailLayout active="connectors">
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          title={query.data?.connector?.name ?? (connectorKey || t("title"))}
          description={
            source ? t("plugin:fromPlugin", { name: source }) : undefined
          }
          visual={
            <span className="bg-muted text-muted-foreground grid size-10 place-items-center rounded-md">
              <Cable className="size-icon-md" />
            </span>
          }
          backAction={{
            label: t("common:backToList"),
            onClick: () => navigate(back),
          }}
        />
        <div className="mt-8 space-y-8">
          {query.data?.connector ? (
            <>
              {query.isError && (
                <QueryState
                  error={query.error}
                  onRetry={() => void query.refetch()}
                />
              )}
              <ConnectorDetails
                connector={query.data.connector}
                compatibility={query.data.compatibility}
                testing={query.testing}
                testError={query.testError}
                testAction={<PluginConnectorTestAction probe={query} />}
                managed
              />
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
        </div>
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
