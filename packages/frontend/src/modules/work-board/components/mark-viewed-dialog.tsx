import { useEffect, useRef, useState, type ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import {
  applyViewingOperation,
  prepareViewingOperation,
  type ViewingOperation,
} from "../api/queries";

export function MarkViewedDialog({
  active,
  visible,
  filters,
}: {
  active: boolean;
  visible: boolean;
  filters: Parameters<typeof prepareViewingOperation>[0];
}) {
  const { t } = useTranslation("workBoard");
  const request = useRef<AbortController>(undefined);
  const [operation, setOperation] = useState<ViewingOperation>();
  const [preparing, setPreparing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState<unknown>();
  useEffect(() => () => request.current?.abort(), [active]);
  const dismiss: ComponentProps<typeof Dialog>["onOpenChange"] = (open) => {
    if (open) return;
    request.current?.abort();
    setPreparing(false);
    setApplying(false);
    setOperation(undefined);
    setError(undefined);
  };
  const prepare = async () => {
    const controller = new AbortController();
    request.current = controller;
    setPreparing(true);
    setError(undefined);
    try {
      const captured = await prepareViewingOperation(
        filters,
        controller.signal,
      );
      if (!controller.signal.aborted) setOperation(captured);
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (request.current === controller) setPreparing(false);
    }
  };
  const apply = async () => {
    if (!operation) return;
    const controller = new AbortController();
    request.current = controller;
    setApplying(true);
    setError(undefined);
    try {
      await applyViewingOperation(operation, controller.signal, setOperation);
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (request.current === controller) setApplying(false);
    }
  };
  const complete =
    operation && operation.completed === operation.targets.length;
  return (
    <>
      {visible ? (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => void prepare()}
          disabled={preparing}
          aria-busy={preparing}
        >
          {t("markAllViewed")}
        </Button>
      ) : null}
      {!operation && error ? (
        <p role="alert" className="text-destructive type-ui">
          {localApiErrorMessage(error, t("viewTargetsFailed"))}
        </p>
      ) : null}
      <Dialog open={active && Boolean(operation)} onOpenChange={dismiss}>
        <DialogContent
          title={t("markAllViewed")}
          description={t("markAllViewedConfirm", {
            count: operation?.targets.length ?? 0,
          })}
        >
          {operation && (operation.completed > 0 || complete) ? (
            <p role="status" className="text-content-secondary type-body">
              {t("markViewedProgress", {
                applied: operation.applied,
                conflict: operation.skippedConflict,
                deleted: operation.skippedDeleted,
                remaining: operation.targets.length - operation.completed,
              })}
            </p>
          ) : null}
          {error ? (
            <p role="alert" className="text-destructive type-ui">
              {localApiErrorMessage(error, t("markViewedFailed"))}
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="ghost" onClick={() => dismiss(false)}>
              {t(complete ? "close" : "cancel")}
            </Button>
            {complete ? null : (
              <Button
                disabled={applying}
                aria-busy={applying}
                onClick={() => void apply()}
              >
                {t(
                  applying
                    ? "markingViewed"
                    : error
                      ? "retry"
                      : "markAllViewed",
                )}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
