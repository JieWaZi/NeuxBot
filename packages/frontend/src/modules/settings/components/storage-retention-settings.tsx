import { useDialogFocusRestore } from "@/shared/ui/use-dialog-focus-restore";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { Switch } from "@/shared/ui/switch";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectItem,
  SelectContent,
} from "@/shared/ui/select";
import {
  reviewChats,
  savePolicy,
  storageKeys,
  storagePolicyOptions,
} from "../api/storage";
import { storageDate, StorageError, StorageLoading } from "./storage-feedback";
import {
  SettingsGroup,
  SettingsCard,
  SettingsRow,
} from "./settings-section-layout";

type Decision = { kind: "enable" | "shorten"; days: number; revision: number };
const presets = [15, 30, 60, 90];

function RetentionConfirm({
  decision,
  disabled,
  onClose,
  onDone,
}: {
  decision: Decision;
  disabled: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useTranslation("storage");
  const focus = useDialogFocusRestore();
  const submit = useMutation({
    mutationFn: async () => {
      const { id } = await reviewChats(decision.days);
      if (!id) throw new Error();
      await savePolicy(true, decision.days, decision.revision, id);
    },
    onSuccess: onDone,
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !submit.isPending) onClose();
      }}
    >
      <DialogContent
        {...focus}
        title={t("policyTitle")}
        description={t("policyImpact", { days: decision.days })}
      >
        {submit.isError ? (
          <div className="px-6 pb-4">
            <StorageError error={submit.error} />
          </div>
        ) : null}
        <DialogFooter>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={submit.isPending}
            onClick={onClose}
          >
            {t(submit.isError ? "close" : "cancel")}
          </Button>
          {!submit.isError ? (
            <Button
              size="settings"
              variant="danger"
              disabled={disabled || submit.isPending}
              onClick={() => submit.mutate()}
            >
              {t(
                decision.kind === "enable" ? "enableConfirm" : "shortenConfirm",
              )}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function StorageRetentionSettings({
  active,
  disabled,
}: {
  active: boolean;
  disabled: boolean;
}) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const policy = useQuery({ ...storagePolicyOptions(), enabled: active });
  const saved = policy.data;
  const [decision, setDecision] = useState<Decision>();
  const [custom, setCustom] = useState<string>();
  const refresh = () => {
    void client.invalidateQueries({ queryKey: storageKeys.policy });
  };
  const save = useMutation({
    mutationFn: ({
      enabled,
      days,
      revision,
    }: {
      enabled: boolean;
      days: number;
      revision: number;
    }) => savePolicy(enabled, days, revision),
    onSettled: refresh,
    onSuccess: () => setCustom(undefined),
  });
  const days = saved?.retentionDays ?? 15;
  function configure(enabled: boolean, nextDays: number) {
    if (!saved || saved.revision == null) return;
    save.reset();
    if (enabled && (!saved.enabled || nextDays < days)) {
      setDecision({
        kind: saved.enabled ? "shorten" : "enable",
        days: nextDays,
        revision: saved.revision,
      });
    } else save.mutate({ enabled, days: nextDays, revision: saved.revision });
  }
  const customDays = Number(custom);
  const valid =
    Number.isInteger(customDays) && customDays >= 1 && customDays <= 3650;
  const blocked =
    disabled || saved?.valid === false || save.isPending || Boolean(decision);
  return (
    <>
      <SettingsGroup title={t("policy")}>
        {policy.isPending ? <StorageLoading /> : null}
        {policy.isError ? (
          <StorageError
            error={policy.error}
            retry={() => void policy.refetch()}
            stale={Boolean(saved)}
          />
        ) : null}
        {saved ? (
          <SettingsCard>
            <SettingsRow
              grouped
              title={t("enabled")}
              description={
                saved.enabled && saved.nextCheckAt
                  ? t("nextCheck", { time: storageDate(saved.nextCheckAt) })
                  : undefined
              }
            >
              <Switch
                aria-label={t("enabled")}
                checked={Boolean(saved.enabled)}
                disabled={blocked}
                onClick={() => configure(!saved.enabled, days)}
              />
            </SettingsRow>
            <SettingsRow grouped title={t("days")}>
              <Select
                value={
                  custom !== undefined || !presets.includes(days)
                    ? "custom"
                    : String(days)
                }
                disabled={blocked}
                onValueChange={(value) => {
                  if (value === "custom") {
                    setCustom(String(days));
                    return;
                  }
                  setCustom(undefined);
                  configure(Boolean(saved.enabled), Number(value));
                }}
              >
                <SelectTrigger
                  aria-label={t("days")}
                  appearance="settings"
                  size="settings"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {presets.map((value) => (
                    <SelectItem key={value} value={String(value)}>
                      {t("daysValue", { days: value })}
                    </SelectItem>
                  ))}
                  <SelectItem value="custom">{t("custom")}</SelectItem>
                </SelectContent>
              </Select>
              {custom !== undefined || !presets.includes(days) ? (
                <label className="flex min-w-0 flex-wrap items-center gap-2">
                  <span className="sr-only">{t("customDays")}</span>
                  <Input
                    type="number"
                    min={1}
                    max={3650}
                    step={1}
                    placeholder="15"
                    aria-label={t("customDays")}
                    value={custom ?? String(days)}
                    disabled={blocked}
                    onChange={(event) => setCustom(event.target.value)}
                  />
                  <Button
                    size="settings"
                    variant="settings-outline"
                    disabled={blocked || !valid || customDays === days}
                    onClick={() =>
                      configure(Boolean(saved.enabled), customDays)
                    }
                  >
                    {t("save")}
                  </Button>
                </label>
              ) : null}
            </SettingsRow>
            {custom !== undefined && !valid ? (
              <p role="alert" className="type-ui text-danger px-4 pb-3">
                {t("range")}
              </p>
            ) : null}
          </SettingsCard>
        ) : null}
        {saved?.valid === false ? (
          <p role="alert" className="type-body text-danger">
            {t("policyInvalid")}
          </p>
        ) : null}
        {save.isError ? <StorageError error={save.error} /> : null}
      </SettingsGroup>
      {decision ? (
        <RetentionConfirm
          decision={decision}
          disabled={disabled}
          onClose={() => {
            setDecision(undefined);
            void policy.refetch();
          }}
          onDone={() => {
            setDecision(undefined);
            setCustom(undefined);
            refresh();
          }}
        />
      ) : null}
    </>
  );
}
