import { lazy, Suspense, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { chatPromptDestination } from "@/shared/lib/chat-draft-intent";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { ResourceCatalogTry } from "@/shared/ui/resource-catalog-try";
import { PluginIcon } from "./plugin-icon";
import { getPluginOptions } from "../api/plugin-queries";
import {
  pluginLocale,
  pluginDisplayName,
  pluginCatalogSnapshotVersion,
  type PluginCatalogEntry,
  type PluginDetail,
} from "../model/plugin-view";
import { PartnerPicker } from "./partner-picker";
import { PluginInstallDialog } from "./deferred-plugin-dialogs";

const CreatePartner = lazy(() =>
  import("@/modules/waker").then((module) => ({
    default: module.WakerFormDialog,
  })),
);

/** 作者示例或产品默认提示只贡献输入草稿；安装、启用与发送仍分别确认。 */
export function PluginPresentation({
  pkg,
  detail,
  catalog,
  wakerId = "",
}: {
  pkg?: PluginCatalogEntry["package"];
  detail?: PluginDetail;
  catalog?: PluginCatalogEntry;
  wakerId?: string;
}) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const navigate = useNavigate();
  const unavailableReason = useId();
  const presentation = pkg?.presentation;
  const localizedPrompts = presentation?.defaultPromptLocalizations?.[
    locale
  ]?.filter((text) => text.trim());
  const defaultPrompts = presentation?.defaultPrompt?.filter((text) =>
    text.trim(),
  );
  const prompts = localizedPrompts?.length
    ? localizedPrompts
    : defaultPrompts?.length
      ? defaultPrompts
      : [t("tryPrompt")];
  const [prompt, setPrompt] = useState<string | null>(null);
  const [selected, setSelected] = useState<string[]>(wakerId ? [wakerId] : []);
  const [installing, setInstalling] = useState(false);
  const [creating, setCreating] = useState(false);
  const pluginId = detail?.plugin?.id;
  const current = useQuery({
    ...getPluginOptions(
      withLocalErrorSurface({
        path: { pluginId: pluginId ?? "pending" },
        query: { wakerId: selected[0] },
      }),
    ),
    enabled: Boolean(pluginId && prompt && selected[0]),
  });
  const selectedDetail = current.data ?? detail;
  const selectedUnsupported = selectedDetail?.packageStatus === "unsupported";
  const matchesSnapshot = (
    candidate: PluginDetail | undefined,
    target: string,
  ) => {
    const versionId = catalog
      ? pluginCatalogSnapshotVersion(candidate, catalog)?.id
      : detail?.resolvedVersionId;
    return Boolean(
      pluginId &&
      candidate?.plugin?.id === pluginId &&
      candidate.packageStatus === "available" &&
      versionId &&
      candidate.installations?.some(
        (item) =>
          item.wakerId === target &&
          item.enabled &&
          item.versionId === versionId,
      ),
    );
  };
  const canReference = matchesSnapshot(selectedDetail, selected[0] ?? "");
  const selectedRelation = selectedDetail?.installations?.find(
    (item) => item.wakerId === selected[0],
  );
  const openDraft = (text: string, target: string, attached?: string) => {
    const destination = chatPromptDestination(text, target, attached);
    // Scene 导航后仍保留挂载，消费提示以关闭选择框，保留伙伴选择。
    setPrompt(null);
    navigate(destination.to, { state: destination.state });
  };
  if (detail?.packageStatus === "unsupported") return null;
  return (
    <>
      <ResourceCatalogTry
        seed={pkg?.manifest?.name ?? pluginDisplayName(pkg)}
        name={pluginDisplayName(pkg)}
        logo={<PluginIcon pkg={pkg} size="navigation" />}
        prompts={prompts}
        onTry={(text) => {
          if (wakerId && matchesSnapshot(detail, wakerId))
            openDraft(text, wakerId, pluginId);
          else setPrompt(text);
        }}
      />
      <Dialog
        open={Boolean(prompt && !installing && !creating)}
        onOpenChange={(open) => {
          if (!open) setPrompt(null);
        }}
      >
        <DialogContent title={t("chooseExamplePartner")} layout="form">
          <div className="grid gap-4 px-6 pb-4">
            <p className="type-resource-body whitespace-pre-wrap">{prompt}</p>
            <PartnerPicker
              selectionMode="single"
              selected={selected}
              onChange={setSelected}
              onlyPartner={wakerId || undefined}
            />
            {!wakerId && (
              <Button
                variant="catalog-ghost"
                size="catalog"
                onClick={() => setCreating(true)}
              >
                <Plus />
                {t("createExamplePartner")}
              </Button>
            )}
            {selected[0] && !canReference && (
              <div className="grid justify-items-start gap-2">
                <p
                  id={unavailableReason}
                  className="type-resource-body text-resource-muted"
                >
                  {t("exampleSnapshotUnavailable")}
                </p>
                {!selectedRelation && (
                  <Button
                    variant="catalog-secondary"
                    size="catalog"
                    disabled={
                      current.isFetching ||
                      current.isError ||
                      selectedUnsupported
                    }
                    onClick={() => setInstalling(true)}
                  >
                    {t("install")}
                  </Button>
                )}
              </div>
            )}
            {(current.isError || selectedUnsupported) && (
              <p role="alert" className="type-resource-body text-destructive">
                {t(selectedUnsupported ? "packageUnsupported" : "failed")}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="catalog-ghost"
              size="catalog"
              onClick={() => setPrompt(null)}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="catalog-primary"
              size="catalog"
              aria-describedby={
                selected[0] && !canReference ? unavailableReason : undefined
              }
              disabled={
                !selected[0] ||
                current.isFetching ||
                current.isError ||
                !canReference
              }
              onClick={() => {
                if (prompt && selected[0] && canReference)
                  openDraft(prompt, selected[0], pluginId);
              }}
            >
              {t("exampleDraftOnly")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {installing && (
        <PluginInstallDialog
          catalog={catalog}
          detail={selectedDetail}
          wakerId={selected[0]}
          onClose={() => {
            setInstalling(false);
            void current.refetch();
          }}
        />
      )}
      {creating && (
        <Suspense fallback={null}>
          <CreatePartner open onOpenChange={setCreating} />
        </Suspense>
      )}
    </>
  );
}
