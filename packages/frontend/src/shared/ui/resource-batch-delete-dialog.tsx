import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "./button";
import { Dialog, DialogContent, DialogFooter } from "./dialog";

export type ResourceDeleteTarget = {
  id: string;
  name: string;
  icon?: ReactNode;
  description?: string;
};

export function ResourceBatchDeleteDialog({
  items,
  title,
  description,
  warning,
  onDelete,
  onDeleted,
  onOpenChange,
  children,
  disabled = false,
}: {
  items: ResourceDeleteTarget[];
  title: string;
  description?: string;
  warning: string;
  onDelete: (id: string) => Promise<unknown>;
  onDeleted: (ids: string[]) => Promise<void>;
  onOpenChange: (open: boolean) => void;
  children?: ReactNode;
  disabled?: boolean;
}) {
  const { t } = useTranslation("common");
  const [remaining, setRemaining] = useState(items);
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const running = useRef(false);
  const confirm = async () => {
    if (running.current) return;
    running.current = true;
    setPending(true);
    setFailed(false);
    const deleted: string[] = [];
    const failures: ResourceDeleteTarget[] = [];
    try {
      for (const item of remaining) {
        try {
          await onDelete(item.id);
          deleted.push(item.id);
        } catch {
          failures.push(item);
        }
      }
      setRemaining(failures);
      setFailed(failures.length > 0);
      await onDeleted(deleted);
      if (!failures.length) onOpenChange(false);
    } finally {
      running.current = false;
      setPending(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!running.current) onOpenChange(open);
      }}
    >
      <DialogContent size="sm" title={title} description={description}>
        <div className="min-h-0 overflow-y-auto px-6 pb-5">
          {children}
          <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
            <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
            <p>{warning}</p>
          </div>
          <ul
            className="type-body mt-3 grid gap-2"
            aria-label={t("resourceSelection.targets")}
          >
            {remaining.map((item) => (
              <li
                key={item.id}
                className="bg-card flex min-w-0 items-center gap-3 rounded-lg p-3"
              >
                {item.icon}
                <div className="min-w-0 flex-1">
                  <p className="font-medium break-words">{item.name}</p>
                  {item.description && (
                    <p className="type-caption text-content-secondary mt-1 break-words">
                      {item.description}
                    </p>
                  )}
                </div>
              </li>
            ))}
          </ul>
          {failed ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {t("resourceSelection.failed", { count: remaining.length })}
            </p>
          ) : null}
        </div>
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
            disabled={disabled || pending || !remaining.length}
            onClick={() => void confirm()}
          >
            {pending ? <LoaderCircle className="animate-spin" /> : null}
            {t(
              pending
                ? "resourceSelection.deleting"
                : failed
                  ? "resourceSelection.retry"
                  : "resourceSelection.confirm",
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
