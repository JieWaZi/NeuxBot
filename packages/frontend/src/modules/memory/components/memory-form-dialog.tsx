import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createMemoryMutation,
  invalidateMemoryQueries,
  updateMemoryMutation,
} from "../api/memory-queries";
import type { MemoryResource, MemoryScope } from "../model/types";
import { MemoryWorkspaceDialogContent } from "./memory-workspace-dialog-content";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogClose, DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  memory?: MemoryResource | null;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  projectId: string;
  scope: MemoryScope;
  scopeName?: string;
  wakerId: string;
};

export function MemoryFormDialog({ ...props }: Props) {
  if (!props.open) return null;
  return <OpenMemoryFormDialog {...props} />;
}

function OpenMemoryFormDialog({
  memory,
  onOpenChange,
  open,
  projectId,
  scope,
  scopeName,
  wakerId,
}: Props) {
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
    const body = {
      content: normalized,
      projectId: scope === "project" ? projectId : undefined,
      scope,
    };
    try {
      if (memory?.id) {
        await updateMemory.mutateAsync({
          body,
          path: { memoryId: memory.id, wakerId },
        });
      } else {
        await createMemory.mutateAsync({ body, path: { wakerId } });
      }
      await invalidateMemoryQueries(queryClient);
      onOpenChange(false);
    } catch {
      setLocalError(t("form.saveFailed"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <MemoryWorkspaceDialogContent
        title={memory ? t("form.editTitle") : t("form.createTitle")}
        description={t("form.description", {
          scope:
            scopeName ??
            (scope === "partner"
              ? t("scope.partner")
              : t("scope.unnamedProject")),
        })}
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
