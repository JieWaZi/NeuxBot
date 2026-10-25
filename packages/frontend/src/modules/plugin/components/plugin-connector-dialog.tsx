import { Play } from "lucide-react";
import { PageAction } from "@/shared/ui/resource-page";
import { ConnectorDetails } from "@/modules/connector";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { useTranslation } from "react-i18next";
import { usePluginConnector } from "../api/use-plugin-connector";
import type { PluginSkillContext } from "./plugin-skill-dialog";

/** 插件连接器只读弹窗与资源详情页共用脱敏信息，不提供独立维护操作。 */
export function PluginConnectorDialog({
  context,
  connectorKey,
  name,
  sourceName,
  onClose,
}: {
  context: PluginSkillContext;
  connectorKey: string;
  name: string;
  sourceName: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("plugin");
  const query = usePluginConnector(context, connectorKey);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        title={name}
        description={t("fromPlugin", { name: sourceName })}
        size="xl"
        layout="form"
      >
        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6">
          {query.data?.connector ? (
            <div className="space-y-8">
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
            </div>
          ) : (
            <QueryState
              loadingLayout="detail"
              pending={query.isPending}
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** 页面与弹窗使用相同的显式连接探测入口，未准备版本不能启动插件内容。 */
export function PluginConnectorTestAction({
  probe,
}: {
  probe: ReturnType<typeof usePluginConnector>;
}) {
  const { t } = useTranslation("connector");
  return (
    <PageAction
      intent="primary"
      disabled={!probe.canTest || probe.testing}
      label={t(probe.testing ? "detail.testing" : "detail.test")}
      title={!probe.canTest ? t("plugin:connectorProbeUnavailable") : undefined}
      onClick={probe.test}
    >
      <Play />
    </PageAction>
  );
}
