import { lazy, Suspense } from "react";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import type { Library } from "./knowledge-model";
import type { useKnowledgeWrites } from "./knowledge-writes";
import type { KnowledgeOrganizeIntent } from "./knowledge-organize-intent";
import { useCopy } from "./use-copy";
export {
  createKnowledgeOrganizeIntent,
  type KnowledgeOrganizeIntent,
} from "./knowledge-organize-intent";
const LocalContent = lazy(() =>
  import("./knowledge-organize-dialog-content").then((module) => ({
    default: module.KnowledgeOrganizeDialogContent,
  })),
);
const RemoteContent = lazy(() =>
  import("./knowledge-remote-organize").then((module) => ({
    default: module.KnowledgeRemoteOrganize,
  })),
);
export type KnowledgeOrganizeDialogProps = {
  library: Library;
  batch: string[];
  operation: ReturnType<typeof useKnowledgeWrites>["organize"];
  onClose: () => void;
  onAccepted: () => void;
  intent?: KnowledgeOrganizeIntent;
  rebuild?: boolean;
};
/** 弹窗外壳跨分包、读取配置和编辑保持同一实例。 */
export function KnowledgeOrganizeDialog(props: KnowledgeOrganizeDialogProps) {
  const t = useCopy();
  const Content = knowledgeAdapter(props.library.ref).localConfiguration
    ? LocalContent
    : RemoteContent;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose();
      }}
    >
      <DialogContent title={t(props.rebuild ? "rebuildKnowledge" : "organize")}>
        <Suspense
          fallback={
            <>
              <div className="px-6 pb-5">
                <KnowledgeWikiContentLoading />
              </div>
              <DialogFooter>
                <Button variant="secondary" onClick={props.onClose}>
                  {t("close")}
                </Button>
              </DialogFooter>
            </>
          }
        >
          <Content {...props} />
        </Suspense>
      </DialogContent>
    </Dialog>
  );
}
