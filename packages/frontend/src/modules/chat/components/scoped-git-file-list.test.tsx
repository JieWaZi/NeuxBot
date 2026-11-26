import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import {
  ScopedGitChangedTree,
  ScopedGitFileList,
} from "./scoped-git-file-list";

it("暂存和丢弃独立于展开补丁与打开普通文件", async () => {
  const user = userEvent.setup();
  const onOpenFile = vi.fn();
  const onToggle = vi.fn();
  const onStage = vi.fn();
  const onDiscard = vi.fn();
  render(
    <ScopedGitFileList
      group={{
        id: ":unstaged",
        title: "工作区",
        files: [
          { path: "src/main.ts", status: "M", addedLines: 44, deletedLines: 2 },
        ],
      }}
      busy={false}
      expanded={() => false}
      onOpenFile={onOpenFile}
      onToggle={onToggle}
      onStage={onStage}
      onDiscard={onDiscard}
      renderPatch={() => null}
    />,
  );
  await user.click(screen.getByRole("button", { name: "暂存 src/main.ts" }));
  await user.click(
    screen.getByRole("button", { name: "丢弃 src/main.ts 的未暂存改动" }),
  );
  expect(onStage).toHaveBeenCalledExactlyOnceWith("src/main.ts");
  expect(onDiscard).toHaveBeenCalledExactlyOnceWith("src/main.ts");
  expect(onOpenFile).not.toHaveBeenCalled();
  expect(onToggle).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "展开 src/main.ts" }));
  expect(onToggle).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "src/main.ts" }));
  expect(onOpenFile).toHaveBeenCalledExactlyOnceWith("src/main.ts");
});

it("改动树沿用文件树的行高和缩进，并显示文件的实际 Git 状态", async () => {
  const user = userEvent.setup();
  const onJump = vi.fn();
  render(
    <ScopedGitChangedTree
      groups={[
        {
          id: "HEAD",
          files: [
            {
              path: "src/main.ts",
              status: "M",
              addedLines: 1,
              deletedLines: 1,
            },
            { path: "src/new.ts", status: "A", addedLines: 2, deletedLines: 0 },
            { path: "src/old.ts", status: "D", addedLines: 0, deletedLines: 3 },
          ],
        },
      ]}
      focused={'["HEAD","src/main.ts"]'}
      onJump={onJump}
    />,
  );

  const tree = screen.getByRole("tree");
  const folder = within(tree).getByRole("button", { name: "src" });
  const modified = within(tree).getByRole("treeitem", { name: "main.ts" });
  const added = within(tree).getByRole("treeitem", { name: "new.ts" });
  const deleted = within(tree).getByRole("treeitem", { name: "old.ts" });
  expect(folder).toHaveClass("min-h-row-compact", "type-ui");
  expect(modified).toHaveClass("min-h-row-compact", "type-ui", "bg-muted");
  expect(modified).toHaveStyle({ paddingLeft: "40px" });
  expect(modified).toHaveAttribute("aria-selected", "true");
  expect(modified).toHaveAccessibleDescription("修改");
  expect(within(modified).getByText("M")).toBeVisible();
  expect(within(modified).getByText("main.ts").parentElement).toHaveClass(
    "text-status-info-content",
  );
  expect(added).toHaveAccessibleDescription("新增");
  expect(within(added).getByText("A")).toBeVisible();
  expect(within(added).getByText("new.ts").parentElement).toHaveClass(
    "text-success",
  );
  expect(deleted).toHaveAccessibleDescription("删除");
  expect(within(deleted).getByText("D")).toBeVisible();

  await user.click(folder);
  expect(within(tree).queryByRole("treeitem", { name: "new.ts" })).toBeNull();
  await user.click(folder);
  await user.click(added);
  expect(onJump).toHaveBeenCalledExactlyOnceWith('["HEAD","src/new.ts"]');
});
