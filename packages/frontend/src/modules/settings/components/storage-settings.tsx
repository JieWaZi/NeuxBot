import {
  useMutation,
  useQuery,
  useQueryClient,
  useInfiniteQuery,
} from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { expiredChatsKey } from "@/shared/api/chat-protection";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { localizedNumberFormat } from "@/shared/i18n/format";
import { chatHref } from "@/shared/lib/chat-navigation";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  readUsage,
  storageOptions,
  storageEntitiesOptions,
  storageKeys,
  storageMaintenanceOptions,
} from "../api/storage";
import {
  SettingsContentShell,
  SettingsGroup,
  SettingsCard,
  SettingsNavigationRow,
} from "./settings-section-layout";
import {
  StorageError,
  StorageLoading,
  storageSize,
  storageDate,
} from "./storage-feedback";
import { StorageCleanup } from "./storage-cleanup-dialog";
import { StorageRetentionSettings } from "./storage-retention-settings";

const colors: Record<string, string> = {
  work_files: "bg-accent",
  conversations: "bg-accent-secondary",
  shared_native: "bg-status-info-content",
  knowledge: "bg-success",
  programs: "bg-warning",
  accounts: "bg-status-warning-content",
  cache: "bg-status-success-content",
  logs: "bg-content-secondary",
  temporary: "bg-content-muted",
  recovery: "bg-danger",
  app_data: "bg-primary",
  other: "bg-border",
};
const percent = localizedNumberFormat({
  style: "percent",
  maximumFractionDigits: 1,
});

function StorageOverview({ active }: { active: boolean }) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const usage = useQuery({ ...storageOptions(), enabled: active });
  const scan = useMutation({
    mutationFn: async () => {
      await client.cancelQueries({ queryKey: storageKeys.usage, exact: true });
      await client.fetchQuery({
        ...storageOptions(),
        staleTime: 0,
        queryFn: ({ signal }) => readUsage(true, signal),
      });
      await client.invalidateQueries({ queryKey: storageKeys.entities });
    },
  });
  const [highlight, setHighlight] = useState<string>();
  const data = usage.data;
  const rows = (data?.categories ?? []).filter((row) => row.category);
  const total = data?.totalBytes ?? 0;
  return (
    <SettingsGroup
      title={t("usage")}
      trailing={
        <Button
          size="settings"
          variant="settings-outline"
          disabled={scan.isPending || usage.isFetching}
          onClick={() => scan.mutate()}
        >
          <RefreshCw />
          {t(scan.isPending ? "scanning" : "rescan")}
        </Button>
      }
    >
      {usage.isPending ? <StorageLoading /> : null}
      {usage.isError ? (
        <StorageError
          error={usage.error}
          retry={() => scan.mutate()}
          stale={Boolean(data)}
        />
      ) : null}
      {data ? (
        <SettingsCard>
          <div className="grid gap-3 p-4">
            <div>
              <p className="type-display text-content-primary font-semibold tabular-nums">
                {data.complete
                  ? storageSize(total)
                  : t("partial", { size: storageSize(total) })}
              </p>
              {data.scannedAt ? (
                <p className="type-ui text-content-secondary">
                  {t("scanned", { time: storageDate(data.scannedAt) })}
                </p>
              ) : null}
            </div>
            {total > 0 ? (
              <div aria-hidden="true" className="flex h-2.5 w-full gap-0.5">
                {rows
                  .filter((row) => (row.bytes ?? 0) > 0)
                  .map((row) => (
                    <span
                      key={row.category}
                      className={cn(
                        "duration-quick min-w-px rounded-xs transition-opacity motion-reduce:transition-none",
                        colors[row.category ?? "other"] ?? colors.other,
                        highlight && highlight !== row.category && "opacity-40",
                      )}
                      style={{ flexGrow: row.bytes, flexBasis: 0 }}
                    />
                  ))}
              </div>
            ) : null}
            <div className="type-ui text-content-secondary grid gap-0.5 break-all">
              {Array.from(
                new Set([data.dataRoot, data.managedRoot].filter(Boolean)),
              ).map((path) => (
                <p key={path}>
                  {[
                    data.dataRoot === path ? t("dataRoot") : null,
                    data.managedRoot === path ? t("managedRoot") : null,
                  ]
                    .filter(Boolean)
                    .join(" · ")}{" "}
                  · {path}
                </p>
              ))}
              {(data.disks ?? []).map((disk) => (
                <p key={disk.path}>
                  {disk.freeBytes == null
                    ? disk.path
                    : t("free", {
                        path: (disk.paths?.length
                          ? disk.paths
                          : [disk.path]
                        ).join(" · "),
                        size: storageSize(disk.freeBytes),
                      })}
                </p>
              ))}
            </div>
          </div>
          {rows.length ? (
            <div
              aria-label={t("categories")}
              className="grid gap-x-5 px-4 pb-3 @[42.5rem]/settings-scene:grid-cols-2"
            >
              {rows.map((row) => {
                const key = row.category ?? "other";
                return (
                  <details
                    key={key}
                    className="min-w-0"
                    onMouseEnter={() => setHighlight(key)}
                    onMouseLeave={() => setHighlight(undefined)}
                    onFocus={() => setHighlight(key)}
                    onBlur={() => setHighlight(undefined)}
                  >
                    <summary className="focus-visible:ring-ring hover:bg-action-quiet-hover type-body flex cursor-pointer list-none items-center gap-2 rounded-sm py-1 outline-none focus-visible:ring-2">
                      <span
                        aria-hidden="true"
                        className={cn(
                          "size-2 shrink-0 rounded-full",
                          colors[key] ?? colors.other,
                        )}
                      />
                      <span className="min-w-0 flex-1">
                        {t("category." + key, {
                          defaultValue: t("category.other"),
                        })}
                      </span>
                      <span className="tabular-nums">
                        {storageSize(row.bytes ?? 0)}
                      </span>
                      {total > 0 ? (
                        <span className="text-content-secondary type-ui tabular-nums">
                          {percent.format((row.bytes ?? 0) / total)}
                        </span>
                      ) : null}
                    </summary>
                    <ul className="text-content-secondary type-ui grid gap-1 px-4 py-1 break-all">
                      {Array.from(new Set(row.paths ?? [])).map((path) => (
                        <li key={path}>{path}</li>
                      ))}
                    </ul>
                  </details>
                );
              })}
            </div>
          ) : data.complete ? (
            <p className="type-body px-4 pb-3">{t("empty")}</p>
          ) : null}
        </SettingsCard>
      ) : null}
    </SettingsGroup>
  );
}

function EntityUsage({
  kind,
  active,
}: {
  kind: "wakers" | "chats";
  active: boolean;
}) {
  const { t } = useTranslation("storage");
  const navigate = useNavigate();
  const list = useInfiniteQuery({
    ...storageEntitiesOptions(kind),
    enabled: active,
  });
  const rows = (list.data?.pages ?? []).flatMap((page) => page.items ?? []);
  if (!list.isPending && !list.isError && !rows.length) return null;
  return (
    <SettingsGroup title={t(kind)}>
      {list.isPending ? <StorageLoading /> : null}
      {list.isError ? (
        <StorageError
          error={list.error}
          retry={() => void list.refetch()}
          stale={rows.length > 0}
        />
      ) : null}
      {rows.length ? (
        <SettingsCard>
          {rows.map((row) => {
            const id = row.id ?? "";
            const largest = rows[0]?.bytes ?? 0;
            const description =
              kind === "chats"
                ? row.wakerName
                : [
                    row.workspaceBytes
                      ? t("workspaceSize", {
                          size: storageSize(row.workspaceBytes),
                        })
                      : null,
                    row.chatBytes
                      ? t("chatSize", { size: storageSize(row.chatBytes) })
                      : null,
                    row.internalBytes
                      ? t("internalSize", {
                          size: storageSize(row.internalBytes),
                        })
                      : null,
                  ]
                    .filter(Boolean)
                    .join(" · ");
            return (
              <SettingsNavigationRow
                key={id}
                title={
                  row.title ||
                  t(kind === "chats" ? "chatFallback" : "wakerFallback")
                }
                description={description}
                onClick={() =>
                  navigate(
                    kind === "chats"
                      ? chatHref(id, row.wakerId ?? "")
                      : "/wakers/" + encodeURIComponent(id),
                  )
                }
                trailing={
                  <>
                    {kind === "wakers" && largest > 0 ? (
                      <span
                        aria-hidden="true"
                        className="bg-muted hidden h-1.5 w-16 overflow-hidden rounded-full @[42.5rem]/settings-scene:block"
                      >
                        <span
                          className="bg-accent block h-full rounded-full"
                          style={{
                            width:
                              String((100 * (row.bytes ?? 0)) / largest) + "%",
                          }}
                        />
                      </span>
                    ) : null}
                    <span className="type-body tabular-nums">
                      {storageSize(row.bytes ?? 0)}
                    </span>
                  </>
                }
              />
            );
          })}
        </SettingsCard>
      ) : null}
      {list.hasNextPage ? (
        <div>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={list.isFetchingNextPage}
            onClick={() => void list.fetchNextPage()}
          >
            {t("more")}
          </Button>
        </div>
      ) : null}
    </SettingsGroup>
  );
}

export function StorageSettings() {
  const { t } = useTranslation("storage");
  const active = useRouteSceneActive();
  const connection = useQueryEvents({
    topic: "storage",
    observeConnection: true,
    resource: { kind: "global" },
    active,
    enabled: active,
    queryKeys: [...Object.values(storageKeys), expiredChatsKey],
    eventTypes: ["storage.changed"],
  });
  const offline =
    connection.state === "unavailable" || connection.state === "reconnecting";
  const maintenance = useQuery({
    ...storageMaintenanceOptions(),
    enabled: active,
  });
  const current = maintenance.data?.current;
  const last = maintenance.data?.last;
  const [receipt, setReceipt] = useState<{
    id: string;
    previousLastId?: string;
    epoch?: string;
  }>();
  const recordRun = (id: string) =>
    setReceipt({ id, previousLastId: last?.id, epoch: connection.epoch });
  const awaiting = Boolean(
    receipt &&
    !current &&
    receipt.id !== last?.id &&
    receipt.previousLastId === last?.id &&
    receipt.epoch === connection.epoch,
  );
  return (
    <SettingsContentShell title={t("title")} showHeader={false}>
      {offline ? (
        <p role="status" className="type-body text-content-secondary">
          {t("offline")}
        </p>
      ) : null}
      <StorageOverview active={active} />
      <EntityUsage kind="wakers" active={active} />
      <EntityUsage kind="chats" active={active} />
      <StorageCleanup
        active={active}
        disabled={offline || Boolean(current) || awaiting}
        onStarted={recordRun}
        last={last}
      />
      <StorageRetentionSettings
        active={active}
        disabled={offline}
        busy={Boolean(current) || awaiting}
        onStarted={recordRun}
      />
      {maintenance.isError ? (
        <StorageError
          error={maintenance.error}
          retry={() => void maintenance.refetch()}
          stale={Boolean(last)}
        />
      ) : null}
      {current || last || awaiting ? (
        <SettingsGroup title={t("maintenance")}>
          <SettingsCard className="type-body grid gap-1 p-4" role="status">
            {awaiting ? <p>{t("accepted")}</p> : null}
            {current?.startedAt ? (
              <p>{t("running", { time: storageDate(current.startedAt) })}</p>
            ) : null}
            {last ? (
              <>
                {last.finishedAt ? (
                  <p>{t("finished", { time: storageDate(last.finishedAt) })}</p>
                ) : null}
                <p>
                  {t("result", {
                    deleted: last.deleted ?? 0,
                    skipped: last.skipped ?? 0,
                    failed: last.failed ?? 0,
                  })}
                </p>
                {last.kind === "resources" || (last.removedBytes ?? 0) > 0 ? (
                  <p>
                    {t("removed", {
                      size: storageSize(last.removedBytes ?? 0),
                    })}
                  </p>
                ) : null}
                {last.pending ? (
                  <p>{t("pending", { count: last.pending })}</p>
                ) : null}
              </>
            ) : null}
          </SettingsCard>
        </SettingsGroup>
      ) : null}
    </SettingsContentShell>
  );
}
