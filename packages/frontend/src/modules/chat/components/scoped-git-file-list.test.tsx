import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ScopedGitFileList } from "./scoped-git-file-list";

it("文件选择覆盖整行，暂存与丢弃操作不会同时选中文件", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const onAction = vi.fn();
  const onDiscard = vi.fn();
  render(
    <ScopedGitFileList
      title="工作区"
      entries={[
        {
          path: "src/main.ts",
          xy: " M",
          stagedAddedLines: 0,
          stagedDeletedLines: 0,
          unstagedAddedLines: 44,
          unstagedDeletedLines: 2,
        },
      ]}
      staged={false}
      busy={false}
      onSelect={onSelect}
      onAction={onAction}
      onDiscard={onDiscard}
    />,
  );
  const row = screen.getByRole("button", { name: /^src\/main.ts/ });
  expect(row).toHaveClass("absolute", "inset-0");
  await user.click(row);
  expect(onSelect).toHaveBeenCalledExactlyOnceWith({
    path: "src/main.ts",
    staged: false,
  });
  await user.click(screen.getByRole("button", { name: "暂存 src/main.ts" }));
  await user.click(
    screen.getByRole("button", { name: "丢弃 src/main.ts 的未暂存改动" }),
  );
  expect(onAction).toHaveBeenCalledExactlyOnceWith("src/main.ts");
  expect(onDiscard).toHaveBeenCalledExactlyOnceWith("src/main.ts");
  expect(onSelect).toHaveBeenCalledTimes(1);
});
