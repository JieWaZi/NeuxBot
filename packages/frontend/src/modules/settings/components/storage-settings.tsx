import {
  useQuery,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import { Avatar } from "@/shared/ui/avatar";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  loadMoreStorage,
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
import { StoragePartnerDetails } from "./storage-files";
import { StorageOverview } from "./storage-overview";
import { StorageChatList } from "./storage-chat-list";

function StorageWakerList({
  active,
  offline,
  onOpenFiles,
}: {
  active: boolean;
  offline: boolean;
  onOpenFiles: (id: string) => void;
}) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const options = storageEntitiesOptions("wakers");
  const list = useInfiniteQuery({
    ...options,
    enabled: active,
  });
  const rows = (list.data?.pages ?? []).flatMap((page) => page.items ?? []);
  if (!list.isPending && !list.isError && !rows.length) return null;
  return (
    <SettingsGroup title={t("wakers")}>
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
            const title = row.title || t("wakerFallback");
            const description = [
              t("fileCount", { count: row.fileCount ?? 0 }),
              t("chatCount", { count: row.chatCount ?? 0 }),
            ].join(" · ");
            return (
              <SettingsNavigationRow
                key={id}
                title={title}
                description={description}
                visual={
                  <Avatar
                    name={title}
                    src={row.avatar}
                    size="sm"
                    animated={false}
                  />
                }
                onClick={() => onOpenFiles(id)}
                trailing={
                  <>
                    {largest > 0 ? (
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
            disabled={offline || list.isFetching}
            onClick={() => void loadMoreStorage(client, options.queryKey, list)}
          >
            {t("more")}
          </Button>
        </div>
      ) : null}
    </SettingsGroup>
  );
}

export function StorageSettings({
  partnerId,
  showSceneChrome,
}: {
  partnerId?: string;
  showSceneChrome: boolean;
}) {
  const { t } = useTranslation("storage");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const kind = params.get("files");
  const id = params.get("owner");
  function openFiles(owner: string) {
    setParams(
      { files: "wakers", owner },
      { state: { fromStorageOverview: true } },
    );
  }
  const connection = useQueryEvents({
    topic: "storage",
    observeConnection: true,
    resource: { kind: "global" },
    active,
    enabled: active,
    queryKeys: Object.values(storageKeys),
    eventTypes: ["storage.changed"],
  });
  const offline =
    connection.state === "unavailable" || connection.state === "reconnecting";
  const maintenance = useQuery({
    ...storageMaintenanceOptions(),
    enabled: active && !partnerId && kind !== "chats",
  });
  const current = maintenance.isError ? undefined : maintenance.data?.current;
  const last = maintenance.data?.last;
  const [receipt, setReceipt] = useState<string>();
  function recordRun(id: string) {
    setReceipt(id);
    void client
      .invalidateQueries({ queryKey: storageKeys.maintenance })
      .finally(() =>
        setReceipt((recorded) => (recorded === id ? undefined : recorded)),
      );
  }
  const awaiting = Boolean(receipt && !current && receipt !== last?.id);
  if (kind === "chats" && id) {
    return <Navigate to={chatHref(id, "")} replace />;
  }
  if (partnerId) {
    return (
      <StoragePartnerDetails
        key={partnerId}
        scope={{ kind: "wakers", id: partnerId }}
        active={active}
        disabled={offline}
        showSceneChrome={showSceneChrome}
        onBack={() => {
          if (location.state?.fromStorageOverview === true) navigate(-1);
          else setParams({}, { replace: true });
        }}
      />
    );
  }
  return (
    <SettingsContentShell title={t("title")} showHeader={false}>
      {offline ? (
        <p role="status" className="type-body text-content-secondary">
          {t("offline")}
        </p>
      ) : null}
      <StorageOverview active={active} />
      <StorageWakerList
        active={active}
        offline={offline}
        onOpenFiles={openFiles}
      />
      <StorageChatList active={active} disabled={offline} />
      <StorageCleanup
        active={active}
        disabled={offline}
        coreBusy={Boolean(current) || awaiting}
        onStarted={recordRun}
        last={last}
        currentRunId={current?.id}
        awaitingRunId={receipt}
      />
      <StorageRetentionSettings active={active} disabled={offline} />
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
