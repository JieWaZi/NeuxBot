import { i18n } from "@/shared/i18n";
import {
  automaticTaskApiZh,
  automaticTaskApiEn,
} from "@/shared/i18n/automatic-task-api";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  integrationServiceOptions,
  integrationServiceQueryKey,
  integrationServiceMutation,
} from "@/shared/api/integration-service";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";

i18n.addResourceBundle(
  "zh-CN",
  "automaticTask",
  automaticTaskApiZh,
  true,
  true,
);
i18n.addResourceBundle(
  "en-US",
  "automaticTask",
  automaticTaskApiEn,
  true,
  true,
);

export function APISettingsForm({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const client = useQueryClient();
  const query = useQuery(integrationServiceOptions());
  const [ipDraft, setIPDraft] = useState<string>();
  const [portDraft, setPortDraft] = useState<string>();
  const ip = ipDraft ?? query.data?.ip ?? "";
  const port = portDraft ?? String(query.data?.port || "");
  const save = useMutation({
    ...integrationServiceMutation(),
    onSuccess: async () => {
      await client.invalidateQueries({
        queryKey: integrationServiceQueryKey(),
      });
      onClose();
    },
  });
  const available =
    query.data?.enabled === true && query.data.available === true;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate({
      body: {
        ip: ip.trim(),
        port: Number(port),
        enabled: query.data?.enabled,
        publicBaseUrl: query.data?.publicBaseUrl,
      },
    });
  };
  const invalid =
    !ip.trim() ||
    !Number.isInteger(Number(port)) ||
    Number(port) < 1 ||
    Number(port) > 65535;
  return (
    <form className="flex min-h-0 flex-col" onSubmit={submit}>
      <div className="grid gap-4 overflow-y-auto px-6 pb-6">
        <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-4">
          <div className="grid gap-2">
            <FieldLabel htmlFor={`${id}-ip`} required>
              {t("apiIP")}
            </FieldLabel>
            <Input
              id={`${id}-ip`}
              size="md"
              required
              value={ip}
              placeholder="127.0.0.1"
              disabled={query.isPending || save.isPending}
              onChange={(e) => setIPDraft(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <FieldLabel htmlFor={`${id}-port`} required>
              {t("port")}
            </FieldLabel>
            <Input
              id={`${id}-port`}
              size="md"
              type="number"
              min={1}
              max={65535}
              required
              value={port}
              placeholder="8431"
              disabled={query.isPending || save.isPending}
              onChange={(e) => setPortDraft(e.target.value)}
            />
          </div>
        </div>
        <p className="text-content-secondary type-ui">{t("apiIPHint")}</p>
        <p
          className={
            available ? "text-success type-ui" : "text-warning type-ui"
          }
          role="status"
        >
          {query.isPending
            ? t("common:pageLoading")
            : t(available ? "apiAvailable" : "apiUnavailable")}
        </p>
        {query.error || save.error ? (
          <p role="alert" className="text-destructive type-ui">
            {localApiErrorMessage(query.error || save.error, t("actionFailed"))}
          </p>
        ) : null}
        {query.isError ? (
          <Button
            type="button"
            variant="secondary"
            onClick={() => void query.refetch()}
          >
            {t("retry")}
          </Button>
        ) : null}
      </div>
      <DialogFooter>
        <Button
          type="button"
          variant="secondary"
          disabled={save.isPending}
          onClick={onClose}
        >
          {t("cancel")}
        </Button>
        <Button
          type="submit"
          disabled={
            query.isPending || query.isError || save.isPending || invalid
          }
        >
          {t(save.isPending ? "apiSaving" : "apiConfirm")}
        </Button>
      </DialogFooter>
    </form>
  );
}
