import { useEffect } from "react";
import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { platform, requirePlatformService } from "./provider";
import type { ApplicationUpdateState } from "@neuxbot/platform-contracts/application-update";

export { applicationReleasesURL } from "@neuxbot/platform-contracts/application-update";
export type { ApplicationUpdateState } from "@neuxbot/platform-contracts/application-update";

const queryKey = ["desktop", "application-update"] as const;
const browserState: ApplicationUpdateState = {
  revision: 0,
  currentVersion: null,
  platform: "browser",
  arch: "",
  status: "unavailable",
  unavailableReason: "browser",
  downloadedBytes: 0,
};

/** 本地快照与事件共享版本；主进程是更新状态的唯一来源。 */
function keepNewest(previous: unknown, next: unknown): unknown {
  if (
    previous &&
    next &&
    typeof previous === "object" &&
    typeof next === "object" &&
    "revision" in previous &&
    "revision" in next &&
    typeof previous.revision === "number" &&
    typeof next.revision === "number" &&
    previous.revision > next.revision
  )
    return previous;
  return next;
}

export function applicationUpdateOptions() {
  return queryOptions({
    queryKey,
    queryFn: () =>
      platform().applicationUpdate
        ? requirePlatformService("applicationUpdate").application_update_state()
        : Promise.resolve(browserState),
    staleTime: Infinity,
    retry: false,
    structuralSharing: keepNewest,
  });
}

const subscriptions = new WeakMap<
  QueryClient,
  { consumers: number; stop: () => void }
>();

function receiveState(client: QueryClient, state: ApplicationUpdateState) {
  client.setQueryData(queryKey, state);
}

/** 设置与 Shell 共用一个原生订阅，最后一个消费者释放时注销。 */
function subscribe(client: QueryClient): () => void {
  if (!platform().applicationUpdate) return () => undefined;
  let shared = subscriptions.get(client);
  if (!shared) {
    shared = {
      consumers: 0,
      stop: requirePlatformService("applicationUpdate").onApplicationUpdate(
        (state) => receiveState(client, state),
      ),
    };
    subscriptions.set(client, shared);
    // 设置 Scene 可暂时卸载 Shell 入口，重新接入时读取快照补齐监听空窗。
    void client
      .fetchQuery({ ...applicationUpdateOptions(), staleTime: 0 })
      .catch(() => undefined);
  }
  shared.consumers++;
  return () => {
    shared.consumers--;
    if (shared.consumers === 0) {
      shared.stop();
      subscriptions.delete(client);
    }
  };
}

export function useApplicationUpdate() {
  const client = useQueryClient();
  const query = useQuery(applicationUpdateOptions());
  useEffect(() => subscribe(client), [client]);
  const onSuccess = (state: ApplicationUpdateState) =>
    receiveState(client, state);
  const check = useMutation({
    mutationFn: () =>
      requirePlatformService("applicationUpdate").check_application_update(),
    onSuccess,
  });
  const download = useMutation({
    mutationFn: () =>
      requirePlatformService("applicationUpdate").download_application_update(),
    onSuccess,
  });
  const openInstaller = useMutation({
    mutationFn: () =>
      requirePlatformService("applicationUpdate").open_application_installer(),
    onSuccess,
  });
  return {
    state: query.data,
    error: query.error ?? check.error ?? download.error ?? openInstaller.error,
    busy: check.isPending || download.isPending || openInstaller.isPending,
    check: () => check.mutate(),
    download: () => download.mutate(),
    openInstaller: () => openInstaller.mutate(),
    retry: () => {
      check.reset();
      download.reset();
      openInstaller.reset();
      void query.refetch();
    },
  };
}
