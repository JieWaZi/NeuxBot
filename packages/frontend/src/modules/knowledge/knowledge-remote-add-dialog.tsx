import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useId, useState } from "react";
import { addRemoteLibrary } from "@/shared/api/generated/sdk.gen";
import type { AddRemoteLibraryResponse } from "@/shared/api/generated/types.gen";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Button } from "@/shared/ui/button";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { useCopy } from "./use-copy";

const schema = z.object({
  url: z.url().refine((value) => /^https?:\/\//.test(value)),
  apiKey: z.string().min(1).max(16384),
  knowledgeBaseId: z.string().trim().min(1).max(200),
});
type Fields = z.infer<typeof schema>;

/** 新接入仅收集三项业务输入，服务识别与权限验证由后端完成。 */
export function KnowledgeRemoteAddDialog({
  onClose,
  onAdded,
}: {
  onClose: () => void;
  onAdded: (receipt: AddRemoteLibraryResponse) => void | Promise<void>;
}) {
  const t = useCopy();
  const id = useId();
  const [requestId] = useState(() => crypto.randomUUID());
  const form = useForm<Fields>({
    resolver: zodResolver(schema),
    defaultValues: { url: "", apiKey: "", knowledgeBaseId: "" },
  });
  const add = useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (fields: Fields) =>
      (
        await addRemoteLibrary<true>({
          body: { ...fields, requestId },
          throwOnError: true,
        })
      ).data,
    onSuccess: async (receipt) => {
      form.reset();
      await onAdded(receipt);
    },
  });
  function close() {
    if (add.isPending) return;
    form.reset();
    add.reset();
    onClose();
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent title={t("addRemote")} description={t("addRemoteHelp")}>
        <form
          className="flex min-h-0 flex-col"
          onSubmit={form.handleSubmit((fields) => add.mutate(fields))}
        >
          <div className="grid gap-4 overflow-y-auto px-6 pb-6">
            <div className="grid gap-2">
              <FieldLabel htmlFor={`${id}-url`} required>
                {t("remoteURL")}
              </FieldLabel>
              <Input
                id={`${id}-url`}
                type="url"
                placeholder={t("remoteURLPlaceholder")}
                {...form.register("url")}
                disabled={add.isPending}
              />
              <p className="type-ui text-content-secondary">
                {t("remoteURLHelp")}
              </p>
            </div>
            <div className="grid gap-2">
              <FieldLabel htmlFor={`${id}-key`} required>
                {t("remoteAPIKey")}
              </FieldLabel>
              <Input
                id={`${id}-key`}
                type="password"
                placeholder="sk-…"
                autoComplete="off"
                {...form.register("apiKey")}
                disabled={add.isPending}
              />
              <p className="type-ui text-content-secondary">
                {t("remoteAPIKeyHelp")}
              </p>
            </div>
            <div className="grid gap-2">
              <FieldLabel htmlFor={`${id}-library`} required>
                {t("remoteLibraryID")}
              </FieldLabel>
              <Input
                id={`${id}-library`}
                placeholder={t("remoteLibraryIDPlaceholder")}
                {...form.register("knowledgeBaseId")}
                disabled={add.isPending}
              />
              <p className="type-ui text-content-secondary">
                {t("remoteLibraryIDHelp")}
              </p>
            </div>
            {Object.keys(form.formState.errors).length > 0 && (
              <p role="alert" className="type-body text-destructive">
                {t("addRemoteHelp")}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={add.isPending}
              onClick={close}
            >
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={add.isPending}>
              {t(add.isPending ? "addingRemote" : "addRemote")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
