import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/shared/ui/page-header";
import { PluginIcon } from "./plugin-icon";
import {
  pluginDisplayName,
  pluginSummary,
  type PluginCatalogEntry,
} from "../model/plugin-view";

/** 三种详情入口共用标题和单行简介，完整简介通过原生悬停提示阅读。 */
export function PluginDetailHeader({
  pkg,
  name,
  description,
  sourceKey,
  ...props
}: Pick<ComponentProps<typeof PageHeader>, "actions" | "backAction"> & {
  pkg?: PluginCatalogEntry["package"];
  name?: string;
  description?: string;
  sourceKey?: string;
}) {
  const { t } = useTranslation("plugin");
  const summary = pluginSummary(pkg, description, sourceKey);
  return (
    <PageHeader
      {...props}
      layout="detail"
      visual={<PluginIcon pkg={pkg} hero />}
      title={pluginDisplayName(pkg, name) || t("plugins")}
      description={
        summary && (
          <p className="truncate" title={summary}>
            {summary}
          </p>
        )
      }
    />
  );
}
