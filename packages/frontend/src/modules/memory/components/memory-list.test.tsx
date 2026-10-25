import { describe, expect, it } from "vitest";

import { splitMemoryContent } from "./memory-list";

describe("splitMemoryContent", () => {
  it("uses the first line as the timeline title", () => {
    expect(
      splitMemoryContent("在个人记忆中沉淀角色定位\n只在复杂任务时展开说明。"),
    ).toEqual({
      description: "只在复杂任务时展开说明。",
      title: "在个人记忆中沉淀角色定位",
    });
  });

  it("keeps a single paragraph as the title", () => {
    expect(splitMemoryContent("用户偏好深色模式")).toEqual({
      description: "",
      title: "用户偏好深色模式",
    });
  });
});
