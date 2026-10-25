import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgeWikiTree } from "./knowledge-wiki-tree";
import type { WikiPage, WikiTheme } from "./knowledge-wiki-queries";

it("系统未分类随内容出现和消失，保留普通空目录", () => {
  const themes = [
    { id: "system", name: "未分类", systemKey: "unclassified" },
    { id: "manual", name: "普通空目录" },
    { id: "same-name", name: "未分类" },
  ];
  const view = (pages: WikiPage[], folders: WikiTheme[] = themes) => (
    <TooltipProvider>
      <KnowledgeWikiTree
        pages={pages}
        themes={folders}
        selectedPage=""
        onPage={vi.fn()}
        onFolder={vi.fn()}
        onCreate={vi.fn()}
        onFolderAction={vi.fn()}
        writable={false}
      />
    </TooltipProvider>
  );
  const { rerender } = render(view([]));
  expect(
    screen.queryByRole("button", { name: "列表视图" }),
  ).not.toBeInTheDocument();
  expect(screen.getAllByRole("treeitem", { name: "未分类" })).toHaveLength(1);
  expect(screen.getByRole("treeitem", { name: "普通空目录" })).toBeVisible();

  rerender(view([{ id: "page", title: "待分类页面", themeId: "system" }]));
  expect(screen.getAllByRole("treeitem", { name: "未分类" })).toHaveLength(2);
  expect(screen.getByRole("treeitem", { name: "待分类页面" })).toBeVisible();

  rerender(
    view([], [...themes, { id: "child", name: "子目录", parentId: "system" }]),
  );
  expect(screen.getAllByRole("treeitem", { name: "未分类" })).toHaveLength(2);
  expect(screen.getByRole("treeitem", { name: "子目录" })).toBeVisible();

  rerender(view([]));
  expect(screen.getAllByRole("treeitem", { name: "未分类" })).toHaveLength(1);
});
