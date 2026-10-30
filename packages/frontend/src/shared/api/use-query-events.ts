import {
  matchQuery,
  useQueryClient,
  type Query,
  type QueryClient,
  type QueryKey,
  isCancelledError,
} from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import {
  subscribeGlobalEvents,
  type GlobalEventResource,
  type GlobalEventTopic,
  type GlobalEventFrame,
} from "./global-events";

type Consumer = {
  current: { active: boolean; queryKeys: readonly QueryKey[] };
  read?: () => Promise<unknown>;
  priority?: number;
};
type Refresh = {
  query: Query;
  dirty: Set<Consumer>;
  revision: number;
  freshFetchRevision?: number;
  timer?: ReturnType<typeof setTimeout>;
  running: boolean;
  readingRevision?: number;
  completion?: Promise<void>;
  ownedFetch?: boolean;
};
type RefreshOwner = {
  consumers: Set<Consumer>;
  queries: Map<string, Refresh>;
  stop: () => void;
};
const refreshOwners = new WeakMap<QueryClient, RefreshOwner>();

function matches(consumer: Consumer, query: Query) {
  return consumer.current.queryKeys.some((queryKey) =>
    matchQuery({ queryKey }, query),
  );
}

function isPageFetch(query: Query) {
  return Boolean(query.state.fetchMeta?.fetchMore?.direction);
}

/** 只保存失效调度与消费者身份；查询和业务事实仍由 Query Cache 唯一持有。 */
function acquireOwner(client: QueryClient): RefreshOwner {
  let owner = refreshOwners.get(client);
  if (owner) return owner;
  owner = { consumers: new Set(), queries: new Map(), stop: () => {} };
  const current = owner;
  current.stop = client.getQueryCache().subscribe((event) => {
    const refresh = current.queries.get(event.query.queryHash);
    if (!refresh) return;
    if (event.type === "removed") {
      if (refresh.timer !== undefined) clearTimeout(refresh.timer);
      current.queries.delete(event.query.queryHash);
    } else if (event.type === "updated") {
      // Scene 自动恢复可覆盖变化；单独翻页只读一个旧窗口，不能覆盖头部对账。
      if (event.action.type === "fetch" && refresh.dirty.size)
        refresh.freshFetchRevision = isPageFetch(event.query)
          ? undefined
          : refresh.running
            ? refresh.readingRevision
            : refresh.revision;
      if (
        ["success", "error"].includes(event.action.type) &&
        event.query.state.fetchStatus === "idle" &&
        !isPageFetch(event.query) &&
        refresh.freshFetchRevision === refresh.revision
      ) {
        refresh.dirty.clear();
        if (refresh.timer !== undefined) clearTimeout(refresh.timer);
        refresh.timer = undefined;
      }
      // 外部取消会以 setState 恢复 idle；被采纳的 GET 尚未完成原来的对账。
      if (
        event.action.type === "setState" &&
        event.query.state.fetchStatus === "idle" &&
        refresh.freshFetchRevision !== undefined &&
        refresh.dirty.size
      ) {
        refresh.freshFetchRevision = undefined;
        if (
          document.visibilityState === "visible" &&
          [...refresh.dirty].some(
            (consumer) =>
              consumer.current.active && matches(consumer, refresh.query),
          )
        )
          scheduleRefresh(client, current, refresh);
      }
    }
  });
  refreshOwners.set(client, current);
  return current;
}

function scheduleRefresh(
  client: QueryClient,
  owner: RefreshOwner,
  refresh: Refresh,
  immediate = false,
) {
  if (refresh.running) {
    // 新变化不能被正在读取旧版本的命令/事件吞掉，取消由同一 owner 发起。
    if (
      document.visibilityState === "visible" &&
      refresh.query.state.fetchStatus !== "idle" &&
      !isPageFetch(refresh.query) &&
      refresh.freshFetchRevision !== refresh.revision &&
      [...refresh.dirty].some(
        (consumer) =>
          consumer.current.active && matches(consumer, refresh.query),
      )
    ) {
      void client.cancelQueries(
        { queryKey: refresh.query.queryKey, exact: true },
        { revert: false },
      );
    }
    return;
  }
  if (refresh.timer !== undefined) {
    if (!immediate) return;
    clearTimeout(refresh.timer);
  }
  // 即时对账也经过共享队列，同一 ready 帧的多个消费者只拥有一次取消/读取。
  refresh.timer = setTimeout(
    () => {
      refresh.timer = undefined;
      void reconcile(client, owner, refresh).catch(() => {});
    },
    immediate ? 0 : 250,
  );
}

function reconcile(
  client: QueryClient,
  owner: RefreshOwner,
  refresh: Refresh,
): Promise<void> {
  if (refresh.running) return refresh.completion ?? Promise.resolve();
  const alive = () => owner.queries.get(refresh.query.queryHash) === refresh;
  const visible = () =>
    document.visibilityState === "visible" &&
    [...refresh.dirty].some(
      (consumer) => consumer.current.active && matches(consumer, refresh.query),
    );
  if (!alive() || !refresh.dirty.size) return Promise.resolve();
  let revision = refresh.revision;
  refresh.readingRevision = revision;
  const execute = async () => {
    const filters = { queryKey: refresh.query.queryKey, exact: true };
    try {
      // 正常分页保留原 Promise 和提交结果；完成后才读取头部，期间变化同批合并。
      while (
        visible() &&
        isPageFetch(refresh.query) &&
        refresh.query.state.fetchStatus !== "idle"
      ) {
        // 分页错误仍由原调用方呈现，头部事实还需要独立对账。
        await refresh.query.promise?.catch(() => {});
        if (!alive()) return;
        revision = refresh.revision;
        refresh.readingRevision = revision;
      }
      // 变化之后开始的原查询已拥有这批事实；等待真正提交。
      if (
        refresh.freshFetchRevision === revision &&
        refresh.query.state.fetchStatus !== "idle"
      ) {
        await refresh.query.promise;
        return;
      }
      if (visible()) {
        refresh.freshFetchRevision = undefined;
        await client.cancelQueries(filters, { revert: false });
      }
      if (!alive()) return;
      const read = visible();
      const command = [...refresh.dirty]
        .filter((consumer) => consumer.read)
        .sort((left, right) => (right.priority ?? 0) - (left.priority ?? 0))[0];
      if (read && command?.read) {
        await client.invalidateQueries({ ...filters, refetchType: "none" });
        if (!alive() || !visible()) return;
        refresh.ownedFetch = true;
        await command.read();
      } else {
        refresh.ownedFetch = read;
        await client.invalidateQueries(
          { ...filters, refetchType: read ? "active" : "none" },
          { cancelRefetch: false },
        );
      }
      if (read && refresh.revision === revision) refresh.dirty.clear();
    } finally {
      refresh.ownedFetch = false;
      refresh.running = false;
      refresh.completion = undefined;
      if (
        alive() &&
        refresh.dirty.size &&
        visible() &&
        (refresh.revision !== revision ||
          refresh.freshFetchRevision === undefined)
      )
        scheduleRefresh(client, owner, refresh);
    }
  };
  refresh.running = true;
  refresh.completion = execute();
  return refresh.completion;
}

function releaseConsumer(
  client: QueryClient,
  owner: RefreshOwner,
  consumer: Consumer,
  cancelOwned = false,
) {
  owner.consumers.delete(consumer);
  for (const [hash, pending] of owner.queries) {
    const waiting = pending.dirty.delete(consumer);
    const cancel =
      cancelOwned &&
      waiting &&
      pending.ownedFetch &&
      pending.query.state.fetchStatus !== "idle" &&
      !isPageFetch(pending.query) &&
      ![...owner.consumers].some(
        (item) => item.current.active && matches(item, pending.query),
      );
    if (![...owner.consumers].some((item) => matches(item, pending.query))) {
      if (pending.timer !== undefined) clearTimeout(pending.timer);
      owner.queries.delete(hash);
    }
    // 只取消最后活跃等待者所属的头部读取；共享请求、外部读取和正常分页继续提交。
    if (cancel)
      void client.cancelQueries(
        { queryKey: pending.query.queryKey, exact: true },
        { revert: false },
      );
  }
  if (!owner.consumers.size) {
    owner.stop();
    refreshOwners.delete(client);
  }
}

/** 调用者可退出共享等待；实际读取的取消仍由原 Query owner 判断。 */
function waitForRefresh(completion: Promise<void>, signal?: AbortSignal) {
  if (!signal) return completion;
  return new Promise<void>((resolve, reject) => {
    const aborted = () => reject(signal.reason);
    signal.addEventListener("abort", aborted, { once: true });
    const settle = () => signal.removeEventListener("abort", aborted);
    completion.then(
      () => {
        settle();
        resolve();
      },
      (error: unknown) => {
        settle();
        reject(error);
      },
    );
    if (signal.aborted) {
      settle();
      aborted();
    }
  });
}

/** 命令与领域推送共用实际 Query 的变化代次；前台显式命令无 observer 时也读取。
 * priority 仅声明同批读取范围的完整程度，较完整策略覆盖较窄策略，不保存领域事实。
 */
export async function refreshQueryFromEvent(
  client: QueryClient,
  queryKey: QueryKey,
  read: () => Promise<unknown>,
  priority = 0,
  signal?: AbortSignal,
): Promise<void> {
  signal?.throwIfAborted();
  const owner = acquireOwner(client);
  const consumer: Consumer = {
    current: { active: true, queryKeys: [queryKey] },
    read,
    priority,
  };
  owner.consumers.add(consumer);
  const query =
    client.getQueryCache().find({ queryKey, exact: true }) ??
    client.getQueryCache().build(client, { queryKey });
  let pending = owner.queries.get(query.queryHash);
  if (!pending) {
    pending = { query, dirty: new Set(), revision: 0, running: false };
    owner.queries.set(query.queryHash, pending);
  }
  pending.dirty.add(consumer);
  pending.revision++;
  const abort = () => releaseConsumer(client, owner, consumer, true);
  signal?.addEventListener("abort", abort, { once: true });
  scheduleRefresh(client, owner, pending, true);
  try {
    // 与 ready 调度相同：同一轮同步命令先合并，再共同拥有一次读取。
    await waitForRefresh(
      new Promise<void>((resolve) => setTimeout(resolve, 0)),
      signal,
    );
    signal?.throwIfAborted();
    while (
      owner.queries.get(query.queryHash) === pending &&
      pending.dirty.has(consumer)
    ) {
      signal?.throwIfAborted();
      if (pending.timer !== undefined) {
        clearTimeout(pending.timer);
        pending.timer = undefined;
      }
      try {
        await waitForRefresh(reconcile(client, owner, pending), signal);
      } catch (error) {
        if (!isCancelledError(error)) throw error;
      }
      if (document.visibilityState !== "visible") break;
    }
    signal?.throwIfAborted();
  } finally {
    signal?.removeEventListener("abort", abort);
    releaseConsumer(client, owner, consumer);
  }
}

/** 订阅领域资源变化；同一 QueryClient/实际 Query 共用刷新所有权。 */
export function useQueryEvents({
  topic,
  resource,
  queryKeys,
  active = true,
  enabled = true,
  eventTypes,
  eventFilter,
}: {
  topic: GlobalEventTopic;
  resource: GlobalEventResource;
  queryKeys: readonly QueryKey[];
  active?: boolean;
  enabled?: boolean;
  eventTypes?: readonly string[];
  eventFilter?: (frame: GlobalEventFrame) => boolean;
}) {
  const client = useQueryClient();
  const current = useRef({ active, queryKeys, eventTypes, eventFilter });
  useEffect(() => {
    current.current = { active, queryKeys, eventTypes, eventFilter };
  }, [active, queryKeys, eventTypes, eventFilter]);
  const restore = useRef<(() => void) | undefined>(undefined);
  const resourceRef = useRef(resource);
  useEffect(() => {
    resourceRef.current = resource;
  }, [resource]);
  const resourceKey = JSON.stringify(resource);
  useEffect(() => {
    if (!enabled) return;
    const owner = acquireOwner(client);
    owner.consumers.add(current);
    const refresh = (immediate = false) => {
      const queries = new Set(
        current.current.queryKeys.flatMap((queryKey) =>
          client.getQueryCache().findAll({ queryKey }),
        ),
      );
      for (const query of queries) {
        let pending = owner.queries.get(query.queryHash);
        if (!pending) {
          pending = { query, dirty: new Set(), revision: 0, running: false };
          owner.queries.set(query.queryHash, pending);
        }
        pending.dirty.add(current);
        pending.revision++;
        scheduleRefresh(client, owner, pending, immediate);
      }
    };
    const restoreVisible = () => {
      if (!current.current.active || document.visibilityState !== "visible")
        return;
      for (const pending of owner.queries.values())
        if (pending.dirty.has(current) && matches(current, pending.query))
          scheduleRefresh(client, owner, pending, true);
    };
    restore.current = restoreVisible;
    const close = subscribeGlobalEvents(topic, resourceRef.current, {
      onEvent: (frame: GlobalEventFrame) => {
        if (frame.type === `${topic}.ready`) {
          refresh(true);
          return;
        }
        if (
          frame.transient ||
          ["subscribed", "unsubscribed", "error"].includes(frame.type)
        )
          return;
        const payload = frame.payload;
        const type =
          payload &&
          typeof payload === "object" &&
          "type" in payload &&
          typeof payload.type === "string"
            ? payload.type
            : frame.type;
        const selected = current.current.eventTypes;
        if (
          (selected &&
            !selected.some((item) =>
              item.endsWith(".") ? type.startsWith(item) : type === item,
            )) ||
          (current.current.eventFilter && !current.current.eventFilter(frame))
        )
          return;
        refresh();
      },
      onResync: () => refresh(),
    });
    document.addEventListener("visibilitychange", restoreVisible);
    return () => {
      restore.current = undefined;
      document.removeEventListener("visibilitychange", restoreVisible);
      close();
      releaseConsumer(client, owner, current);
    };
  }, [client, enabled, resourceKey, topic]);
  useEffect(() => {
    restore.current?.();
  }, [active, client]);
}
