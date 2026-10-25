import { expect, it } from "vitest";

import { resolveGitWorkspaceDirectories } from "./git-workspace-directories";

it("优先使用已冻结的项目源目录", () => {
  expect(
    resolveGitWorkspaceDirectories({
      additionalWorkingDirectories: ["/tmp/default", "/tmp/other"],
      selectedDraftProject: {
        sources: [{ path: "/tmp/draft", available: true }],
      },
      workspaceDirectory: "/tmp/folder",
    }),
  ).toEqual(["/tmp/default", "/tmp/other"]);
});

it("草稿项目按默认目录优先返回全部源目录", () => {
  expect(
    resolveGitWorkspaceDirectories({
      selectedDraftProject: {
        sources: [
          { path: "/tmp/backend", available: true },
          { path: "/tmp/frontend", available: true, isDefault: true },
          { path: "/tmp/missing", available: false },
        ],
      },
    }),
  ).toEqual(["/tmp/frontend", "/tmp/backend"]);
});

it("无项目时回退到临时文件夹", () => {
  expect(
    resolveGitWorkspaceDirectories({
      workspaceDirectory: "/tmp/folder",
    }),
  ).toEqual(["/tmp/folder"]);
});

it("受管临时工作区不把父目录仓库作为聊天的项目", () => {
  expect(
    resolveGitWorkspaceDirectories({
      managed: true,
      workspaceDirectory: "/repo/.scratch/chat/workspace",
      additionalWorkingDirectories: ["/repo/.scratch/chat/workspace"],
    }),
  ).toEqual([]);
});
