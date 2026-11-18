import { useDialogFocusRestore } from "@/shared/ui/use-dialog-focus-restore";
import type { DesktopStorageCategory } from "@neuxbot/platform-contracts/services";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { platform } from "@/shared/platform/provider";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  cleanupCategories,
  clearResources,
  reviewResources,
  storageKeys,
  storageOptions,
  readUsage,
  type CleanupCategory,
} from "../api/storage";
import { storageSize, StorageError, StorageLoading } from "./storage-feedback";
import {
  SettingsGroup,
  SettingsCard,
  SettingsRow,
} from "./settings-section-layout";

type Category = CleanupCategory | DesktopStorageCategory;
type Item = { category: Category; bytes: number; available: boolean };
type Receipt = {
  category: Category;
  failed: boolean;
  removedBytes?: number;
  error?: unknown;
};
function isCore(value: unknown): value is CleanupCategory {
  return value === "caches" || value === "logs" || value === "runtimes";
}
function isDesktop(value: Category): value is DesktopStorageCategory {
  return value === "desktop-cache" || value === "desktop-logs";
}

async function previewCore(categories: CleanupCategory[]) {
  const preview = await reviewResources(categories);
  const items: Item[] = (preview.items ?? []).flatMap((row) =>
    isCore(row.category)
      ? [
          {
            category: row.category,
            available: Boolean(row.available),
            bytes: row.bytes ?? 0,
          },
        ]
      : [],
  );
  return { items, id: preview.id };
}
async function previewDesktop(): Promise<Item[]> {
  const host = platform().host;
  if (!host.preview_desktop_storage || !host.clear_desktop_storage) return [];
  const preview = await host.preview_desktop_storage();
  return preview.items.map((row) => ({ ...row, available: true }));
}

function CleanupConfirmation({
  selection,
  disabled,
  onDone,
  onClose,
}: {
  selection: Category[];
  disabled: boolean;
  onDone: (result: { items: Receipt[]; runId?: string }) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("storage");
  const focus = useDialogFocusRestore();
  const preview = useQuery({
    queryKey: ["storage", "confirm-resources", selection],
    gcTime: 0,
    staleTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const core = selection.filter(isCore);
      const [server, host] = await Promise.all([
        core.length ? previewCore(core) : undefined,
        selection.some(isDesktop) ? previewDesktop() : [],
      ]);
      return {
        id: server?.id,
        items: [
          ...(server?.items ?? []),
          ...host.filter((row) => selection.includes(row.category)),
        ],
      };
    },
  });
  const submit = useMutation({
    mutationFn: async () => {
      const confirmed = preview.data;
      if (!confirmed) throw Error();
      const receipts: Receipt[] = [];
      let runId: string | undefined;
      const core = confirmed.items.filter(
        (row) => isCore(row.category) && row.available && row.bytes > 0,
      );
      if (core.length && confirmed.id) {
        try {
          runId = (await clearResources(confirmed.id)).runId;
          receipts.push(
            ...core.map(({ category }) => ({ category, failed: false })),
          );
        } catch (error) {
          receipts.push(
            ...core.map(({ category }) => ({ category, failed: true, error })),
          );
        }
      }
      for (const row of confirmed.items) {
        if (!isDesktop(row.category) || !row.available || row.bytes <= 0)
          continue;
        try {
          const clear = platform().host.clear_desktop_storage;
          if (!clear) throw Error();
          const result = await clear({ category: row.category });
          receipts.push({
            category: row.category,
            removedBytes: result.removedBytes,
            failed: false,
          });
        } catch (error) {
          receipts.push({ category: row.category, failed: true, error });
        }
      }
      return { items: receipts, runId };
    },
    onSuccess: onDone,
  });
  const actionable = preview.data?.items.some(
    (row) =>
      row.available &&
      row.bytes > 0 &&
      (isDesktop(row.category) || preview.data?.id),
  );
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !submit.isPending) onClose();
      }}
    >
      <DialogContent
        {...focus}
        title={t("confirmResources")}
        description={t("resourceImpact")}
      >
        <div className="grid gap-2 px-6 pb-4">
          {preview.isFetching ? <StorageLoading /> : null}
          {preview.isError ? (
            <StorageError
              error={preview.error}
              retry={() => void preview.refetch()}
            />
          ) : null}
          {preview.data?.items.map((row) => (
            <p
              key={row.category}
              className="type-body flex justify-between gap-4"
            >
              {t("resources." + row.category)}
              <span>
                {row.available ? storageSize(row.bytes) : t("notAvailable")}
              </span>
            </p>
          ))}
          {submit.isError ? <StorageError error={submit.error} /> : null}
        </div>
        <DialogFooter>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={submit.isPending}
            onClick={onClose}
          >
            {t("cancel")}
          </Button>
          <Button
            size="settings"
            variant="danger"
            disabled={
              disabled ||
              submit.isPending ||
              preview.isFetching ||
              preview.isError ||
              !actionable
            }
            onClick={() => submit.mutate()}
          >
            {t("clear")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function StorageCleanup({
  disabled,
  active,
  last,
  onStarted,
}: {
  disabled: boolean;
  active: boolean;
  onStarted: (id: string) => void;
  last?: {
    id?: string;
    items?: Array<{
      category?: string;
      failed?: boolean;
      removedBytes?: number;
    }>;
  } | null;
}) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const [selection, setSelection] = useState<Category[]>();
  const [receipts, setReceipts] = useState<Receipt[]>([]);
  const available = useQuery({
    queryKey: [...storageKeys.cleanup, "core"],
    queryFn: () => previewCore(cleanupCategories),
    enabled: active,
  });
  const host = platform().host;
  const desktop = useQuery({
    queryKey: [...storageKeys.cleanup, "desktop"],
    queryFn: previewDesktop,
    enabled:
      active &&
      Boolean(host.preview_desktop_storage && host.clear_desktop_storage),
  });
  const rows = [...(available.data?.items ?? []), ...(desktop.data ?? [])];
  const candidates = rows
    .filter((row) => row.available && row.bytes > 0)
    .map((row) => row.category);
  const failures = new Set<Category>(
    (last?.items ?? []).flatMap((row) =>
      row.failed && isCore(row.category) ? [row.category] : [],
    ),
  );
  for (const row of receipts) {
    if (row.failed) failures.add(row.category);
  }
  function done({
    items: results,
    runId,
  }: {
    items: Receipt[];
    runId?: string;
  }) {
    if (runId) onStarted(runId);
    setReceipts((previous) => [
      ...previous.filter(
        (row) => !results.some((result) => result.category === row.category),
      ),
      ...results,
    ]);
    setSelection(undefined);
    void client.invalidateQueries({ queryKey: storageKeys.maintenance });
    void client.invalidateQueries({ queryKey: storageKeys.cleanup });
    if (results.some((row) => isDesktop(row.category))) {
      void client
        .fetchQuery({
          ...storageOptions(),
          staleTime: 0,
          queryFn: ({ signal }) => readUsage(true, signal),
        })
        .catch(() => {});
    } else void client.invalidateQueries({ queryKey: storageKeys.usage });
  }
  const results = new Map<Category, Receipt>();
  for (const row of last?.items ?? []) {
    if (isCore(row.category))
      results.set(row.category, {
        category: row.category,
        failed: Boolean(row.failed),
        removedBytes: row.removedBytes,
      });
  }
  for (const row of receipts) {
    if (row.failed || row.removedBytes != null) results.set(row.category, row);
  }
  return (
    <SettingsGroup
      title={t("cleanup")}
      trailing={
        candidates.length ? (
          <Button
            size="settings"
            variant="settings-outline"
            disabled={disabled}
            onClick={() => setSelection(candidates)}
          >
            {t("resourceAll")}
          </Button>
        ) : null
      }
    >
      {available.isPending ? <StorageLoading /> : null}
      {available.isError ? (
        <StorageError
          error={available.error}
          retry={() => void available.refetch()}
          stale={Boolean(available.data)}
        />
      ) : null}
      {desktop.isError ? (
        <StorageError
          error={desktop.error}
          retry={() => void desktop.refetch()}
          stale={Boolean(desktop.data)}
        />
      ) : null}
      {rows.length ? (
        <SettingsCard>
          {rows.map((row) => (
            <SettingsRow
              grouped
              key={row.category}
              title={t("resources." + row.category)}
            >
              <span className="type-body text-content-secondary tabular-nums">
                {row.available ? storageSize(row.bytes) : t("notAvailable")}
              </span>
              {row.available && row.bytes > 0 ? (
                <Button
                  size="settings"
                  variant="settings-outline"
                  disabled={disabled}
                  onClick={() => setSelection([row.category])}
                >
                  {t("review")}
                </Button>
              ) : null}
            </SettingsRow>
          ))}
        </SettingsCard>
      ) : available.data ? (
        <p className="type-body text-content-secondary">{t("noCleanup")}</p>
      ) : null}
      {Array.from(results.values()).map((row) => (
        <p
          key={row.category}
          role={row.failed ? "alert" : "status"}
          className="type-body text-content-secondary"
        >
          {t("resources." + row.category)} ·{" "}
          {row.failed
            ? localApiErrorMessage(row.error, t("cleanupFailed"))
            : t("removed", { size: storageSize(row.removedBytes ?? 0) })}
        </p>
      ))}
      {failures.size ? (
        <div>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={disabled}
            onClick={() => setSelection([...failures])}
          >
            {t("retryCategories")}
          </Button>
        </div>
      ) : null}
      {selection ? (
        <CleanupConfirmation
          selection={selection}
          disabled={disabled}
          onClose={() => setSelection(undefined)}
          onDone={done}
        />
      ) : null}
    </SettingsGroup>
  );
}
