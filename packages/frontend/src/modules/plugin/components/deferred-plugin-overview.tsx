import { lazy, Suspense, type ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";
const Content = lazy(() =>
  import("./plugin-overview").then((module) => ({
    default: module.PluginOverview,
  })),
);

/** 插件详情与正文在实际进入详情后加载，查询期间仍使用统一内容骨架。 */
export function PluginOverview(props: ComponentProps<typeof Content>) {
  const { t } = useTranslation("common");
  return (
    <Suspense
      fallback={<ResourceLoadingState label={t("detailState.loading")} />}
    >
      <Content {...props} />
    </Suspense>
  );
}
