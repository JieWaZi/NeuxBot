import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { useState } from "react";
import { KeyRound, Pencil, Trash2 } from "lucide-react";
import { deleteIntegrationKey } from "@/shared/api/integration-service";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsIntegrationKey as APIKey } from "@/shared/api/generated/types.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Pagination } from "@/shared/ui/pagination";
import { QueryState } from "@/shared/ui/query-state";
import { Skeleton } from "@/shared/ui/skeleton";
import { PageAction } from "@/shared/ui/resource-page";
import { apiKeysOptions, apiKeysQueryKey } from "../api/integration-queries";
import { SettingsContentShell, SettingsGroup } from "./settings-section-layout";
import { MCPIntegrationSection } from "./mcp-integration-section";
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
    <SettingsContentShell
      title={t("title")}
      description={t("description")}
      actions={
        <PageAction
          intent="primary"
          label={t("create")}
          onClick={() => setDialog({ action: "create" })}
        />
      }
    >
      <APIServiceSettings />
      <MCPIntegrationSection />
      <SettingsGroup
        title={
          <span className="type-label text-content-primary font-medium">
            {t("keys")}
          </span>
        }
        description={t("keysHelp")}
        trailing={
          query.data ? (
            <span className="type-ui text-content-secondary tabular-nums">
              {query.data.totalSize ?? 0}
            </span>
          ) : undefined
        }
      >
        {query.isPending ? (
          <div role="status" aria-busy="true" className="grid gap-3">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : query.error && !query.data ? (
          <QueryState
            error={query.error}
            onRetry={() => void query.refetch()}
          />
        ) : (
          <>
            {!entries.length && (
              <Card variant="section">
                <div
                  className="flex items-start gap-4 py-4"
                  data-od-id="api-keys-empty"
                >
                  <KeyRound
                    className="size-icon-lg text-content-secondary shrink-0"
                    aria-hidden="true"
                  />
                  <div className="grid min-w-0 gap-1">
                    <p className="type-body font-medium">{t("empty")}</p>
                    <p className="type-ui text-content-secondary">
                      {t("emptyHelp")}
                    </p>
                  </div>
                </div>
              </Card>
            )}
            {entries.length > 0 && (
              <TooltipProvider>
                <Table aria-label={t("keys")} data-od-id="api-keys-list">
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("name")}</TableHead>
                      <TableHead>{t("key")}</TableHead>
                      <TableHead>{t("created")}</TableHead>
                      <TableHead>{t("lastUsed")}</TableHead>
                      <TableHead>{t("expires")}</TableHead>
                      <TableHead>
                        <span className="sr-only">{t("actions")}</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {entries.map((entry) => (
                      <TableRow key={entry.id} aria-label={entry.name}>
                        <TableCell>
                          <span className="font-medium break-all">
                            {entry.name}
                          </span>
                        </TableCell>
                        <TableCell>
                          <code className="type-ui text-content-secondary whitespace-nowrap">
                            {entry.displayPrefix}…
                          </code>
                        </TableCell>
                        <TableCell className="type-ui text-content-secondary whitespace-nowrap">
                          {date(entry.createdAt) || "—"}
                        </TableCell>
                        <TableCell className="type-ui text-content-secondary whitespace-nowrap">
                          {date(entry.lastUsedAt) || t("neverUsed")}
                        </TableCell>
                        <TableCell className="type-ui text-content-secondary whitespace-nowrap">
                          {date(entry.expiresAt) || t("noExpiry")}
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center justify-end gap-1">
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
                                      onClick={() =>
                                        setDialog({ action, entry })
                                      }
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
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TooltipProvider>
            )}
            {(query.data?.totalSize ?? 0) > 0 && (
              <div className="min-w-0 overflow-x-auto">
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
            {query.error && (
              <QueryState
                error={query.error}
                onRetry={() => void query.refetch()}
              />
            )}
          </>
        )}
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
