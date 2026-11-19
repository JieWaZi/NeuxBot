import { useRef } from "react";

/** 删除确认关闭时优先返回原操作。原行消失时选择仍可用的邻行动作。 */
export function useStorageDeleteFocus(fallback: () => HTMLElement | null) {
  const session = useRef<{
    origin: HTMLButtonElement;
    neighbors: HTMLButtonElement[];
    removed: boolean;
  }>(null);

  return {
    capture(container: HTMLElement | null, id: string) {
      const actions = Array.from(
        container?.querySelectorAll<HTMLButtonElement>(
          "button[data-storage-delete-action]",
        ) ?? [],
      );
      const index = actions.findIndex(
        (action) => action.dataset.storageDeleteAction === id,
      );
      const origin = actions[index];
      session.current = origin
        ? {
            origin,
            neighbors: [
              ...actions.slice(index + 1),
              ...actions.slice(0, index).reverse(),
            ],
            removed: false,
          }
        : null;
    },
    markRemoved() {
      if (session.current) session.current.removed = true;
    },
    target() {
      const current = session.current;
      const candidates = current
        ? current.removed
          ? current.neighbors
          : [current.origin, ...current.neighbors]
        : [];
      return (
        candidates.find((action) => action.isConnected && !action.disabled) ??
        fallback()
      );
    },
  };
}
