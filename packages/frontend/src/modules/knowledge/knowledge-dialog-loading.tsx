import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { useCopy } from "./use-copy";

/** 按需表单加载时保留可关闭的统一外壳，不持有草稿或服务器状态。 */
export function KnowledgeDialogLoading({
  opened = true,
  title,
  description,
  close,
  form = false,
}: {
  opened?: boolean;
  title: string;
  description?: string;
  close: () => void;
  form?: boolean;
}) {
  const t = useCopy();
  return (
    <Dialog
      open={opened}
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        title={title}
        description={description}
        size={form ? "xl" : "md"}
        layout={form ? "form" : "content"}
      >
        <p
          role="status"
          aria-busy="true"
          className="type-body text-content-secondary px-6 pb-5"
        >
          {t("loadingContent")}
        </p>
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            {t("cancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
