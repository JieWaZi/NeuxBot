import { LoaderCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type TeamConfirmationDialogProps = {
  confirmLabel: string;
  description: string;
  errorMessage?: string;
  isPending?: boolean;
  onConfirm: () => Promise<void> | void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  title: string;
};

export function TeamConfirmationDialog({
  confirmLabel,
  description,
  errorMessage,
  isPending = false,
  onConfirm,
  onOpenChange,
  open,
  title,
}: TeamConfirmationDialogProps) {
  const { t } = useTranslation("common");

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!isPending) onOpenChange(nextOpen);
      }}
    >
      <DialogContent size="sm" title={title} description={description}>
        {errorMessage ? (
          <p role="alert" className="text-destructive type-caption px-6 pb-3">
            {errorMessage}
          </p>
        ) : null}
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            disabled={isPending}
            onClick={() => onOpenChange(false)}
          >
            {t("cancel")}
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={isPending}
            onClick={() => void onConfirm()}
          >
            {isPending ? <LoaderCircle className="animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
