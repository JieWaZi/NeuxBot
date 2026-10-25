import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

/** 本地日期跨界使默认时间范围陈旧，只有可见 Scene 发起一次对账。 */
export function useWorkBoardDateBoundary(
  active: boolean,
  queryKeys: readonly QueryKey[],
) {
  const client = useQueryClient();
  const current = useRef({ active, queryKeys });
  const dirty = useRef(false);
  useEffect(() => {
    current.current = { active, queryKeys };
  }, [active, queryKeys]);
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    const restoreVisible = () => {
      if (
        !current.current.active ||
        document.visibilityState !== "visible" ||
        !dirty.current
      )
        return;
      dirty.current = false;
      for (const queryKey of current.current.queryKeys) {
        void client.refetchQueries(
          { queryKey, exact: true, type: "active", stale: true },
          { cancelRefetch: false },
        );
      }
    };
    const schedule = () => {
      const midnight = new Date();
      midnight.setHours(24, 0, 0, 0);
      timer = setTimeout(
        () => {
          const visible =
            current.current.active && document.visibilityState === "visible";
          dirty.current = !visible;
          for (const queryKey of current.current.queryKeys) {
            void client.invalidateQueries({
              queryKey,
              exact: true,
              refetchType: visible ? "active" : "none",
            });
          }
          schedule();
        },
        Math.max(1, midnight.getTime() - Date.now()),
      );
    };
    schedule();
    document.addEventListener("visibilitychange", restoreVisible);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", restoreVisible);
    };
  }, [client]);
  useEffect(() => {
    if (!active || document.visibilityState !== "visible" || !dirty.current)
      return;
    dirty.current = false;
    for (const queryKey of current.current.queryKeys) {
      void client.refetchQueries(
        { queryKey, exact: true, type: "active", stale: true },
        { cancelRefetch: false },
      );
    }
  }, [active, client]);
}
