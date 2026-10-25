import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";

type SkillResourcePathDialogProps = {
  confirmLabel: string;
  error: boolean;
  label: string;
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
  onValueChange: (value: string) => void;
  open: boolean;
  title: string;
  value: string;
};

export function SkillResourcePathDialog({
  confirmLabel,
  error,
  label,
  onConfirm,
  onOpenChange,
  onValueChange,
  open,
  title,
  value,
}: SkillResourcePathDialogProps) {
  const { t } = useTranslation("skill");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={title} size="sm">
        <div className="grid gap-2 px-6 pb-6">
          <label className="grid gap-1.5">
            <span className="type-label font-medium">{label}</span>
            <Input
              value={value}
              aria-label={label}
              aria-invalid={error}
              onChange={(event) => onValueChange(event.target.value)}
            />
          </label>
          {error ? (
            <p role="alert" className="text-destructive type-caption">
              {t("detail.resourcePathInvalid")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {t("common:cancel")}
          </Button>
          <Button disabled={error || !value.trim()} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type SkillResourceDeleteDialogProps = {
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  path: string;
};

export function SkillResourceDeleteDialog({
  onConfirm,
  onOpenChange,
  open,
  path,
}: SkillResourceDeleteDialogProps) {
  const { t } = useTranslation("skill");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("detail.deleteResource")}
        description={t("detail.deleteResourceDescription", { path })}
        size="sm"
      >
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {t("common:cancel")}
          </Button>
          <Button variant="danger" onClick={onConfirm}>
            {t("detail.confirmDeleteResource")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
