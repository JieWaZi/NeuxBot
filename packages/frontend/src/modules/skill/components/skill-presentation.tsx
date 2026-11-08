import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useNavigate } from "react-router-dom";
import { PartnerPicker } from "@/modules/plugin";
import {
  getPluginOptions,
  listWakerSkillsOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { chatPromptDestination } from "@/shared/lib/chat-draft-intent";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { ResourceCatalogTry } from "@/shared/ui/resource-catalog-try";

type SkillTryContext =
  | { kind: "skill"; skillId: string; versionId: string }
  | { kind: "plugin"; pluginId: string; versionId: string }
  | { kind: "preview" };

/** Try 只建立独立草稿；引用只来自选定伙伴已启用且与预览一致的安装版本。 */
export function SkillPresentation({
  name,
  context,
  wakerId = "",
  usable = false,
}: {
  name: string;
  context: SkillTryContext;
  wakerId?: string;
  usable?: boolean;
}) {
  const { t } = useTranslation("skill");
  const navigate = useNavigate();
  const active = useRouteSceneActive();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>(wakerId ? [wakerId] : []);
  const target = selected[0] ?? "";
  const skills = useQuery({
    ...listWakerSkillsOptions(
      withLocalErrorSurface({ path: { wakerId: target || "pending" } }),
    ),
    enabled: active && open && context.kind === "skill" && Boolean(target),
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
  const skill =
    context.kind === "skill"
      ? skills.data?.find((item) => item.skill?.id === context.skillId)
      : undefined;
  const installation = plugin.data?.installations?.find(
    (item) => item.wakerId === target,
  );
  const canTry =
    context.kind === "skill"
      ? Boolean(
          skill?.enabled &&
          context.versionId &&
          skill.currentVersion?.id === context.versionId,
        )
      : Boolean(
          plugin.data?.packageStatus === "available" &&
          installation?.enabled &&
          context.kind === "plugin" &&
          context.versionId &&
          installation.versionId === context.versionId,
        );
  const query = context.kind === "skill" ? skills : plugin;
  const prompt = t("try.prompt");
  const draft = (partnerId: string) => {
    const destination =
      context.kind === "skill"
        ? chatPromptDestination(prompt, partnerId, undefined, context.skillId)
        : chatPromptDestination(
            prompt,
            partnerId,
            context.kind === "plugin" ? context.pluginId : undefined,
          );
    // Scene 导航后仍保留挂载，需显式关闭位于 Portal 的选择框。
    setOpen(false);
    navigate(destination.to, { state: destination.state });
  };
  return (
    <>
      <ResourceCatalogTry
        seed={name}
        name={name}
        logo={<Sparkles />}
        prompts={[prompt]}
        caption={context.kind === "preview" ? t("try.unavailable") : undefined}
        onTry={
          context.kind === "preview"
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
              disabled={!target || !canTry || query.isFetching || query.isError}
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
