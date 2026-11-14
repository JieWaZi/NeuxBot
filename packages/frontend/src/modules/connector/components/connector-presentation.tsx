import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cable } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { PartnerPicker } from "@/modules/plugin";
import {
  getPluginOptions,
  listWakerConnectorsOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { chatPromptDestination } from "@/shared/lib/chat-draft-intent";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { ResourceCatalogTry } from "@/shared/ui/resource-catalog-try";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { connectorReferenceAvailable } from "../model/connector-view";

type ConnectorTryContext =
  | { kind: "connector"; connectorId: string }
  | { kind: "plugin"; pluginId: string; versionId: string }
  | { kind: "preview" };

/** 产品默认试用只创建带真实资源引用的独立草稿，不安装、启用或发送。 */
export function ConnectorPresentation({
  name,
  context,
  wakerId = "",
  usable = false,
  systemManaged = false,
}: {
  name: string;
  context: ConnectorTryContext;
  wakerId?: string;
  usable?: boolean;
  systemManaged?: boolean;
}) {
  const { t } = useTranslation("connector");
  const active = useRouteSceneActive();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>(wakerId ? [wakerId] : []);
  const target = selected[0] ?? "";
  const connectors = useQuery({
    ...listWakerConnectorsOptions(
      withLocalErrorSurface({ path: { wakerId: target || "pending" } }),
    ),
    enabled: active && open && context.kind === "connector" && Boolean(target),
  });
  const plugin = useQuery({
    ...getPluginOptions(
      withLocalErrorSurface({
        path: {
          pluginId: context.kind === "plugin" ? context.pluginId : "pending",
        },
        query: { wakerId: target },
      }),
    ),
    enabled: active && open && context.kind === "plugin" && Boolean(target),
  });
  const connector =
    context.kind === "connector"
      ? connectors.data?.find(
          (item) => item.connector?.id === context.connectorId,
        )
      : undefined;
  const installation = plugin.data?.installations?.find(
    (item) => item.wakerId === target,
  );
  const canTry =
    context.kind === "connector"
      ? connectorReferenceAvailable(connector, context.connectorId)
      : context.kind === "plugin" &&
        installation?.enabled === true &&
        Boolean(context.versionId) &&
        installation.versionId === context.versionId;
  const query = context.kind === "connector" ? connectors : plugin;
  const prompt = t("try.prompt");
  const unavailable = systemManaged
    ? t("try.systemManaged")
    : t("try.unavailable");
  const disabled =
    systemManaged || context.kind === "preview" || Boolean(wakerId && !usable);
  const draft = (partnerId: string) => {
    if (disabled) return;
    const destination =
      context.kind === "connector"
        ? chatPromptDestination(
            prompt,
            partnerId,
            undefined,
            undefined,
            context.connectorId,
          )
        : chatPromptDestination(
            prompt,
            partnerId,
            context.kind === "plugin" ? context.pluginId : undefined,
          );
    setOpen(false);
    navigate(destination.to, { state: destination.state });
  };
  return (
    <>
      <ResourceCatalogTry
        seed={name}
        name={name}
        logo={<Cable />}
        prompts={[prompt]}
        caption={disabled ? unavailable : undefined}
        onTry={
          disabled
            ? undefined
            : () => {
                if (wakerId && usable) draft(wakerId);
                else setOpen(true);
              }
        }
      />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title={t("try.choosePartner")} layout="form">
          <div className="grid gap-4 px-6 pb-4">
            <p className="type-resource-body">{prompt}</p>
            <PartnerPicker
              selectionMode="single"
              selected={selected}
              onChange={setSelected}
              onlyPartner={wakerId || undefined}
            />
            {target && (query.isPending || query.isError) ? (
              <QueryState
                loadingLayout="content"
                pending={query.isPending}
                error={query.error}
                onRetry={() => void query.refetch()}
              />
            ) : target && !canTry ? (
              <p className="type-resource-body text-resource-muted">
                {t("try.unavailable")}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              variant="catalog-ghost"
              size="catalog"
              onClick={() => setOpen(false)}
            >
              {t("common:cancel")}
            </Button>
            <Button
              variant="catalog-primary"
              size="catalog"
              disabled={
                disabled ||
                !target ||
                !canTry ||
                query.isFetching ||
                query.isError
              }
              onClick={() => draft(target)}
            >
              {t("try.continue")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
