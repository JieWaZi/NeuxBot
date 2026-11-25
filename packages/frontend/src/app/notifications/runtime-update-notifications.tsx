import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import type { ListRuntimesResponse } from "@/shared/api/generated/types.gen";
import { registerCoreConnectionRecovery } from "@/shared/api/global-events";
import {
  listRuntimesOptions,
  runtimeInstallationOptions,
} from "@/shared/api/runtime-queries";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { localDateToInput } from "@/shared/lib/local-date";
import { runtimeDisplayVersion } from "@/shared/lib/runtime-display-name";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { toast } from "@/shared/ui/sonner";

type InstalledRuntime = { id: string; base: string; name: string };

function installedRuntimes(snapshot: ListRuntimesResponse) {
  if (snapshot.startupDetectionPending) return [];
  const selected = new Map<string, InstalledRuntime>();
  for (const runtime of snapshot.resources ?? []) {
    const { id, baseRuntimeId: base, name, installed } = runtime;
    if (!installed || !id || !base || !name) continue;
    if (!selected.has(base) || id === base)
      selected.set(base, { id, base, name });
  }
  return [...selected.values()];
}

function RuntimeUpdateNotification({
  runtime,
  shownDates,
}: {
  runtime: InstalledRuntime;
  shownDates: Map<string, string>;
}) {
  const { id, base, name } = runtime;
  const { t } = useTranslation("notification");
  const navigate = useNavigate();
  const client = useQueryClient();
  const options = useMemo(() => runtimeInstallationOptions(id), [id]);
  const { data } = useQuery(options);
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    eventTypes: ["runtime.installation.changed"],
    eventFilter: ({ resource }) =>
      resource.kind === "global" ||
      (resource.kind === "runtime" &&
        (resource.id === id || resource.id === base)),
  });
  useEffect(
    () =>
      registerCoreConnectionRecovery(() => {
        void client.resetQueries({ queryKey: options.queryKey, exact: true });
      }),
    [client, options.queryKey],
  );

  useEffect(() => {
    if (
      data?.checkState !== "checked" ||
      data.updateAvailable !== true ||
      data.state === "installing"
    )
      return;
    const current = runtimeDisplayVersion(data.version).replace(/^v/i, "");
    const latest = runtimeDisplayVersion(data.latestVersion).replace(/^v/i, "");
    if (!current || !latest) return;

    const today = localDateToInput(new Date());
    if (shownDates.get(base) === today) return;
    const storageKey = `bot:runtime-update-shown:${base}`;
    try {
      if (window.localStorage.getItem(storageKey) === today) return;
      window.localStorage.setItem(storageKey, today);
    } catch {
      // 本地存储不可用时，仍在当前工作台内按天去重。
    }
    shownDates.set(base, today);
    const noticeId = `runtime-update:${base}`;
    toast.manual(t("inApp.runtimeUpdate", { name }), {
      id: noticeId,
      description: t("inApp.versions", { current, latest }),
      icon: <RuntimeCliIcon runtimeId={base} />,
      action: {
        label: t("inApp.viewUpdate"),
        onClick: () => {
          void navigate(
            `/settings/agents?${new URLSearchParams({ agent: id })}`,
          );
          toast.dismiss(noticeId);
        },
      },
    });
  }, [base, data, id, name, navigate, shownDates, t]);
  return null;
}

/** 已安装智能体的通知由现有安装快照驱动，重连和首次打开都能补回启动结果。 */
export function RuntimeUpdateNotifications() {
  const { data } = useQuery({
    ...listRuntimesOptions(),
    select: installedRuntimes,
  });
  const [shownDates] = useState(() => new Map<string, string>());
  return data?.map((runtime) => (
    <RuntimeUpdateNotification
      key={runtime.id}
      runtime={runtime}
      shownDates={shownDates}
    />
  ));
}
