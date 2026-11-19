import { useDialogFocusRestore } from "@/shared/ui/use-dialog-focus-restore";
import {
  useQuery,
  useQueries,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { platform } from "@/shared/platform/provider";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import {
  cleanupCategories,
  storageKeys,
  storageOptions,
  readUsage,
} from "../api/storage";
import {
  cleanupItemEmpty,
  confirmStorageCleanup,
  desktopStorageCategories,
  isCoreCleanup,
  isDesktopCleanup,
  previewCoreCleanup,
  previewDesktopCleanup,
  type CleanupReceipt,
  type StorageCleanupCategory,
} from "../api/storage-cleanup";
import { storageSize, StorageError, StorageLoading } from "./storage-feedback";
import {
  SettingsGroup,
  SettingsCard,
  SettingsRow,
} from "./settings-section-layout";

type Selection = {
  categories: StorageCleanupCategory[];
  retry?: boolean;
  opener: HTMLButtonElement;
  previousLastId?: string;
};
type MaintenanceRun = {
  id?: string;
  items?: Array<{ category?: string; failed?: boolean; removedBytes?: number }>;
};
type CleanupFailure = { error?: unknown; unavailable?: boolean };
type TrackedReceipt = {
  result: CleanupReceipt;
  previousLastId?: string;
};
const resourceGroups = ["cache", "diagnostics", "programs"] as const;
const resourceGroupByCategory: Record<
  StorageCleanupCategory,
  (typeof resourceGroups)[number]
> = {
  caches: "cache",
  "desktop-cache": "cache",
  logs: "diagnostics",
  "desktop-logs": "diagnostics",
  runtimes: "programs",
};

function settleReceipt(
  receipt: TrackedReceipt,
  last: MaintenanceRun | null | undefined,
): TrackedReceipt {
  const { result } = receipt;
  if (result.state !== "accepted" || last?.id !== result.runId) return receipt;
  const terminal = last.items?.find((row) => row.category === result.category);
  const removed = terminal?.removedBytes;
  return {
    previousLastId: last.id,
    result:
      terminal?.failed === false &&
      typeof removed === "number" &&
      Number.isFinite(removed) &&
      removed >= 0
        ? { category: result.category, state: "cleared", removedBytes: removed }
        : { category: result.category, state: "failed" },
  };
}

function CleanupConfirmation({
  selection,
  disabled,
  onDone,
  onClose,
  returnFocusTo,
}: {
  selection: Selection;
  disabled: boolean;
  onDone: (results: CleanupReceipt[]) => void;
  onClose: () => void;
  returnFocusTo: () => HTMLElement | null;
}) {
  const { t, i18n } = useTranslation("storage");
  const focus = useDialogFocusRestore(returnFocusTo);
  const { categories, retry } = selection;
  const submit = useMutation({
    mutationFn: () => confirmStorageCleanup(categories),
    onSuccess: onDone,
  });
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
        description={t(retry ? "resourceRetryImpact" : "resourceImpact", {
          resources: new Intl.ListFormat(i18n.resolvedLanguage).format(
            Array.from(
              new Set(
                categories.map((category) => resourceGroupByCategory[category]),
              ),
              (group) => t(`resources.${group}`),
            ),
          ),
        })}
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
            {t("cancel")}
          </Button>
          <Button
            size="settings"
            variant="danger"
            disabled={disabled || submit.isPending}
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
  coreBusy,
  active,
  last,
  currentRunId,
  awaitingRunId,
  onStarted,
}: {
  disabled: boolean;
  coreBusy: boolean;
  active: boolean;
  onStarted: (id: string) => void;
  last?: MaintenanceRun | null;
  currentRunId?: string;
  awaitingRunId?: string;
}) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const [selection, setSelection] = useState<Selection>();
  const heading = useRef<HTMLSpanElement>(null);
  const all = useRef<HTMLDivElement>(null);
  const [receipts, setReceipts] = useState<TrackedReceipt[]>([]);
  // Core 只保留最近一项维护。读到本次终态后保留结果，避免后续任务覆盖。
  const settledReceipts = receipts.map((receipt) =>
    settleReceipt(receipt, last),
  );
  if (settledReceipts.some((receipt, index) => receipt !== receipts[index])) {
    setReceipts(settledReceipts);
  }
  const available = useQuery({
    queryKey: [...storageKeys.cleanup, "core"],
    queryFn: ({ signal }) => previewCoreCleanup(cleanupCategories, signal),
    enabled: active,
  });
  const host = platform().host;
  const desktopAvailable = Boolean(
    host.preview_desktop_storage && host.clear_desktop_storage,
  );
  const desktop = useQueries({
    queries: desktopStorageCategories.map((category) => ({
      queryKey: [...storageKeys.cleanup, category],
      queryFn: () => previewDesktopCleanup(category),
      enabled: active && desktopAvailable,
    })),
  });
  const loading =
    active &&
    (available.isPending ||
      (desktopAvailable && desktop.some((query) => query.isPending)));
  const previewError =
    available.isError ||
    (desktopAvailable && desktop.some((query) => query.isError));
  const items = [
    ...(available.data?.items ?? []),
    ...(desktopAvailable
      ? desktop.flatMap((query) => (query.data ? [query.data] : []))
      : []),
  ];
  const incomplete = items.some((item) => !item.complete);
  const candidates = items.filter((row) => row.available && row.bytes > 0);
  const candidateCategories = candidates.map((row) => row.category);
  const rows = resourceGroups
    .map((group) => {
      const items = candidates.filter(
        ({ category }) => resourceGroupByCategory[category] === group,
      );
      return {
        group,
        categories: items.map((item) => item.category),
        bytes: items.reduce((total, item) => total + item.bytes, 0),
      };
    })
    .filter((row) => row.categories.length > 0);
  const reclaimableBytes = rows.reduce((total, row) => total + row.bytes, 0);
  const failures = new Map<StorageCleanupCategory, CleanupFailure>();
  for (const row of last?.items ?? []) {
    if (
      row.failed &&
      isCoreCleanup(row.category) &&
      !cleanupItemEmpty(items.find((item) => item.category === row.category))
    ) {
      failures.set(row.category, {});
    }
  }
  for (const { result, previousLastId } of settledReceipts) {
    const category = result.category;
    if (result.state === "accepted") {
      if (currentRunId === result.runId || awaitingRunId === result.runId)
        failures.delete(category);
      else failures.set(category, { unavailable: true });
      continue;
    }
    if (
      isCoreCleanup(category) &&
      last?.id !== previousLastId &&
      last?.items?.some((row) => row.category === category)
    ) {
      continue;
    }
    if (result.state === "failed")
      failures.set(category, { error: result.error });
    else failures.delete(category);
  }
  const failedCategories = [...failures.keys()];
  function blocked(categories: StorageCleanupCategory[]) {
    return disabled || (coreBusy && !categories.some(isDesktopCleanup));
  }
  function open(
    opener: HTMLButtonElement,
    categories: StorageCleanupCategory[],
    retry = false,
  ) {
    setSelection({ opener, categories, retry, previousLastId: last?.id });
  }
  function done(results: CleanupReceipt[]) {
    const accepted = results.find((row) => row.state === "accepted");
    if (accepted) onStarted(accepted.runId);
    if (selection) {
      const previousLastId = selection.previousLastId;
      setReceipts((previous) => [
        ...previous.filter(
          ({ result }) =>
            !results.some((row) => row.category === result.category),
        ),
        ...results.map((result) => ({ result, previousLastId })),
      ]);
    }
    setSelection(undefined);
    if (!accepted)
      void client.invalidateQueries({ queryKey: storageKeys.maintenance });
    void client.invalidateQueries({ queryKey: storageKeys.cleanup });
    if (
      results.some(
        (row) => isDesktopCleanup(row.category) && row.state !== "empty",
      )
    ) {
      void client
        .fetchQuery({
          ...storageOptions(),
          staleTime: 0,
          queryFn: ({ signal }) => readUsage(true, signal),
        })
        .catch(() => {});
    } else void client.invalidateQueries({ queryKey: storageKeys.usage });
  }
  return (
    <SettingsGroup
      title={
        <span ref={heading} tabIndex={-1} className="outline-none">
          {t("cleanup")}
        </span>
      }
      trailing={
        rows.length ? (
          <div ref={all} className="flex flex-wrap items-center gap-2">
            <span className="type-body text-content-secondary tabular-nums">
              {t(
                loading || previewError || incomplete
                  ? "knownReclaimable"
                  : "reclaimable",
                {
                  size: storageSize(reclaimableBytes),
                },
              )}
            </span>
            <Button
              size="settings"
              variant="settings-outline"
              disabled={blocked(candidateCategories)}
              onClick={(event) =>
                open(event.currentTarget, candidateCategories)
              }
            >
              {t("resourceAll")}
            </Button>
          </div>
        ) : null
      }
    >
      {loading ? <StorageLoading /> : null}
      {available.isError ? (
        <StorageError
          error={available.error}
          retry={() => void available.refetch()}
          stale={Boolean(available.data)}
        />
      ) : null}
      {desktopAvailable
        ? desktop.map((query, index) =>
            query.isError ? (
              <StorageError
                key={desktopStorageCategories[index]}
                error={query.error}
                retry={() => void query.refetch()}
                stale={Boolean(query.data)}
              />
            ) : null,
          )
        : null}
      {rows.length ? (
        <TooltipProvider>
          <SettingsCard>
            {rows.map((row) => {
              const title = t(`resources.${row.group}`);
              const label = t("clearItemLabel", { name: title });
              return (
                <SettingsRow
                  grouped
                  key={row.group}
                  title={title}
                  description={t(`resourceDescription.${row.group}`)}
                >
                  <span className="type-body text-content-secondary tabular-nums">
                    {storageSize(row.bytes)}
                  </span>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        size="icon"
                        variant="danger-quiet"
                        aria-label={label}
                        disabled={blocked(row.categories)}
                        onClick={(event) =>
                          open(event.currentTarget, row.categories)
                        }
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{label}</TooltipContent>
                  </Tooltip>
                </SettingsRow>
              );
            })}
          </SettingsCard>
        </TooltipProvider>
      ) : !loading && !previewError && available.data ? (
        <p className="type-body text-content-secondary">
          {t(incomplete ? "cleanupIncomplete" : "noCleanup")}
        </p>
      ) : null}
      {resourceGroups.map((group) => {
        const messages = new Set<string>();
        for (const [category, failure] of failures) {
          if (resourceGroupByCategory[category] === group) {
            messages.add(
              failure.unavailable
                ? t("cleanupOutcomeUnknown")
                : localApiErrorMessage(failure.error, t("cleanupFailed")),
            );
          }
        }
        if (!messages.size) return null;
        return (
          <p
            key={group}
            role="alert"
            className="type-body text-content-secondary"
          >
            {t(`resources.${group}`)} · {Array.from(messages).join(" · ")}
          </p>
        );
      })}
      {failures.size ? (
        <div>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={blocked(failedCategories)}
            onClick={(event) =>
              open(event.currentTarget, failedCategories, true)
            }
          >
            {t("retryCategories")}
          </Button>
        </div>
      ) : null}
      {selection ? (
        <CleanupConfirmation
          selection={selection}
          disabled={blocked(selection.categories)}
          onClose={() => setSelection(undefined)}
          onDone={done}
          returnFocusTo={() => {
            if (selection.opener.isConnected && !selection.opener.disabled)
              return selection.opener;
            return (
              all.current?.querySelector<HTMLElement>(
                "button:not(:disabled)",
              ) ?? heading.current
            );
          }}
        />
      ) : null}
    </SettingsGroup>
  );
}
