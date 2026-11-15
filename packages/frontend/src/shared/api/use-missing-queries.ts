import { useQueryClient, type QueryCache } from "@tanstack/react-query";
import { useMemo, useSyncExternalStore } from "react";

import { ApiError } from "./errors";

function missingQueryStore(cache: QueryCache) {
  const read = () =>
    new Set(
      cache
        .getAll()
        .filter(
          (query) =>
            query.state.error instanceof ApiError &&
            query.state.error.status === 404,
        )
        .map((query) => query.queryHash),
    );
  let snapshot = read();
  return {
    getSnapshot: () => snapshot,
    subscribe: (listener: () => void) => {
      const stop = cache.subscribe((event) => {
        if (
          event.type !== "updated" &&
          event.type !== "removed" &&
          event.type !== "added"
        )
          return;
        const missing =
          event.type !== "removed" &&
          event.query.state.error instanceof ApiError &&
          event.query.state.error.status === 404;
        const hash = event.query.queryHash;
        if (snapshot.has(hash) === missing) return;
        snapshot = new Set(snapshot);
        if (missing) snapshot.add(hash);
        else snapshot.delete(hash);
        listener();
      });
      // 补齐首次 render 与订阅之间的变化；普通流式更新只检查当前 Query。
      const current = read();
      if (
        current.size !== snapshot.size ||
        [...current].some((hash) => !snapshot.has(hash))
      ) {
        snapshot = current;
        listener();
      }
      return stop;
    },
  };
}

/** 只在 404 身份变化时通知标签与导航，避免每个 Token 扫描全部缓存。 */
export function useMissingQueries(): ReadonlySet<string> {
  const cache = useQueryClient().getQueryCache();
  const store = useMemo(() => missingQueryStore(cache), [cache]);
  return useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getSnapshot,
  );
}
