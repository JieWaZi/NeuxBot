import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatSummary } from "./chat-summary";

const { getWorkspaceGitChanges, listChatWorkspaceFiles } = vi.hoisted(() => ({
  getWorkspaceGitChanges: vi.fn(async (root: string) => ({
    isGitRepo: true,
    files: [
      {
        path: "a.ts",
        name: "a.ts",
        status: "modified" as const,
        addedLines: 2,
        deletedLines: 1,
        gitRoot: root,
      },
    ],
    totalAddedLines: root === "/projects/one" ? 12 : 4,
    totalDeletedLines: root === "/projects/one" ? 3 : 1,
  })),
  listChatWorkspaceFiles: vi.fn(async () => ({
    generated: [
      { name: "second.txt", relativePath: "second.txt" },
      { name: "third.txt", relativePath: "third.txt" },
      { name: "fourth.txt", relativePath: "fourth.txt" },
    ],
    removedPaths: [],
  })),
}));

vi.mock("@/shared/platform", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  getWorkspaceGitChanges,
  listChatWorkspaceFiles,
}));

it("摘要按目录展示修改、只预览会话文件，并把完整列表交给工作台", async () => {
  const user = userEvent.setup();
  const onViewAllFiles = vi.fn();
  const onOpenFile = vi.fn();
  const onOpenChanges = vi.fn();
  const onMemberOpened = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ChatSummary
        chatId="chat-1"
        chatFiles={[{ name: "first.txt", path: "first.txt", source: "chat" }]}
        gitRoots={["/projects/one", "/projects/two"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={onViewAllFiles}
        onOpenFile={onOpenFile}
        onOpenChanges={onOpenChanges}
        renderTeamMembers={(close) => (
          <button
            onClick={() => {
              onMemberOpened();
              close();
            }}
          >
            成员一
          </button>
        )}
      />
    </QueryClientProvider>,
  );

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  expect(screen.getByRole("button", { name: "查看摘要" }).textContent).toBe("");
  const projects = screen.getByRole("region", { name: "项目目录修改" });
  expect(
    await within(projects).findByRole("button", { name: "one 的改动" }),
  ).toBeVisible();
  expect(
    within(projects).getByRole("button", { name: "two 的改动" }),
  ).toBeVisible();
  expect(within(projects).getByText("+12")).toBeVisible();
  expect(within(projects).getByText("-3")).toBeVisible();
  const files = screen.getByRole("region", { name: "会话文件" });
  expect(
    await within(files).findByRole("button", { name: "third.txt" }),
  ).toBeVisible();
  expect(
    within(files).queryByRole("button", { name: "fourth.txt" }),
  ).toBeNull();
  await user.click(within(files).getByRole("button", { name: "查看全部" }));
  expect(onViewAllFiles).toHaveBeenCalledOnce();

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(screen.getByRole("button", { name: "first.txt" }));
  expect(onOpenFile).toHaveBeenCalledWith({
    name: "first.txt",
    path: "first.txt",
    source: "chat",
  });

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(screen.getByRole("button", { name: "two 的改动" }));
  expect(onOpenChanges).toHaveBeenCalledWith("/projects/two");

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(screen.getByRole("button", { name: "成员一" }));
  expect(onMemberOpened).toHaveBeenCalledOnce();
});
