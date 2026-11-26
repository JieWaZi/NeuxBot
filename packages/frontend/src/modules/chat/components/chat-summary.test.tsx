import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, onTestFinished, vi } from "vitest";

import "@/shared/i18n";
import type {
  ScopedGitStatus,
  WorkspaceGitAction,
  WorkspaceGitActionResult,
} from "@/shared/platform";
import { configurePlatform, platform } from "@/shared/platform/provider";
import type { WorkspaceResourceChangeEvent } from "@neuxbot/platform-contracts/resources";
import type { WorkspaceService } from "@neuxbot/platform-contracts/services";
import { ChatSummary } from "./chat-summary";

const {
  getScopedGitStatus,
  getScopedGitComparison,
  listWorkspaceGitBranches,
  previewWorkspaceGitCheckout,
  runWorkspaceGitAction,
  listChatWorkspaceFiles,
} = vi.hoisted(() => ({
  getScopedGitStatus: vi.fn(
    async (_chatId: string, root: string): Promise<ScopedGitStatus> => ({
      isRepo: true,
      root,
      repositories: [root],
      branch: "feature",
      head: "abcdef1",
      entries: [
        {
          path: "staged.ts",
          xy: "M ",
          stagedAddedLines: 2,
          stagedDeletedLines: 1,
          unstagedAddedLines: 0,
          unstagedDeletedLines: 0,
        },
      ],
    }),
  ),
  getScopedGitComparison: vi.fn(async (chatId: string, root: string) => ({
    root,
    branch: "feature",
    baseRef: chatId === "chat-push" ? "upstream/release" : "origin/main",
    entries: [
      {
        path: "a.ts",
        status: "M",
        addedLines: root === "/projects/one" ? 12 : 4,
        deletedLines: root === "/projects/one" ? 3 : 1,
      },
    ],
  })),
  listWorkspaceGitBranches: vi.fn(async () => ({
    isGitRepo: true,
    branches: [
      { name: "feature", isCurrent: true, isRemote: false },
      { name: "main", isCurrent: false, isRemote: false },
    ],
  })),
  previewWorkspaceGitCheckout: vi.fn(async () => ({
    canCheckout: true,
    conflicts: [],
  })),
  runWorkspaceGitAction: vi.fn<
    (input: WorkspaceGitAction) => Promise<WorkspaceGitActionResult>
  >(async () => ({ commit: "new-commit" })),
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
  getScopedGitStatus,
  getScopedGitComparison,
  listWorkspaceGitBranches,
  previewWorkspaceGitCheckout,
  runWorkspaceGitAction,
  listChatWorkspaceFiles,
}));

it("重试 Git 摘要中的实时错误时只重建对应仓库根的观察", async () => {
  const listeners = new Set<(event: WorkspaceResourceChangeEvent) => void>();
  const registrations: string[] = [];
  const workspace: Partial<WorkspaceService> = {
    open_workspace_scope: async () => ({ id: "scope", root: "/workspace" }),
    watch_workspace_resource: async (input) => {
      const subscriptionId = `${input.root}:${registrations.length}`;
      registrations.push(subscriptionId);
      return {
        subscriptionId,
        sequence: 0,
        state: "recovering",
        resource: null,
      };
    },
    unwatch_workspace_resource: async () => true,
    onWorkspaceResourceChange: (handler) => {
      listeners.add(handler);
      return () => listeners.delete(handler);
    },
  };
  const previousPlatform = platform();
  onTestFinished(() => configurePlatform(previousPlatform));
  configurePlatform({
    ...previousPlatform,
    workspace: workspace as WorkspaceService,
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatSummary
        pane={document.body}
        chatId="chat-watch-retry"
        chatFiles={[]}
        gitRoots={["/projects/one", "/projects/two"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await waitFor(() => expect(registrations).toHaveLength(3));
  const readSubscription = (root: string) => {
    const subscriptionId = registrations.findLast((id) =>
      id.startsWith(`${root}:`),
    );
    if (!subscriptionId) throw new Error(`Missing observation for ${root}`);
    return subscriptionId;
  };
  const report = (subscriptionId: string, state: "failed" | "ready") => {
    act(() => {
      for (const listener of listeners)
        listener({ subscriptionId, sequence: 1, state, resource: null });
    });
  };
  const failed = readSubscription("/projects/one");
  report(failed, "failed");
  const retry = /one.*Git 文件改动暂时无法读取/;
  await user.click(await screen.findByRole("button", { name: retry }));
  await waitFor(() =>
    expect(readSubscription("/projects/one")).not.toBe(failed),
  );
  expect(registrations).toHaveLength(4);
  report(readSubscription("/projects/one"), "ready");
  expect(screen.queryByRole("button", { name: retry })).toBeNull();
});

it("从会话详情切换分支时提交所显示的仓库与 HEAD", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ChatSummary
        pane={document.body}
        chatId="chat-branches"
        chatFiles={[]}
        gitRoots={["/projects/one"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(await screen.findByRole("button", { name: "feature" }));
  await user.click(await screen.findByRole("button", { name: "main" }));
  expect(previewWorkspaceGitCheckout).toHaveBeenCalledWith(
    "/projects/one",
    "main",
    { chatId: "chat-branches", repository: "/projects/one" },
  );
  expect(runWorkspaceGitAction).toHaveBeenCalledWith(
    expect.objectContaining({
      action: "checkout",
      branch: "main",
      expectedBranch: "feature",
      expectedHead: "abcdef1",
    }),
  );
});

it("会话详情提交留空时交给 Core 自动生成消息", async () => {
  const observed = await getScopedGitStatus("chat-git", "/projects/one");
  getScopedGitStatus.mockResolvedValueOnce({
    ...observed,
    entries: observed.entries.map((entry) => ({
      ...entry,
      stagedAddedLines: 2055,
    })),
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ChatSummary
        pane={document.body}
        chatId="chat-git"
        chatFiles={[]}
        gitRoots={["/projects/one"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(await screen.findByRole("button", { name: "提交" }));
  const dialog = await screen.findByRole("dialog", { name: "提交" });
  expect(within(dialog).getByText("staged.ts")).toBeVisible();
  expect(within(dialog).getByText("feature")).toBeVisible();
  expect(within(dialog).getAllByText("+2,055")).toHaveLength(2);
  expect(within(dialog).getAllByText("−1")).toHaveLength(2);
  expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
  expect(
    within(dialog).getByRole("textbox", { name: "提交说明（可选）" }),
  ).toHaveValue("");
  await user.click(within(dialog).getByRole("button", { name: "提交" }));
  expect(runWorkspaceGitAction).toHaveBeenCalledWith(
    expect.objectContaining({
      chatId: "chat-git",
      action: "commit",
      message: "",
      filePaths: undefined,
      expectedBranch: "feature",
      expectedHead: "abcdef1",
    }),
  );
});

it("未暂存文件可直接选择提交，留空只发送实际勾选的路径", async () => {
  const previous = await getScopedGitStatus("chat-files", "/projects/one");
  const [changed] = previous.entries;
  if (!changed) throw new Error("missing Git status fixture");
  getScopedGitStatus.mockResolvedValueOnce({
    ...previous,
    index: "",
    entries: [
      { ...changed, path: "work.ts", xy: " M" },
      { ...changed, path: "local-notes.txt", xy: "??" },
    ],
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ChatSummary
        pane={document.body}
        chatId="chat-files"
        chatFiles={[]}
        gitRoots={["/projects/one"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(await screen.findByRole("button", { name: "提交" }));
  const dialog = await screen.findByRole("dialog", { name: "提交" });
  expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "编辑" }));
  expect(
    within(dialog).getByRole("checkbox", { name: "work.ts" }),
  ).toBeChecked();
  await user.click(within(dialog).getByText("local-notes.txt"));
  expect(
    within(dialog).getByRole("checkbox", { name: "local-notes.txt" }),
  ).not.toBeChecked();
  await user.click(within(dialog).getByRole("button", { name: "完成" }));
  expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
  expect(within(dialog).queryByText("local-notes.txt")).not.toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "提交" }));
  expect(runWorkspaceGitAction).toHaveBeenLastCalledWith(
    expect.objectContaining({
      action: "commit",
      message: "",
      filePaths: ["work.ts"],
      expectedIndex: "",
    }),
  );
});

function renderGitSummary(chatId: string) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatSummary
        pane={document.body}
        chatId={chatId}
        chatFiles={[]}
        gitRoots={["/projects/one"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
}

it("Git 菜单只保留提交、推送和创建 PR", async () => {
  const previous = await getScopedGitStatus("chat-actions", "/projects/one");
  getScopedGitStatus.mockResolvedValueOnce({
    ...previous,
    upstream: "origin/feature",
    remote: "origin",
    github: true,
    defaultBranch: "main",
    ahead: 1,
    aheadOfDefault: 1,
  });
  const user = userEvent.setup();
  renderGitSummary("chat-actions");
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(await screen.findByRole("button", { name: "Git 操作" }));
  const menu = await screen.findByRole("menu");
  expect(within(menu).getAllByRole("menuitem")).toHaveLength(3);
  for (const name of ["提交", "推送", "创建 PR"]) {
    expect(within(menu).getByRole("menuitem", { name })).toBeVisible();
  }
});

it("新分支提交失败后保留说明并只重试提交", async () => {
  const mock = getScopedGitStatus.getMockImplementation();
  const previous = await getScopedGitStatus("chat-new-branch", "/projects/one");
  const status = {
    ...previous,
    index: "index-snapshot",
    upstream: "origin/feature",
  };
  getScopedGitStatus.mockResolvedValue(status);
  const before = runWorkspaceGitAction.mock.calls.length;
  runWorkspaceGitAction
    .mockImplementationOnce(async () => {
      getScopedGitStatus.mockResolvedValue({
        ...status,
        branch: "work/notes",
        upstream: "",
      });
      return { branch: "work/notes" };
    })
    .mockRejectedValueOnce(new Error("Commit failed after branch creation"))
    .mockResolvedValueOnce({ branch: "work/notes", commit: "new-commit" });
  const user = userEvent.setup();
  try {
    renderGitSummary("chat-new-branch");
    await user.click(screen.getByRole("button", { name: "查看摘要" }));
    await user.click(await screen.findByRole("button", { name: "提交" }));
    const dialog = await screen.findByRole("dialog", { name: "提交" });
    await user.type(
      within(dialog).getByRole("textbox", { name: "提交说明（可选）" }),
      "Keep draft",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "在新分支提交" }),
    );
    const branch = await screen.findByRole("dialog", { name: "在新分支提交" });
    await user.type(
      within(branch).getByRole("textbox", { name: "新分支名称" }),
      "work/notes",
    );
    await user.click(
      within(branch).getByRole("button", { name: "创建分支并提交" }),
    );
    expect(
      await screen.findByText("Commit failed after branch creation"),
    ).toBeVisible();
    const retry = screen.getByRole("dialog", { name: "提交" });
    expect(within(retry).getByText("work/notes")).toBeVisible();
    expect(
      within(retry).getByRole("textbox", { name: "提交说明（可选）" }),
    ).toHaveValue("Keep draft");
    await user.click(within(retry).getByRole("button", { name: "提交" }));
    const calls = runWorkspaceGitAction.mock.calls
      .slice(before)
      .map(([x]) => x);
    expect(calls).toMatchObject([
      {
        action: "create_branch",
        branch: "work/notes",
        expectedBranch: "feature",
        expectedHead: "abcdef1",
      },
      {
        action: "commit",
        expectedBranch: "work/notes",
        expectedHead: "abcdef1",
        expectedIndex: "index-snapshot",
        message: "Keep draft",
      },
      {
        action: "commit",
        expectedBranch: "work/notes",
        expectedHead: "abcdef1",
        expectedIndex: "index-snapshot",
        message: "Keep draft",
      },
    ]);
  } finally {
    if (mock) getScopedGitStatus.mockImplementation(mock);
  }
});

it("创建分支后暂存版本变化会停止自动提交并保留原核验条件", async () => {
  const mock = getScopedGitStatus.getMockImplementation();
  const previous = await getScopedGitStatus(
    "chat-branch-checkpoint",
    "/projects/one",
  );
  const status = { ...previous, index: "confirmed-index" };
  getScopedGitStatus.mockResolvedValue(status);
  const before = runWorkspaceGitAction.mock.calls.length;
  runWorkspaceGitAction
    .mockImplementationOnce(async () => {
      getScopedGitStatus.mockResolvedValue({
        ...status,
        branch: "work/checkpoint",
        index: "changed-index",
      });
      return { branch: "work/checkpoint" };
    })
    .mockRejectedValueOnce(new Error("Index is stale"));
  const user = userEvent.setup();
  try {
    renderGitSummary("chat-branch-checkpoint");
    await user.click(screen.getByRole("button", { name: "查看摘要" }));
    await user.click(await screen.findByRole("button", { name: "提交" }));
    let dialog = await screen.findByRole("dialog", { name: "提交" });
    await user.click(
      within(dialog).getByRole("button", { name: "在新分支提交" }),
    );
    dialog = await screen.findByRole("dialog", { name: "在新分支提交" });
    await user.type(
      within(dialog).getByRole("textbox", { name: "新分支名称" }),
      "work/checkpoint",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "创建分支并提交" }),
    );
    expect(
      await screen.findByText("分支或提交已变化，请重新确认操作。"),
    ).toBeVisible();
    expect(runWorkspaceGitAction.mock.calls.slice(before)).toHaveLength(1);
    dialog = screen.getByRole("dialog", { name: "提交" });
    await user.click(within(dialog).getByRole("button", { name: "提交" }));
    expect(await screen.findByText("Index is stale")).toBeVisible();
    expect(
      runWorkspaceGitAction.mock.calls.slice(before).map(([input]) => input),
    ).toMatchObject([
      {
        action: "create_branch",
        branch: "work/checkpoint",
        expectedHead: "abcdef1",
      },
      {
        action: "commit",
        expectedBranch: "work/checkpoint",
        expectedHead: "abcdef1",
        expectedIndex: "confirmed-index",
      },
    ]);
  } finally {
    if (mock) getScopedGitStatus.mockImplementation(mock);
  }
});

it("已有本地提交且远端没有跟踪引用时可显式首次推送", async () => {
  const previous = await getScopedGitStatus("chat-first-push", "/projects/one");
  getScopedGitStatus.mockResolvedValueOnce({
    ...previous,
    branch: "main",
    remote: "origin",
    remotes: ["origin"],
    ahead: 0,
    behind: 0,
    aheadOfDefault: 0,
    entries: [],
  });
  const user = userEvent.setup();
  renderGitSummary("chat-first-push");

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(await screen.findByRole("button", { name: "Git 操作" }));
  const push = screen.getByRole("menuitem", { name: "推送" });
  expect(push).not.toHaveAttribute("aria-disabled", "true");
  await user.click(push);
  const dialog = await screen.findByRole("dialog", { name: "推送" });
  expect(within(dialog).getByText(/origin\/main/)).toBeVisible();
  runWorkspaceGitAction.mockResolvedValueOnce({ branch: "main", pushed: true });
  await user.click(within(dialog).getByRole("button", { name: "推送" }));

  expect(await screen.findByText("已推送")).toBeVisible();
  expect(runWorkspaceGitAction).toHaveBeenLastCalledWith(
    expect.objectContaining({
      action: "push",
      chatId: "chat-first-push",
      remote: "origin",
      expectedBranch: "main",
      expectedHead: "abcdef1",
      expectedRemoteBranch: "main",
    }),
  );
});

it.each([
  { upstream: "", label: "推送并创建 PR" },
  { upstream: "origin/feature", label: "创建或打开 PR" },
])("工作区干净时按分支发布状态显示 $label", async ({ upstream, label }) => {
  const previous = await getScopedGitStatus("chat-publish", "/projects/one");
  getScopedGitStatus.mockResolvedValueOnce({
    ...previous,
    upstream,
    remote: "origin",
    github: true,
    defaultBranch: "main",
    ahead: 0,
    aheadOfDefault: 1,
    entries: [],
  });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatSummary
        pane={document.body}
        chatId="chat-publish"
        chatFiles={[]}
        gitRoots={["/projects/one"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  const action = await screen.findByRole("button", { name: label });
  expect(action).toBeEnabled();
  await user.click(action);
  expect(await screen.findByRole("dialog", { name: label })).toBeVisible();
});

it("提交后推送失败只重试远端步骤，并沿用确认的目标", async () => {
  const mock = getScopedGitStatus.getMockImplementation();
  const status = {
    ...(await getScopedGitStatus("chat-push", "/projects/one")),
    remote: "origin",
    remotes: ["origin", "publish"],
    github: true,
    defaultBranch: "main",
  };
  getScopedGitStatus.mockResolvedValue(status);
  runWorkspaceGitAction.mockImplementationOnce(async () => {
    getScopedGitStatus.mockResolvedValue({
      ...status,
      head: "new-commit",
      entries: [],
    });
    return { commit: "new-commit", failedStep: "push", base: "main" };
  });
  const user = userEvent.setup();
  try {
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <ChatSummary
          pane={document.body}
          chatId="chat-push"
          chatFiles={[]}
          gitRoots={["/projects/one"]}
          workspaceDirectory="/workspace"
          onViewAllFiles={vi.fn()}
          onOpenFile={vi.fn()}
          onOpenChanges={vi.fn()}
        />
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "查看摘要" }));
    await user.click(
      await screen.findByRole("button", { name: "提交、推送并创建 PR" }),
    );
    const commit = await screen.findByRole("dialog", {
      name: "提交、推送并创建 PR",
    });
    await user.type(
      within(commit).getByRole("textbox", { name: "提交说明（可选）" }),
      "Keep manual message",
    );
    expect(
      within(commit).getByRole("textbox", { name: "目标分支" }),
    ).toHaveValue("");
    await user.click(within(commit).getByRole("combobox", { name: "远端" }));
    await user.click(await screen.findByRole("option", { name: "publish" }));
    expect(within(commit).getByText(/publish\/feature/)).toBeVisible();
    await user.click(
      within(commit).getByRole("button", { name: "提交、推送并创建 PR" }),
    );
    expect(await screen.findByText("已提交 new-comm")).toBeVisible();
    const retry = await screen.findByRole("button", { name: "推送并创建 PR" });
    await user.click(retry);
    const push = await screen.findByRole("dialog", { name: "推送并创建 PR" });
    expect(
      within(push).getByRole("combobox", { name: "远端" }),
    ).toHaveTextContent("publish");
    await user.click(
      within(push).getByRole("button", { name: "推送并创建 PR" }),
    );
    expect(runWorkspaceGitAction).toHaveBeenLastCalledWith(
      expect.objectContaining({
        action: "push_pr",
        remote: "publish",
        base: "main",
        expectedHead: "new-commit",
        expectedRemoteBranch: "feature",
        message: undefined,
      }),
    );
  } finally {
    if (mock) getScopedGitStatus.mockImplementation(mock);
  }
});

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
        pane={document.body}
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
  expect(screen.getByRole("dialog")).toHaveAttribute(
    "data-motion",
    "side-popup",
  );
  const projects = screen.getByRole("region", { name: "项目目录修改" });
  expect(
    await within(projects).findByRole("button", { name: "one 的改动" }),
  ).toBeVisible();
  expect(
    within(projects).getByRole("button", { name: "two 的改动" }),
  ).toBeVisible();
  expect(await within(projects).findByText("+12")).toBeVisible();
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
  expect(onOpenChanges).toHaveBeenCalledWith("/projects/two", "/projects/two");

  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  await user.click(screen.getByRole("button", { name: "成员一" }));
  expect(onMemberOpened).toHaveBeenCalledOnce();
});

it("一个项目读取失败不会遮蔽其他项目的 Git 状态和会话文件", async () => {
  getScopedGitStatus.mockRejectedValueOnce(new Error("repository unavailable"));
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ChatSummary
        pane={document.body}
        chatId="chat-2"
        chatFiles={[]}
        gitRoots={["/projects/one", "/projects/two"]}
        workspaceDirectory="/workspace"
        onViewAllFiles={vi.fn()}
        onOpenFile={vi.fn()}
        onOpenChanges={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  expect(await screen.findByText("+4")).toBeVisible();
  expect(
    await screen.findByRole("button", { name: "second.txt" }),
  ).toBeVisible();
  expect(screen.getAllByText("Git 文件改动暂时无法读取")).toHaveLength(1);
});
