import { useState } from "react";

export function useResourceSelection<T extends { id: string }>(
  items: T[],
  scope: string,
) {
  const [state, setState] = useState({ scope, ids: new Set<string>() });
  if (state.scope !== scope) setState({ scope, ids: new Set<string>() });
  const selected = items.filter(
    (item) => state.scope === scope && state.ids.has(item.id),
  );
  const selectedIds = new Set(selected.map((item) => item.id));
  const checked: boolean | "indeterminate" =
    selected.length === 0
      ? false
      : selected.length === items.length
        ? true
        : "indeterminate";
  const setSelected = (id: string, value: boolean) => {
    setState((current) => {
      const ids = new Set(current.scope === scope ? current.ids : []);
      if (value) ids.add(id);
      else ids.delete(id);
      return { scope, ids };
    });
  };
  const selectAll = (value: boolean) =>
    setState({
      scope,
      ids: new Set(value ? items.map((item) => item.id) : []),
    });
  const clear = () => selectAll(false);
  const remove = (deletedIds: string[]) =>
    setState((current) => ({
      scope,
      ids: new Set(
        current.scope === scope
          ? [...current.ids].filter((id) => !deletedIds.includes(id))
          : [],
      ),
    }));
  return {
    checked,
    selected,
    selectedIds,
    setSelected,
    selectAll,
    clear,
    remove,
    disabled: items.length === 0,
  };
}
