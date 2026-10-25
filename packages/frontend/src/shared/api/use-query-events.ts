import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import {
  subscribeGlobalEvents,
  type GlobalEventResource,
  type GlobalEventTopic,
  type GlobalEventFrame,
} from "./global-events";

/** 订阅领域资源变化，隐藏 Scene 只失效，重新可见后一次对账。 */
export function useQueryEvents({
  topic,
  resource,
  queryKeys,
  active = true,
  enabled = true,
  eventTypes,
}: {
  topic: GlobalEventTopic;
  resource: GlobalEventResource;
  queryKeys: readonly QueryKey[];
  active?: boolean;
  enabled?: boolean;
  eventTypes?: readonly string[];
}) {
  const client = useQueryClient();
  const current = useRef({ active, queryKeys, eventTypes });
  useEffect(() => {
    current.current = { active, queryKeys, eventTypes };
  }, [active, queryKeys, eventTypes]);
  const dirty = useRef(false);
  const requestRefresh = useRef<(() => void) | undefined>(undefined);
  const resourceRef = useRef(resource);
  useEffect(() => {
    resourceRef.current = resource;
  }, [resource]);
  const resourceKey = JSON.stringify(resource);
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let revision = 0;
    const reconcile = async () => {
      const observedRevision = revision;
      const visible = () =>
        current.current.active && document.visibilityState === "visible";
      const keys = current.current.queryKeys;
      // 无 data 的初始 GET 不会被 invalidate 自动取消；取消后显式重读，避免旧响应吞掉变化。
      if (visible())
        await Promise.all(
          keys.map((queryKey) => client.cancelQueries({ queryKey })),
        );
      if (!alive) return;
      const shouldRead = visible();
      for (const queryKey of current.current.queryKeys) {
        void client.invalidateQueries(
          { queryKey, refetchType: shouldRead ? "active" : "none" },
          { cancelRefetch: false },
        );
      }
      // 取消等待期间到达的变化属于下一批，旧批次不能抹去它的恢复资格。
      if (shouldRead && revision === observedRevision) dirty.current = false;
    };
    const refresh = (immediate = false) => {
      dirty.current = true;
      revision++;
      if (immediate) {
        if (timer) clearTimeout(timer);
        timer = undefined;
        void reconcile();
        return;
      }
      if (timer) return;
      timer = setTimeout(() => {
        timer = undefined;
        void reconcile();
      }, 250);
    };
    requestRefresh.current = () => refresh(true);
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
          selected &&
          !selected.some((item) =>
            item.endsWith(".") ? type.startsWith(item) : type === item,
          )
        )
          return;
        refresh();
      },
      onResync: () => refresh(),
    });
    const restoreVisible = () => {
      if (
        current.current.active &&
        document.visibilityState === "visible" &&
        dirty.current
      )
        refresh(true);
    };
    document.addEventListener("visibilitychange", restoreVisible);
    return () => {
      alive = false;
      requestRefresh.current = undefined;
      document.removeEventListener("visibilitychange", restoreVisible);
      close();
      if (timer) clearTimeout(timer);
    };
  }, [client, enabled, resourceKey, topic]);
  useEffect(() => {
    if (!active || document.visibilityState !== "visible" || !dirty.current)
      return;
    requestRefresh.current?.();
  }, [active, client]);
}
