import { useQuery } from "@tanstack/react-query";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { SkillInstructionsPanel, SkillMetadataCard } from "@/modules/skill";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  getPluginSkillOptions,
  getCatalogPluginSkillOptions,
} from "../api/plugin-queries";

export type PluginSkillContext = {
  catalogKey?: string;
  sourceCommit?: string;
  archiveSHA256?: string;
  target?: string;
  pluginId?: string;
  versionId?: string;
  wakerId?: string;
};

/** 插件技能弹窗复用详情页的元数据与正文，读取所属插件固定版本。 */
export function PluginSkillDialog({
  context,
  skillKey,
  name,
  sourceName,
  onClose,
}: {
  context: PluginSkillContext;
  skillKey: string;
  name: string;
  sourceName: string;
  onClose: () => void;
}) {
  const catalog = useQuery({
    ...getCatalogPluginSkillOptions(
      withLocalErrorSurface({
        path: { key: context.catalogKey ?? "" },
        query: {
          skillKey,
          target: context.target,
          sourceCommit: context.sourceCommit,
          archiveSHA256: context.archiveSHA256,
        },
      }),
    ),
    enabled: Boolean(context.catalogKey),
  });
  const installed = useQuery({
    ...getPluginSkillOptions(
      withLocalErrorSurface({
        path: { pluginId: context.pluginId ?? "" },
        query: {
          versionId: context.versionId ?? "",
          key: skillKey,
          target: context.target,
          wakerId: context.wakerId,
        },
      }),
    ),
    enabled:
      !context.catalogKey && Boolean(context.pluginId && context.versionId),
  });
  const query = context.catalogKey ? catalog : installed;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent title={`/${name}`} size="xl" layout="form">
        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6">
          {query.isPending || query.isError ? (
            <QueryState
              loadingLayout="content"
              pending={query.isPending}
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <>
              <SkillMetadataCard
                name={query.data?.skill?.name ?? name}
                source={sourceName}
                description={query.data?.skill?.description ?? ""}
              />
              <div className="mt-8">
                <SkillInstructionsPanel content={query.data?.content ?? ""} />
              </div>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
