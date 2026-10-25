import { Link } from "react-router-dom";
import { DropdownMenuItem } from "@/shared/ui/dropdown-menu";
import { Cable, Package } from "lucide-react";
import { useTranslation } from "react-i18next";
import { SkillIcon } from "@/modules/skill";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import {
  ResourceManagementCard,
  ResourcePartnerUsage,
  ResourceMoreAction,
} from "@/shared/ui/resource-management-card";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { useResourceWindow } from "@/shared/hooks/use-resource-window";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { ResourceSourceHeading } from "@/shared/ui/resource-source-heading";
import type { usePluginComponents } from "../api/use-plugin-components";

/** 插件能力保留所属插件与固定版本；查看正文和管理始终回到原安装事实。 */
export function PluginComponentCollection({
  kind,
  wakerId = "",
  resources,
  preview,
  onSourceSelect,
}: {
  kind: "skills" | "connectors";
  wakerId?: string;
  resources: ReturnType<typeof usePluginComponents>;
  preview: boolean;
  onSourceSelect: () => void;
}) {
  const { t } = useTranslation("plugin");
  const query = resources;
  const components = query.items;
  const window = useResourceWindow(
    components,
    JSON.stringify([wakerId, kind, preview]),
  );
  if (preview && !query.isPending && !query.isError && components.length === 0)
    return null;
  return (
    <section className="space-y-3" aria-label={t("fromPlugins")}>
      {preview && components.length > 0 && (
        <ResourceSourceHeading
          title={t("fromPlugins")}
          count={components.length}
          onSelect={preview ? onSourceSelect : undefined}
        />
      )}
      {query.isPending ||
      (query.isError && !components.length) ||
      !components.length ? (
        <QueryState
          loadingLayout="cards"
          pending={query.isPending}
          error={query.error}
          emptyLabel={t("noResults")}
          onRetry={() => void query.refetch()}
        />
      ) : (
        <ResourceCardGrid>
          {window.visible.map((item) => {
            const to = wakerId
              ? `/wakers/${wakerId}/plugins/${item.pluginId}`
              : `/resources/plugins/${item.pluginId}`;
            const detailTo =
              kind === "skills"
                ? `${to}/skills/${encodeURIComponent(item.key)}?version=${encodeURIComponent(item.versionId)}`
                : `${to}/connectors/${encodeURIComponent(item.key)}?version=${encodeURIComponent(item.versionId)}`;
            return (
              <ResourceManagementCard
                key={item.id}
                title={item.name}
                description={item.description}
                source={t("fromPlugin", { name: item.sourceName })}
                icon={
                  kind === "skills" ? (
                    <SkillIcon />
                  ) : (
                    <span className="bg-muted text-muted-foreground grid size-10 shrink-0 place-items-center rounded-md">
                      <Cable className="size-icon-lg" />
                    </span>
                  )
                }
                to={detailTo}
                label={t(kind === "skills" ? "viewSkill" : "viewConnector", {
                  name: item.name,
                })}
                usage={
                  <ResourcePartnerUsage
                    count={item.installations.length}
                    partners={item.installations.map((relation) => ({
                      id: relation.wakerId ?? "",
                      name:
                        item.detail.partnerNames?.[relation.wakerId ?? ""] ||
                        t("unknown"),
                      avatar:
                        item.detail.partnerAvatars?.[relation.wakerId ?? ""],
                    }))}
                  />
                }
                actions={
                  <ResourceMoreAction to={detailTo} name={item.name}>
                    <DropdownMenuItem asChild>
                      <Link to={to}>
                        <Package />
                        {t("viewPluginAction")}
                      </Link>
                    </DropdownMenuItem>
                  </ResourceMoreAction>
                }
              />
            );
          })}
        </ResourceCardGrid>
      )}
      {components.length > 0 && query.isError && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
      {!preview && (
        <ResourceInfiniteScroll
          hasMore={window.hasMore}
          onLoadMore={window.loadMore}
        />
      )}
    </section>
  );
}
