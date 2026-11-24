import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createMemoryMutation,
  invalidatePartnerMemory,
  updateMemoryMutation,
} from "../api/memory-queries";
import type { MemoryResource } from "../model/types";
import { MemoryWorkspaceDialogContent } from "./memory-workspace-dialog-content";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogClose, DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  memory?: MemoryResource | null;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  wakerId: string;
};

export function MemoryFormDialog({ ...props }: Props) {
  if (!props.open) return null;
  return <OpenMemoryFormDialog {...props} />;
}

function OpenMemoryFormDialog({ memory, onOpenChange, open, wakerId }: Props) {
  const { t } = useTranslation("memory");
  const queryClient = useQueryClient();
  const [content, setContent] = useState(memory?.content ?? "");
  const [localError, setLocalError] = useState("");
  const createMemory = useMutation(
    withLocalErrorMutation(createMemoryMutation()),
  );
  const updateMemory = useMutation(
    withLocalErrorMutation(updateMemoryMutation()),
  );
  const pending = createMemory.isPending || updateMemory.isPending;

  const save = async () => {
    const normalized = content.trim();
    if (!normalized) {
      setLocalError(t("form.required"));
      return;
    }
    const body = { content: normalized };
    try {
      if (memory?.id) {
        await updateMemory.mutateAsync({
          body,
          path: { memoryId: memory.id, wakerId },
        });
      } else {
        await createMemory.mutateAsync({ body, path: { wakerId } });
      }
      await invalidatePartnerMemory(queryClient, wakerId);
      onOpenChange(false);
    } catch (error) {
      setLocalError(localApiErrorMessage(error, t("form.saveFailed")));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <MemoryWorkspaceDialogContent
        title={memory ? t("form.editTitle") : t("form.createTitle")}
        description={t("form.description")}
      >
        <div className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)_auto_auto] gap-2 px-6 py-5">
          <label className="type-label font-medium" htmlFor="memory-content">
            {t("form.content")}
          </label>
          <Textarea
            id="memory-content"
            value={content}
            maxLength={4096}
            size="lg"
            placeholder={t("form.placeholder")}
            onChange={(event) => setContent(event.target.value)}
          />
          <p className="text-muted-foreground type-caption text-right">
            {t("form.count", {
              count: Array.from(content).length,
              max: 4096,
            })}
          </p>
          {localError ? (
            <p role="alert" className="text-destructive type-body">
              {localError}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary" disabled={pending}>
              {t("form.cancel")}
            </Button>
          </DialogClose>
          <Button disabled={pending} onClick={() => void save()}>
            {pending ? t("form.saving") : t("form.save")}
          </Button>
        </DialogFooter>
      </MemoryWorkspaceDialogContent>
    </Dialog>
  );
}
