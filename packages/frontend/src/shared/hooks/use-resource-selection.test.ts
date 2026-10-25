import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useResourceSelection } from "./use-resource-selection";

describe("resource selection", () => {
  it("supports partial selection, all selection and successful-item removal", () => {
    const { result } = renderHook(() =>
      useResourceSelection([{ id: "a" }, { id: "b" }], "scope"),
    );
    act(() => result.current.setSelected("a", true));
    expect(result.current.checked).toBe("indeterminate");
    act(() => result.current.selectAll(true));
    expect(result.current.checked).toBe(true);
    act(() => result.current.remove(["a"]));
    expect(result.current.selected).toEqual([{ id: "b" }]);
    act(() => result.current.clear());
    expect(result.current.checked).toBe(false);
  });
  it("clears on filter or partner changes without restoring stale selection on return", () => {
    const { result, rerender } = renderHook(
      ({ scope }) => useResourceSelection([{ id: "a" }], scope),
      { initialProps: { scope: "first" } },
    );
    act(() => result.current.selectAll(true));
    rerender({ scope: "second" });
    expect(result.current.selected).toEqual([]);
    rerender({ scope: "first" });
    expect(result.current.selected).toEqual([]);
  });
});
