import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { deleteIntegrationKey } from "@/shared/api/integration-service";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsIntegrationKey as APIKey } from "@/shared/api/generated/types.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { Button } from "@/shared/ui/button";
import { SettingsCard } from "@/modules/settings";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Pagination } from "@/shared/ui/pagination";
import { StatusPill } from "@/shared/ui/status-pill";
import { Skeleton } from "@/shared/ui/skeleton";
import { apiKeysOptions, apiKeysQueryKey } from "../api/integration-queries";
import { SettingsContentShell, SettingsGroup } from "./settings-section-layout";
import { APIServiceSettings } from "./api-service-settings";

import { useIntegrationCopy } from "../use-integration-copy";

import { APIKeyDialog } from "./api-key-dialog";

function APIKeyDeleteDialog({
  entry,
  onClose,
}: {
  entry: APIKey;
  onClose: () => void;
}) {
  const t = useIntegrationCopy();
  const client = useQueryClient();
  const deletion = useMutation({
    retry: false,
    mutationFn: () => deleteIntegrationKey(entry.id ?? "", entry.revision ?? 0),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: apiKeysQueryKey() });
      onClose();
    },
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !deletion.isPending) onClose();
      }}
    >
      <DialogContent title={t("deleteTitle")} description={t("deleteHelp")}>
        <div className="grid gap-3 px-6 pb-6">
          <p className="type-body font-medium break-all">{entry.name}</p>
          {deletion.error && (
            <p role="alert" className="type-body text-destructive">
              {localApiErrorMessage(deletion.error, t("failed"))}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            disabled={deletion.isPending}
            onClick={onClose}
          >
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={deletion.isPending}
            onClick={() => deletion.mutate()}
          >
            {t("confirmDelete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function APIIntegrationSection() {
  const t = useIntegrationCopy();
  const [params, setParams] = useSearchParams();
  const page = Math.max(1, Number(params.get("keysPage")) || 1);
  const requestedSize = Number(params.get("keysPageSize"));
  const size = [10, 30, 50].includes(requestedSize) ? requestedSize : 10;
  const query = useQuery(apiKeysOptions(page, size));
  const [openedAt] = useState(Date.now);
  const checkedAt = Math.max(openedAt, query.dataUpdatedAt);
  const entries = query.data?.resources ?? [];
  const [dialog, setDialog] = useState<{
    action: "edit" | "delete" | "create";
    entry?: APIKey;
  }>();
  const date = (value: number | undefined) =>
    value
      ? new Intl.DateTimeFormat(undefined, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(value)
      : "";
  function pagination(patch: Record<string, string>) {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(patch)) next.set(key, value);
    setParams(next);
  }
  return (
    <SettingsContentShell showHeader={false}>
      <APIServiceSettings />
      <SettingsGroup
        title={t("keys")}
        trailing={
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  variant="plain"
                  aria-label={t("create")}
                  onClick={() => setDialog({ action: "create" })}
                >
                  <Plus />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("create")}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        }
      >
        <SettingsCard data-od-id="api-keys-list">
          {query.isPending ? (
            <div
              role="status"
              aria-busy="true"
              aria-label={t("keys")}
              className="grid gap-3 px-4 py-3"
            >
              <Skeleton className="h-row-detail w-full" />
              <Skeleton className="h-row-detail w-full" />
            </div>
          ) : query.error && !query.data ? (
            <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
              <p role="alert" className="type-resource-body text-destructive">
                {localApiErrorMessage(query.error, t("readFailed"))}
              </p>
              <Button
                type="button"
                size="settings"
                variant="settings-outline"
                onClick={() => void query.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : (
            <>
              {!entries.length ? (
                <p
                  className="type-resource-body text-content-secondary px-4 py-3"
                  data-od-id="api-keys-empty"
                >
                  {t("empty")}
                </p>
              ) : (
                <TooltipProvider>
                  <ul aria-label={t("keys")}>
                    {entries.map((entry) => (
                      <li
                        key={entry.id}
                        aria-label={entry.name}
                        className="before:bg-border-subtle min-h-row-detail relative flex min-w-0 flex-wrap items-start gap-3 px-4 py-3 before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden"
                      >
                        <div className="min-w-0 flex-1 basis-48">
                          <div className="flex min-w-0 flex-wrap items-center gap-2">
                            <span className="type-label font-medium break-all">
                              {entry.name}
                            </span>
                            <code className="type-ui text-content-secondary break-all select-text">
                              {entry.displayPrefix}…
                            </code>
                            {entry.revokedAt ? (
                              <StatusPill tone="unavailable">
                                {t("revoked")}
                              </StatusPill>
                            ) : entry.expiresAt &&
                              entry.expiresAt <= checkedAt ? (
                              <StatusPill tone="attention">
                                {t("expired")}
                              </StatusPill>
                            ) : entry.enabled === false ? (
                              <StatusPill tone="unavailable">
                                {t("inactive")}
                              </StatusPill>
                            ) : null}
                          </div>
                          <dl className="type-ui text-content-secondary mt-1 flex min-w-0 flex-wrap gap-x-4 gap-y-1">
                            {(
                              [
                                ["created", date(entry.createdAt) || "—"],
                                [
                                  "lastUsed",
                                  date(entry.lastUsedAt) || t("neverUsed"),
                                ],
                                [
                                  "expires",
                                  date(entry.expiresAt) || t("noExpiry"),
                                ],
                              ] as const
                            ).map(([label, value]) => (
                              <div
                                key={label}
                                className="flex min-w-0 flex-wrap gap-x-2"
                              >
                                <dt>{t(label)}</dt>
                                <dd>{value}</dd>
                              </div>
                            ))}
                          </dl>
                        </div>
                        <div className="flex max-w-full flex-wrap items-center gap-1">
                          {(["edit", "delete"] as const).map((action) =>
                            action === "edit" && entry.revokedAt ? null : (
                              <Tooltip key={action}>
                                <TooltipTrigger asChild>
                                  <Button
                                    type="button"
                                    size="icon"
                                    variant={
                                      action === "delete"
                                        ? "danger-quiet"
                                        : "ghost"
                                    }
                                    aria-label={t(action)}
                                    onClick={() => setDialog({ action, entry })}
                                  >
                                    {action === "edit" ? (
                                      <Pencil aria-hidden="true" />
                                    ) : (
                                      <Trash2 aria-hidden="true" />
                                    )}
                                  </Button>
                                </TooltipTrigger>
                                <TooltipContent>{t(action)}</TooltipContent>
                              </Tooltip>
                            ),
                          )}
                        </div>
                      </li>
                    ))}
                  </ul>
                </TooltipProvider>
              )}
              {(query.data?.totalSize ?? 0) > 0 && (
                <div className="min-w-0 overflow-x-auto px-4 py-3">
                  <Pagination
                    labels={{
                      navigation: t("pagination.navigation"),
                      previous: t("pagination.previous"),
                      next: t("pagination.next"),
                      page: (page) => t("pagination.page", { page }),
                      pageSize: (count) => t("pagination.pageSize", { count }),
                      pageSizeSelection: t("pagination.pageSizeSelection"),
                      total: (count) => t("pagination.total", { count }),
                    }}
                    page={page}
                    pageSize={size}
                    pageSizeOptions={[10, 30, 50]}
                    totalItems={query.data?.totalSize ?? 0}
                    pending={query.isFetching}
                    onPageChange={(page) =>
                      pagination({ keysPage: String(page) })
                    }
                    onPageSizeChange={(size) =>
                      pagination({ keysPage: "1", keysPageSize: String(size) })
                    }
                  />
                </div>
              )}
              {query.error ? (
                <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
                  <p
                    role="alert"
                    className="type-resource-body text-destructive"
                  >
                    {localApiErrorMessage(query.error, t("readFailed"))}
                  </p>
                  <Button
                    type="button"
                    size="settings"
                    variant="settings-outline"
                    disabled={query.isFetching}
                    onClick={() => void query.refetch()}
                  >
                    {t("retry")}
                  </Button>
                </div>
              ) : null}
            </>
          )}
        </SettingsCard>
      </SettingsGroup>
      {(dialog?.action === "create" || dialog?.action === "edit") && (
        <APIKeyDialog
          entry={dialog.entry}
          onClose={() => setDialog(undefined)}
        />
      )}
      {dialog?.action === "delete" && dialog.entry && (
        <APIKeyDeleteDialog
          entry={dialog.entry}
          onClose={() => setDialog(undefined)}
        />
      )}
    </SettingsContentShell>
  );
}
