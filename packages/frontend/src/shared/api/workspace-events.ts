import { useEffect, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  hasPlatformService,
  observeWorkspaceResource,
  openWorkspaceResourceScope,
} from "@/shared/platform";
import { registerCoreConnectionRecovery } from "./global-events";
import { logger } from "@/shared/diagnostics/logger";
import { i18n } from "@/shared/i18n";
import type { WorkspaceResourceChangeEvent } from "@neuxbot/platform-contracts/resources";

type Observer = (event: WorkspaceResourceChangeEvent | Error) => void;
type Subscription = {
  observers: Set<Observer>;
  controller: AbortController;
  last?: WorkspaceResourceChangeEvent | Error;
  start: () => void;
  stopRecovery: () => void;
};
const subscriptions = new Map<string, Subscription>();

/** 同 Chat/授权根的文件、搜索、生成文件和 Git 消费者共享观察，不读取第二套事实。 */
export function subscribeWorkspaceEvents(
  chatId: string,
  root: string,
  observer: Observer,
  relativePath?: string,
): () => void {
  const key = `${chatId}\0${root}\0${relativePath ?? ""}`;
  let subscription = subscriptions.get(key);
  if (!subscription) {
    subscription = {
      observers: new Set(),
      controller: new AbortController(),
      start: () => {},
      stopRecovery: () => {},
    };
    subscriptions.set(key, subscription);
    const current = subscription;
    current.start = () => {
      const controller = current.controller;
      const notify = (event: WorkspaceResourceChangeEvent | Error) => {
        if (controller.signal.aborted) return;
        current.last = event;
        for (const consumer of current.observers) consumer(event);
      };
      void openWorkspaceResourceScope(chatId)
        .then((scope) =>
          observeWorkspaceResource(
            scope.id,
            relativePath ?? "",
            notify,
            controller.signal,
            root,
            relativePath === undefined,
          ),
        )
        .catch((cause: unknown) => {
          if (controller.signal.aborted) return;
          logger.warn("workspace event subscription unavailable");
          notify(
            cause instanceof Error
              ? cause
              : new Error(i18n.t("common:workspaceUpdatesUnavailable")),
          );
        });
    };
    current.stopRecovery = registerCoreConnectionRecovery(() => {
      current.controller.abort();
      current.controller = new AbortController();
      current.last = undefined;
      current.start();
    });
    current.start();
  }
  subscription.observers.add(observer);
  if (subscription.last) observer(subscription.last);
  const current = subscription;
  return () => {
    current.observers.delete(observer);
    if (!current.observers.size) {
      current.controller.abort();
      current.stopRecovery();
      subscriptions.delete(key);
    }
  };
}

type InvalidationBatch = {
  scopes: Map<string, { root: string; path?: string; prefixes: Set<string> }>;
};
const invalidations = new WeakMap<QueryClient, InvalidationBatch>();

type CompletionBatch = { keys: Set<string>; stop: () => void };
const completions = new WeakMap<QueryClient, CompletionBatch>();

/** 首次读取尚未结束时记录真实变化，完成后再对账一次，避免首次无数据查询吞掉失效信号。 */
function reconcileAfterPendingQuery(
  queryClient: QueryClient,
  queryHash: string,
) {
  let batch = completions.get(queryClient);
  if (!batch) {
    batch = { keys: new Set(), stop: () => {} };
    completions.set(queryClient, batch);
    const current = batch;
    current.stop = queryClient.getQueryCache().subscribe((event) => {
      if (!current.keys.has(event.query.queryHash)) return;
      if (
        event.type === "removed" ||
        (event.type === "updated" && event.query.state.fetchStatus === "idle")
      ) {
        current.keys.delete(event.query.queryHash);
        if (!current.keys.size) {
          current.stop();
          completions.delete(queryClient);
        }
        if (event.type !== "removed")
          void queryClient.invalidateQueries({
            queryKey: event.query.queryKey,
            exact: true,
            refetchType:
              document.visibilityState === "visible" ? "active" : "none",
          });
      }
    });
  }
  batch.keys.add(queryHash);
}

/** 同一次文件事件的多个视图合并失效，避免对同一个成果 Query 重复取消和重发请求。 */
function invalidateWorkspaceQueries(
  queryClient: QueryClient,
  root: string,
  prefixes: readonly string[],
  relativePath?: string,
) {
  let batch = invalidations.get(queryClient);
  if (!batch) {
    batch = { scopes: new Map() };
    invalidations.set(queryClient, batch);
    const current = batch;
    queueMicrotask(() => {
      invalidations.delete(queryClient);
      const affected = (query: { queryKey: readonly unknown[] }) =>
        [...current.scopes.values()].some(
          ({
            root: affectedRoot,
            path: affectedPath,
            prefixes: affectedPrefixes,
          }) =>
            (!affectedPath || query.queryKey.includes(affectedPath)) &&
            affectedPrefixes.has(String(query.queryKey[0])) &&
            query.queryKey.some(
              (part) =>
                part === affectedRoot ||
                (Array.isArray(part) && part.includes(affectedRoot)),
            ),
        );
      for (const query of queryClient
        .getQueryCache()
        .findAll({ predicate: affected })) {
        if (
          query.state.fetchStatus === "fetching" &&
          query.state.data === undefined
        )
          reconcileAfterPendingQuery(queryClient, query.queryHash);
      }
      void queryClient.invalidateQueries({
        refetchType: document.visibilityState === "visible" ? "active" : "none",
        predicate: affected,
      });
    });
  }
  const key = `${root}\0${relativePath ?? ""}`;
  const affected = batch.scopes.get(key) ?? {
    root,
    path: relativePath,
    prefixes: new Set<string>(),
  };
  for (const prefix of prefixes) affected.prefixes.add(prefix);
  batch.scopes.set(key, affected);
}

/** 原生失效只刷新所属根的 Query；重载/重新可见时一次重建，不定时查询。 */
export function useWorkspaceQueryEvents(
  chatId: string | undefined,
  roots: readonly string[],
  prefixes: readonly string[],
  active = true,
  relativePath?: string,
) {
  const queryClient = useQueryClient();
  const [error, setError] = useState<Error>();
  const [generation, setGeneration] = useState(0);
  const [visible, setVisible] = useState(
    document.visibilityState === "visible",
  );
  useEffect(() => {
    const visibilityChanged = () =>
      setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", visibilityChanged);
    return () =>
      document.removeEventListener("visibilitychange", visibilityChanged);
  }, []);
  const rootKey = [...new Set(roots.filter(Boolean))].sort().join("\0");
  const prefixKey = [...new Set(prefixes)].sort().join("\0");
  useEffect(() => {
    if (
      !active ||
      !visible ||
      !chatId ||
      !rootKey ||
      !hasPlatformService("workspace")
    )
      return;
    const rootPaths = rootKey.split("\0");
    const queryPrefixes = prefixKey.split("\0");
    const refresh = (root: string) =>
      invalidateWorkspaceQueries(
        queryClient,
        root,
        queryPrefixes,
        relativePath,
      );
    const stops = rootPaths.map((root) =>
      subscribeWorkspaceEvents(
        chatId,
        root,
        (event) => {
          if (
            event instanceof Error ||
            event.state === "failed" ||
            event.state === "recovering"
          ) {
            setError(
              event instanceof Error
                ? event
                : new Error(i18n.t("common:workspaceUpdatesUnavailable")),
            );
            return;
          }
          setError(undefined);
          // ready 后也对账一次，覆盖首次 Query 与注册观察之间发生的文件修改。
          refresh(root);
        },
        relativePath,
      ),
    );
    return () => {
      for (const stop of stops) stop();
    };
  }, [
    active,
    chatId,
    generation,
    prefixKey,
    queryClient,
    rootKey,
    relativePath,
    visible,
  ]);
  return {
    error: active ? error : undefined,
    retry: () => {
      for (const root of rootKey.split("\0")) {
        const subscription = subscriptions.get(
          `${chatId}\0${root}\0${relativePath ?? ""}`,
        );
        if (subscription) {
          subscription.controller.abort();
          subscription.controller = new AbortController();
          subscription.last = undefined;
          subscription.start();
        }
      }
      setGeneration((value) => value + 1);
    },
  };
}
