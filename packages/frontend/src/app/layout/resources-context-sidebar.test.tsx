import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import {
  changeResourceCategory,
  resourceCategoryFromParams,
} from "../pages/resources-navigation";
import { ResourcesContextSidebar } from "./resources-context-sidebar";

describe("ResourcesContextSidebar", () => {
  it("分类导航显示当前资源类型并复用外部路由更新", async () => {
    const onCategoryChange = vi.fn();
    const user = userEvent.setup();
    render(
      <ResourcesContextSidebar
        category="skills"
        onCategoryChange={onCategoryChange}
      />,
    );
    expect(screen.getByRole("button", { name: "技能" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    for (const category of ["插件", "技能", "连接器"])
      expect(screen.getByRole("button", { name: category })).toBeVisible();
    expect(screen.queryByRole("button", { name: "工作流" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "连接器" }));
    expect(onCategoryChange).toHaveBeenCalledExactlyOnceWith("connectors");
  });
});

it("分类迁移保留伙伴目标，精确清理不适用的筛选和分页", () => {
  const params = new URLSearchParams(
    "category=skills&search=review&view=mine&source=plugins&page=4&componentPage=3&partner=w1&keep=value",
  );
  expect(changeResourceCategory(params, "connectors").toString()).toBe(
    "category=connectors&partner=w1&keep=value",
  );
  expect(params.get("search")).toBe("review");
  expect(
    resourceCategoryFromParams(new URLSearchParams("category=workflows")),
  ).toBe("workflows");
  expect(
    resourceCategoryFromParams(new URLSearchParams("category=workflows"), "w1"),
  ).toBe("plugins");
  expect(
    resourceCategoryFromParams(new URLSearchParams("category=unknown")),
  ).toBe("plugins");
});
