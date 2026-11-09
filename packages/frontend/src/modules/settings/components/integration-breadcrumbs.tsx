import { useTranslation } from "react-i18next";
import { useLocation, useSearchParams } from "react-router-dom";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import {
  integrationOverviewParams,
  integrationTabOf,
} from "../settings-navigation";
import { useIntegrationCopy } from "../use-integration-copy";

/** 顶栏路径由 Scene 持有，不等待所属集成配置模块加载。 */
export function IntegrationBreadcrumbs({
  connection,
}: {
  connection?: { name?: string; platform?: string };
}) {
  const { t } = useTranslation(["appSettings", "im"]);
  const copy = useIntegrationCopy();
  const location = useLocation();
  const [params] = useSearchParams();
  const tab = integrationTabOf(location.pathname, params);
  const platform = connection?.platform || params.get("platform");
  const name =
    params.get("detail") === "service" && tab !== "im"
      ? tab === "api"
        ? copy("service")
        : copy("mcp.service")
      : tab === "im" &&
          platform &&
          ["weixin", "feishu", "wecom"].includes(platform)
        ? t(`im:${platform}`)
        : tab === "im" && params.has("connection")
          ? "IM"
          : undefined;
  const overview = integrationOverviewParams(params);
  const parent = new URLSearchParams(params);
  for (const key of ["connection", "binding", "chat"]) parent.delete(key);
  if (platform) parent.set("platform", platform);
  return (
    <SceneBreadcrumbs
      ariaLabel={t("sceneLabel")}
      items={
        name
          ? [
              {
                label: t("sections.integrations"),
                href: `/settings/integrations${overview.size ? `?${overview}` : ""}`,
              },
              {
                label: name,
                href: params.has("connection")
                  ? `/settings/integrations?${parent}`
                  : undefined,
              },
              ...(params.has("connection")
                ? [{ label: connection?.name || t("im:connectionDetail") }]
                : []),
            ]
          : [{ label: t("sections.integrations") }]
      }
    />
  );
}
