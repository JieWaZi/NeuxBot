import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { readUsage, storageKeys, storageOptions } from "../api/storage";
import { SettingsGroup } from "./settings-section-layout";
import {
  StorageError,
  StorageLoading,
  storageDate,
  storageSize,
} from "./storage-feedback";

const usageGroups = {
  workspace: "bg-accent",
  chats: "bg-accent-secondary",
  knowledge: "bg-success",
  application: "bg-primary",
};

function usageGroup(category?: string): keyof typeof usageGroups {
  switch (category) {
    case "work_files":
      return "workspace";
    case "conversations":
    case "shared_native":
      return "chats";
    case "knowledge":
      return "knowledge";
    default:
      return "application";
  }
}

export function StorageOverview({ active }: { active: boolean }) {
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
      await Promise.all([
        client.invalidateQueries({ queryKey: storageKeys.entities }),
        client.invalidateQueries({ queryKey: storageKeys.files }),
      ]);
    },
  });
  const data = usage.data;
  const bytesByGroup = { workspace: 0, chats: 0, knowledge: 0, application: 0 };
  for (const row of data?.categories ?? []) {
    bytesByGroup[usageGroup(row.category)] += row.bytes ?? 0;
  }
  const groups = (Object.keys(usageGroups) as Array<keyof typeof usageGroups>)
    .map((group) => ({ group, bytes: bytesByGroup[group] }))
    .filter(({ bytes }) => bytes > 0);
  const disks = (data?.disks ?? []).filter((disk) => disk.freeBytes != null);
  const total = data?.totalBytes ?? 0;

  return (
    <SettingsGroup
      title={t("usage")}
      trailing={
        <Button
          type="button"
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
        <div className="grid gap-3">
          <div>
            <p className="type-display text-content-primary font-semibold tabular-nums">
              {data.complete
                ? storageSize(total)
                : t("partial", { size: storageSize(total, "lower-bound") })}
            </p>
            <div className="type-ui text-content-secondary flex flex-wrap gap-x-3 gap-y-1">
              {data.scannedAt ? (
                <p>{t("scanned", { time: storageDate(data.scannedAt) })}</p>
              ) : null}
              {disks.map((disk, index) => (
                <p key={disk.path}>
                  {t(disks.length > 1 ? "freeDisk" : "free", {
                    index: index + 1,
                    size: storageSize(disk.freeBytes ?? 0),
                  })}
                </p>
              ))}
            </div>
          </div>
          {groups.length ? (
            <>
              <div aria-hidden="true" className="flex h-2.5 w-full gap-0.5">
                {groups.map(({ group, bytes }) => (
                  <span
                    key={group}
                    className={cn("min-w-px rounded-xs", usageGroups[group])}
                    style={{ flexGrow: bytes, flexBasis: 0 }}
                  />
                ))}
              </div>
              <ul
                aria-label={t("categories")}
                className="grid gap-x-5 @[42.5rem]/settings-scene:grid-cols-2"
              >
                {groups.map(({ group, bytes }) => (
                  <li
                    key={group}
                    className="type-body flex items-center gap-2 py-1"
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-2 shrink-0 rounded-full",
                        usageGroups[group],
                      )}
                    />
                    <span className="min-w-0 flex-1">
                      {t(`category.${group}`)}
                    </span>
                    <span className="tabular-nums">{storageSize(bytes)}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : data.complete ? (
            <p className="type-body">{t("empty")}</p>
          ) : null}
        </div>
      ) : null}
    </SettingsGroup>
  );
}
