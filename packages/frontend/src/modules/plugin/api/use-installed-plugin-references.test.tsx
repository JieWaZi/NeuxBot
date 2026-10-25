import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useInstalledPluginReferences } from "./use-installed-plugin-references";

vi.mock("./use-plugin-resources", () => ({
  usePluginResources: () => ({
    data: [
      {
        plugin: { id: "installed", name: "办公" },
        installations: [{ wakerId: "w1", versionId: "v1", enabled: true }],
        versions: [{ id: "v1", version: "1.0" }],
        ready: false,
        account: { available: false },
      },
      {
        plugin: { id: "disabled" },
        installations: [{ wakerId: "w1", versionId: "v2", enabled: false }],
        versions: [{ id: "v2" }],
        ready: true,
      },
    ],
  }),
}));

it("keeps enabled installed references before authorization is checked after restart", () => {
  const { result } = renderHook(() => useInstalledPluginReferences("w1"));
  expect(result.current.plugins).toEqual([
    {
      id: "installed",
      name: "办公",
      description: "",
      version: "1.0",
      versionId: "v1",
    },
  ]);
});
