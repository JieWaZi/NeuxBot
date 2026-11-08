import { Play } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { useTranslation } from "react-i18next";
import type { usePluginConnector } from "../api/use-plugin-connector";

/** 固定版本详情提供显式连接探测入口，未准备版本不能启动插件内容。 */
export function PluginConnectorTestAction({
  probe,
}: {
  probe: ReturnType<typeof usePluginConnector>;
}) {
  const { t } = useTranslation("connector");
  return (
    <Button
      size="catalog"
      variant="catalog-outline"
      disabled={!probe.canTest || probe.testing}
      title={!probe.canTest ? t("plugin:connectorProbeUnavailable") : undefined}
      onClick={probe.test}
    >
      <Play />
      {t(probe.testing ? "detail.testing" : "detail.test")}
    </Button>
  );
}
