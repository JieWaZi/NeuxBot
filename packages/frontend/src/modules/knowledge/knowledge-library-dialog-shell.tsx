import type { ReactNode } from "react";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { useCopy } from "./use-copy";

/** 外壳跨代码加载、数据加载和编辑保持同一实例，避免重复播放弹窗动效。 */
export function KnowledgeLibraryDialogShell({
  opened = true,
  title,
  close,
  children,
}: {
  opened?: boolean;
  title: string;
  close: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog
      open={opened}
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent title={title} size="xl" layout="form">
        {children}
      </DialogContent>
    </Dialog>
  );
}

export function KnowledgeLibraryDialogLoading({
  close,
}: {
  close: () => void;
}) {
  const t = useCopy();
  return (
    <>
      <p
        role="status"
        aria-busy="true"
        className="type-body text-content-secondary flex-1 px-6 py-5"
      >
        {t("loadingContent")}
      </p>
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={close}>
          {t("cancel")}
        </Button>
      </DialogFooter>
    </>
  );
}
