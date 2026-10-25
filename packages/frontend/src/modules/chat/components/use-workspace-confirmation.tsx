import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type Confirmation = { kind: "file" | "directory" | "discard"; path: string };
/** 破坏性工作区动作在共享 UI 确认，宿主和 Core 只执行已确认的请求。 */
export function useWorkspaceConfirmation() {
  const { t } = useTranslation("chat");
  const [request, setRequest] = useState<Confirmation>();
  const pending = useRef<((value: boolean) => void) | undefined>(undefined);
  useEffect(
    () => () => {
      pending.current?.(false);
      pending.current = undefined;
    },
    [],
  );
  const finish = (value: boolean) => {
    pending.current?.(value);
    pending.current = undefined;
    setRequest(undefined);
  };
  const confirm = (next: Confirmation) =>
    new Promise<boolean>((resolve) => {
      pending.current?.(false);
      pending.current = resolve;
      setRequest(next);
    });
  const dialog = request ? (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) finish(false);
      }}
    >
      <DialogContent
        size="sm"
        title={t(
          request.kind === "file"
            ? "workspace.deleteFile"
            : request.kind === "directory"
              ? "workspace.deleteDirectory"
              : "workspace.confirmDiscard",
        )}
        description={request.path}
      >
        <p className="text-destructive type-body px-6 pb-5">
          {t(
            request.kind === "discard"
              ? "workspace.discardWarning"
              : request.kind === "directory"
                ? "workspace.deleteDirectoryWarning"
                : "workspace.deleteFileWarning",
          )}
        </p>
        <DialogFooter>
          <Button variant="secondary" onClick={() => finish(false)}>
            {t("common:cancel")}
          </Button>
          <Button variant="danger" onClick={() => finish(true)}>
            {request.kind === "discard"
              ? t("workspace.confirmDiscard")
              : t("common:resourceSelection.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ) : null;
  return { confirm, dialog };
}
