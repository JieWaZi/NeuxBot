import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import type * as PluginModule from "@/modules/plugin";
import type { PluginBrowseEntry } from "@/modules/plugin";
import {
  changeResourceCategory,
  resourceCategoryFromParams,
  resourceLibraryHref,
} from "../pages/resources-navigation";
import { ResourcesContextSidebar } from "./resources-context-sidebar";
const state = vi.hoisted(() => ({
  entries: [] as PluginBrowseEntry[],
  hasMore: false,
  loadMore: vi.fn(),
  browse: vi.fn(),
}));
vi.mock("@/modules/plugin", async (original) => ({
  ...(await original<typeof PluginModule>()),
  usePluginBrowse: (options: unknown) => {
    state.browse(options);
    return {
      entries: state.entries,
      hasNextPage: state.hasMore,
      isRefetchError: false,
      isFetching: false,
      isError: false,
      fetchNextPage: state.loadMore,
    };
  },
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.entries = [];
  state.hasMore = false;
});
describe("ResourcesContextSidebar", () => {
  it("shows the library title, four categories and one current category", async () => {
    const onCategoryChange = vi.fn();
    const user = userEvent.setup();
    render(
      <ResourcesContextSidebar
        category="skills"
        onCategoryChange={onCategoryChange}
      />,
    );
    expect(screen.getByRole("heading", { name: "资源库" })).toBeVisible();
    expect(screen.getByRole("button", { name: "技能" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    for (const label of ["插件", "技能", "连接器", "工作流"])
      expect(screen.getByRole("button", { name: label })).toBeVisible();
    expect(screen.queryByText("已安装")).toBeNull();
    await user.click(screen.getByRole("button", { name: "连接器" }));
    expect(onCategoryChange).toHaveBeenCalledExactlyOnceWith("connectors");
    await user.click(screen.getByRole("button", { name: "工作流" }));
    expect(onCategoryChange).toHaveBeenLastCalledWith("workflows");
  });
  it("uses an unfiltered installed window and selects only the active shortcut", async () => {
    state.entries = [
      {
        sourceKey: "source:office",
        pluginId: "p1",
        name: "办公",
        installedCount: 2,
        icon: "data:image/png;base64,light",
        icons: {
          default: "data:image/png;base64,light",
          dark: "data:image/png;base64,dark",
        },
      },
    ];
    const onPluginSelect = vi.fn();
    const user = userEvent.setup();
    render(
      <ResourcesContextSidebar
        category="plugins"
        currentPath="/resources/plugins/p1"
        onCategoryChange={vi.fn()}
        onPluginSelect={onPluginSelect}
      />,
    );
    expect(state.browse).toHaveBeenCalledWith({
      installedOnly: true,
      active: true,
    });
    expect(screen.getByRole("button", { name: "办公" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "插件" })).toHaveAttribute(
      "aria-current",
      "location",
    );
    expect(document.querySelectorAll('[aria-current="page"]')).toHaveLength(1);
    expect(screen.getByRole("button", { name: "插件" })).not.toHaveClass(
      "bg-resource-hover",
    );
    expect(screen.getByRole("button", { name: "办公" })).toHaveClass(
      "bg-resource-hover",
    );
    const images = screen
      .getByRole("button", { name: "办公" })
      .querySelectorAll("img");
    expect(images).toHaveLength(2);
    expect(images[1]).toHaveAttribute("src", "data:image/png;base64,dark");
    expect(images[1]).toHaveClass("dark:block");
    await user.click(screen.getByRole("button", { name: "办公" }));
    expect(onPluginSelect).toHaveBeenCalledWith("/resources/plugins/p1");
  });
  it.each(["skills/guide", "connectors/github"])(
    "keeps plugin component %s under its installed shortcut only",
    (component) => {
      state.entries = [
        {
          sourceKey: "source:office",
          pluginId: "p1",
          name: "办公",
          installedCount: 1,
        },
      ];
      render(
        <ResourcesContextSidebar
          category="plugins"
          currentPath={`/resources/plugins/p1/${component}`}
          onCategoryChange={vi.fn()}
        />,
      );
      expect(screen.getByRole("button", { name: "办公" })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(screen.getByRole("button", { name: "插件" })).toHaveAttribute(
        "aria-current",
        "location",
      );
      for (const name of ["插件", "技能", "连接器"])
        expect(screen.getByRole("button", { name })).not.toHaveClass(
          "bg-resource-hover",
        );
      expect(screen.getByRole("button", { name: "技能" })).not.toHaveAttribute(
        "aria-current",
      );
      expect(
        screen.getByRole("button", { name: "连接器" }),
      ).not.toHaveAttribute("aria-current");
    },
  );
  it("shows the complete installed directory without a load-more row", () => {
    state.entries = Array.from({ length: 20 }, (_, index) => ({
      sourceKey: `s${index}`,
      pluginId: `p${index}`,
      name: `办公${index}`,
      installedCount: 1,
    }));
    render(
      <ResourcesContextSidebar category="plugins" onCategoryChange={vi.fn()} />,
    );
    expect(screen.getByRole("button", { name: "办公19" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "加载更多" })).toBeNull();
  });
});
it("category changes clear category filters while retaining partner and unrelated parameters", () => {
  const params = new URLSearchParams(
    "category=skills&search=review&source=plugins&skillCategory=writing&page=4&componentPage=3&partner=w1&keep=value",
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
it("shortcut navigation retains original list search and partner across detail hops", () => {
  const back = "/resources?category=plugins&search=review&partner=w1";
  expect(
    resourceLibraryHref({
      pathname: "/resources/plugins/p1",
      search: "?version=v1",
      state: { resourceBack: back },
    }),
  ).toBe(back);
  expect(
    resourceLibraryHref({
      pathname: "/resources/plugins/p1",
      search: "?search=review&partner=w1&version=v1",
      state: null,
    }),
  ).toBe(back);
  expect(
    resourceLibraryHref({
      pathname: "/resources/plugins/p1",
      search: "",
      state: { resourceBack: "https://example.org" },
    }),
  ).toBe("/resources?category=plugins");
});

it.each(["writing", ""])(
  "retains skill category %j through detail fallback and clears it when leaving skills",
  (category) => {
    const query = new URLSearchParams({
      category: "skills",
      skillCategory: category,
      search: "review",
      partner: "w1",
    });
    const back = `/resources?${query.toString()}`;
    expect(
      resourceLibraryHref({
        pathname: "/resources/skills/s1",
        search: "",
        state: { resourceBack: back },
      }),
    ).toBe(back);
    const fallback = resourceLibraryHref({
      pathname: "/resources/skills/s1",
      search: `?${query.toString()}`,
      state: null,
    });
    const params = new URLSearchParams(fallback.split("?")[1]);
    expect(params.get("category")).toBe("skills");
    expect(params.has("skillCategory")).toBe(true);
    expect(params.get("skillCategory")).toBe(category);
    expect(params.get("search")).toBe("review");
    expect(params.get("partner")).toBe("w1");
    expect(changeResourceCategory(query, "skills").get("skillCategory")).toBe(
      category,
    );
    expect(
      changeResourceCategory(query, "connectors").has("skillCategory"),
    ).toBe(false);
  },
);

it("keeps the original list context when a shortcut is selected from a fixed plugin component", () => {
  const back = "/resources?category=plugins&search=review&partner=w1";
  expect(
    resourceLibraryHref({
      pathname: "/resources/plugins/p1/skills/guide",
      search: "?version=v1",
      state: {
        resourceBack: "/resources/plugins/p1?version=v1",
        pluginResourceBack: back,
      },
    }),
  ).toBe(back);
  expect(
    resourceLibraryHref({
      pathname: "/resources/plugins/p1/connectors/calendar",
      search: "?version=v1",
      state: {
        resourceBack: "/resources/plugins/p1",
        pluginResourceBack: "https://example.org",
      },
    }),
  ).toBe("/resources?category=plugins");
});
