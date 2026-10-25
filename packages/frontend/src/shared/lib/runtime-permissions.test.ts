import { describe, expect, it } from "vitest";

import {
  modelPermissionLevels,
  runtimeDefaultPermissionLevels,
  safeDefaultPermissionLevel,
  supportedPermissionLevels,
} from "./runtime-permissions";

describe("runtime permissions", () => {
  it("does not invent product levels when the catalog is empty", () => {
    expect(supportedPermissionLevels().size).toBe(0);
    expect(supportedPermissionLevels([]).size).toBe(0);
    expect(modelPermissionLevels(undefined, "gpt-5").size).toBe(0);
    expect(modelPermissionLevels([{ id: "gpt-5" }], "gpt-5").size).toBe(0);
  });

  it("keeps the preferred default when no levels are declared", () => {
    expect(safeDefaultPermissionLevel("full-access", new Set())).toBe(
      "full-access",
    );
  });

  it("downgrades an unsupported default to auto then default", () => {
    expect(
      safeDefaultPermissionLevel("full-access", new Set(["default", "auto"])),
    ).toBe("auto");
    expect(
      safeDefaultPermissionLevel("full-access", new Set(["default"])),
    ).toBe("default");
  });

  it("reads permission levels from the selected model only", () => {
    const models = [
      {
        id: "sonnet",
        permissionLevels: ["default", "auto"],
      },
      {
        id: "gpt-5",
        permissionLevels: ["default", "auto", "full-access"],
      },
    ];
    expect([...modelPermissionLevels(models, "sonnet")]).toEqual([
      "default",
      "auto",
    ]);
    expect(
      runtimeDefaultPermissionLevels({
        defaultModel: "gpt-5",
        models,
      }).has("full-access"),
    ).toBe(true);
  });
});
