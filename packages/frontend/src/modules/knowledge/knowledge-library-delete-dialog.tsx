import { useState } from "react";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import type { Library } from "./knowledge-model";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { useCopy } from "./use-extended-copy";

export function KnowledgeLibraryDeleteDialog({
  library,
  open,
  onOpenChange,
  onDelete,
}: {
  library: Library;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDelete: (library: Library, requestId: string) => Promise<void>;
}) {
  const t = useCopy();
  const [requestId] = useState(() => crypto.randomUUID());
  const [pending, setPending] = useState(false);
  const confirmDelete = async () => {
    if (pending) return;
    setPending(true);
    try {
      await onDelete(library, requestId);
      onOpenChange(false);
    } catch (cause) {
      reportKnowledgeError(
        cause,
        `delete-library:${library.ref.connectionId}:${library.id}`,
        t("deleteLibraryFailed"),
      );
    } finally {
      setPending(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!pending) onOpenChange(value);
      }}
    >
      <DialogContent
        title={t(library.mountId ? "removeRemoteMount" : "deleteLibrary")}
        description={t(
          library.mountId ? "removeRemoteMountHelp" : "deleteLibraryHelp",
          { name: library.name },
        )}
      >
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={pending}
            onClick={() => void confirmDelete()}
          >
            {t(
              library.mountId
                ? "removeRemoteMount"
                : pending
                  ? "deletingLibrary"
                  : "deleteLibrary",
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
