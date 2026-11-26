import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ScopedGitFileList } from "./scoped-git-file-list";

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
