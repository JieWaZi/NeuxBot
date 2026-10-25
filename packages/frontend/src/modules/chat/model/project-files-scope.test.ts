import { describe, expect, it } from "vitest";

import { projectFilesScopePath } from "./project-files-scope";

describe("projectFilesScopePath", () => {
  it("uses managed projects namespace after first send", () => {
    expect(projectFilesScopePath("chat-1")).toBe("projects");
  });

  it("lists workspace root directly in draft", () => {
    expect(projectFilesScopePath()).toBe("");
    expect(projectFilesScopePath("")).toBe("");
  });
});
