import { expect, it } from "vitest";
import { resourceListBack } from "./resource-navigation";
it("preserves valid source and search filters for the same resource scope", () => {
  expect(
    resourceListBack(
      "/resources?category=skills&source=plugins&search=review",
      "/resources?category=skills",
    ),
  ).toBe("/resources?category=skills&source=plugins&search=review");
  expect(
    resourceListBack(
      "/wakers/ada/resources?view=mine&search=desk",
      "/wakers/ada/resources",
    ),
  ).toBe("/wakers/ada/resources?view=mine&search=desk");
});
it("rejects external, detail and other-owner destinations", () => {
  for (const value of [
    "https://example.com/resources",
    "//example.com/resources",
    "/wakers/grace/resources",
    "/resources/plugins/abc",
    "javascript:alert(1)",
  ])
    expect(resourceListBack(value, "/wakers/ada/resources")).toBe(
      "/wakers/ada/resources",
    );
});
