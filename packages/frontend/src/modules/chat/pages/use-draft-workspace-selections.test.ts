import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  draftWorkspaceStorageKey,
  useDraftWorkspaceSelections,
} from "./use-draft-workspace-selections";

beforeEach(() => {
  const values = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: vi.fn((key: string) => values.get(key) ?? null),
      setItem: vi.fn((key: string, value: string) => values.set(key, value)),
    },
  });
});

describe("new conversation workspace preferences", () => {
  it("restores the latest explicit directory or project after remount, separately for each partner and team", () => {
    const first = renderHook(useDraftWorkspaceSelections);
    act(() =>
      first.result.current[1]("a", { type: "folder", path: "/work/目录" }),
    );
    act(() =>
      first.result.current[1]("team:b", {
        type: "project",
        projectId: "project-b",
      }),
    );
    first.unmount();

    const next = renderHook(useDraftWorkspaceSelections);
    expect(next.result.current[0]).toEqual({
      a: { type: "folder", path: "/work/目录" },
      "team:b": { type: "project", projectId: "project-b" },
    });
    act(() =>
      next.result.current[1]("a", { type: "project", projectId: "project-a" }),
    );
    next.unmount();
    expect(renderHook(useDraftWorkspaceSelections).result.current[0].a).toEqual(
      { type: "project", projectId: "project-a" },
    );
  });

  it("ignores malformed preferences and keeps valid entries", () => {
    window.localStorage.setItem(
      draftWorkspaceStorageKey,
      JSON.stringify({
        valid: { type: "folder", path: "/work" },
        empty: { type: "project", projectId: "" },
        invalid: { type: "folder", path: 42 },
      }),
    );
    expect(renderHook(useDraftWorkspaceSelections).result.current[0]).toEqual({
      valid: { type: "folder", path: "/work" },
    });
    window.localStorage.setItem(draftWorkspaceStorageKey, "{");
    expect(renderHook(useDraftWorkspaceSelections).result.current[0]).toEqual(
      {},
    );
  });

  it("still allows selection when local storage is unavailable", () => {
    vi.mocked(window.localStorage.getItem).mockImplementation(() => {
      throw new Error("unavailable");
    });
    vi.mocked(window.localStorage.setItem).mockImplementation(() => {
      throw new Error("unavailable");
    });
    const { result } = renderHook(useDraftWorkspaceSelections);
    act(() => result.current[1]("a", { type: "folder", path: "/work" }));
    expect(result.current[0].a).toEqual({ type: "folder", path: "/work" });
  });
});
