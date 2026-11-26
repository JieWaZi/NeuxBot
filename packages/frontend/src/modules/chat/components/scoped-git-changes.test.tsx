import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import type {
  discardScopedGitFile as DiscardScopedGitFile,
  WorkspaceGitComparison,
} from "@/shared/platform";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  ScopedGitChanges,
  type ScopedGitChangesProps,
} from "./scoped-git-changes";

const chatId = "a".repeat(32);
const root = "/tmp/bot";
const testPath = "src/main.test.ts";
const changedPath = "src/main.ts";
const hash = "1234567890abcdef1234567890abcdef12345678";
const {
  discardScopedGitFile,
  getScopedGitStatus,
  getScopedGitBranchesForScope,
  getScopedGitComparison,
  stageScopedGitFile,
  unstageScopedGitFile,
  getScopedGitLog,
  gitState,
} = vi.hoisted(() => {
  const state = { staged: false, worktreeVersion: "observed-worktree-version" };
  return {
    gitState: state,
    discardScopedGitFile: vi.fn<typeof DiscardScopedGitFile>(async () => true),
    getScopedGitStatus: vi.fn(async () => ({
      isRepo: true,
      branch: "main",
      head: "observed-head",
      index: "observed-index",
      root: "/tmp/bot",
      repositories: ["/tmp/bot"],
      entries: [],
    })),
    getScopedGitBranchesForScope: vi.fn(async () => ({
      isGitRepo: true,
      gitRoot: "/tmp/bot",
      branches: [
        { name: "main", isCurrent: true, isRemote: false },
        { name: "release", isCurrent: false, isRemote: false },
      ],
    })),
    getScopedGitComparison: vi.fn<
      (
        chatId: string,
        root: string,
        repository: string,
        base?: string,
        path?: string,
        ignoreWhitespace?: boolean,
        previousPath?: string,
        signal?: AbortSignal,
      ) => Promise<WorkspaceGitComparison>
    >(
      async (
        _chat: string,
        selectedRoot: string,
        _repo: string,
        base?: string,
        path?: string,
      ) => ({
        root: selectedRoot,
        branch: "main",
        baseRef: base ?? "origin/main",
        worktreeVersion:
          base === ":unstaged" && path ? state.worktreeVersion : undefined,
        entries:
          base === "HEAD" || (base === ":staged" && !state.staged)
            ? []
            : [
                {
                  path: "src/main.ts",
                  status: "M",
                  addedLines: 2,
                  deletedLines: 1,
                  lineCountUnavailable: false,
                },
                ...(base
                  ? []
                  : [
                      {
                        path: "src/main.test.ts",
                        status: "A",
                        addedLines: 1,
                        deletedLines: 0,
                        lineCountUnavailable: false,
                      },
                    ]),
              ].filter((entry) => !path || entry.path === path),
        content: path
          ? "diff --git a/" +
            path +
            " b/" +
            path +
            "\n--- a/" +
            path +
            "\n+++ b/" +
            path +
            "\n@@ -1 +1 @@\n-old line\n+new line\n"
          : undefined,
      }),
    ),
    stageScopedGitFile: vi.fn(async () => {
      state.staged = true;
    }),
    unstageScopedGitFile: vi.fn(async () => {
      state.staged = false;
    }),
    getScopedGitLog: vi.fn(async () => [
      {
        hash: "1234567",
        hashFull: "1234567890abcdef1234567890abcdef12345678",
        subject: "initial",
        author: "Test",
        date: "2026-09-28",
        refs: "HEAD -> main",
      },
    ]),
  };
});

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService: () => false,
  getScopedGitStatus,
  listWorkspaceGitBranches: getScopedGitBranchesForScope,
  getScopedGitComparison,
  getScopedGitLog,
  stageScopedGitFile,
  unstageScopedGitFile,
  discardScopedGitFile,
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));
vi.mock("@git-diff-view/react", () => ({
  DiffModeEnum: { Unified: "unified", Split: "split" },
  DiffView: ({
    diffViewMode,
    diffViewWrap,
  }: {
    diffViewMode: string;
    diffViewWrap: boolean;
  }) => (
    <div
      data-testid="git-patch-preview"
      data-mode={diffViewMode}
      data-wrap={diffViewWrap}
    />
  ),
}));

function renderChanges(props: Partial<ScopedGitChangesProps> = {}) {
  const onOpenFile = vi.fn();
  const view = render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TooltipProvider>
        <ScopedGitChanges
          query=""
          rootPath={root}
          scopeChatId={chatId}
          onOpenFile={onOpenFile}
          {...props}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, user: userEvent.setup(), onOpenFile };
}

describe("ScopedGitChanges", () => {
  beforeEach(() => {
    gitState.staged = false;
    gitState.worktreeVersion = "observed-worktree-version";
    vi.clearAllMocks();
    localStorage.clear();
  });

  it("先读取比较元数据，展开才请求单文件补丁，文件名打开普通文件", async () => {
    const { user, onOpenFile } = renderChanges();

    const main = await screen.findByRole("main", { name: "Git Diff 预览" });
    const file = await within(main).findByRole("button", { name: changedPath });
    expect(getScopedGitStatus).toHaveBeenCalledTimes(1);
    expect(getScopedGitComparison.mock.calls.map((args) => args[4])).toEqual([
      undefined,
    ]);
    expect(screen.queryByTestId("git-patch-preview")).toBeNull();

    await user.click(file);
    expect(onOpenFile).toHaveBeenCalledWith(root, changedPath);
    await user.click(
      within(main).getByRole("button", { name: "展开 " + changedPath }),
    );
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    expect(getScopedGitComparison).toHaveBeenLastCalledWith(
      chatId,
      root,
      root,
      undefined,
      changedPath,
      false,
      "",
      expect.any(AbortSignal),
    );

    const unified = screen.getByRole("radio", { name: "统一" });
    const split = screen.getByRole("radio", { name: "分栏" });
    expect(unified).toBeChecked();
    await user.click(split);
    expect(split).toBeChecked();
    await user.click(screen.getByRole("button", { name: "自动换行" }));
    expect(screen.getByTestId("git-patch-preview")).toHaveAttribute(
      "data-mode",
      "split",
    );
    expect(screen.getByTestId("git-patch-preview")).toHaveAttribute(
      "data-wrap",
      "true",
    );

    await user.click(screen.getByRole("button", { name: "隐藏空白改动" }));
    await waitFor(() =>
      expect(getScopedGitComparison).toHaveBeenCalledWith(
        chatId,
        root,
        root,
        undefined,
        undefined,
        true,
        undefined,
        expect.any(AbortSignal),
      ),
    );
    await user.click(screen.getByRole("button", { name: "隐藏测试文件" }));
    expect(within(main).queryByRole("button", { name: testPath })).toBeNull();
  });

  it("改动树定位并展开对应行，工具栏可以折叠全部补丁", async () => {
    const { user } = renderChanges();
    await user.click(
      await screen.findByRole("button", { name: "显示改动列表" }),
    );
    const tree = await screen.findByRole("complementary", { name: "改动文件" });
    const main = screen.getByRole("main", { name: "Git Diff 预览" });
    const surface = main.closest(".bg-surface-panel");
    const toolbar = screen
      .getByRole("button", { name: "Diff 来源" })
      .closest("header");
    expect(surface).toContainElement(tree);
    expect(surface).toContainElement(toolbar);
    expect(toolbar).toHaveClass("h-row-navigation", "bg-surface-panel");
    const stop = await within(tree).findByRole("treeitem", { name: "main.ts" });
    await user.click(stop);
    expect(stop).toHaveAttribute("aria-current", "true");
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    expect(
      within(main).getByRole("button", { name: "折叠 " + changedPath }),
    ).toHaveFocus();
    await user.click(screen.getByRole("button", { name: "折叠全部文件" }));
    expect(screen.queryByTestId("git-patch-preview")).toBeNull();
  });

  it("关闭 Diff 时取消仍在读取的文件补丁", async () => {
    const { user, unmount } = renderChanges();
    const toggle = await screen.findByRole("button", {
      name: "展开 " + changedPath,
    });
    getScopedGitComparison.mockImplementationOnce(() => new Promise(() => {}));
    await user.click(toggle);
    await waitFor(() =>
      expect(getScopedGitComparison.mock.lastCall?.[4]).toBe(changedPath),
    );
    const signal = getScopedGitComparison.mock.lastCall?.[7];
    expect(signal?.aborted).toBe(false);
    unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("首次打开 Diff 收起改动树，重新打开时恢复上次选择", async () => {
    const { user, unmount } = renderChanges();
    const open = await screen.findByRole("button", { name: "显示改动列表" });
    expect(open).toHaveAttribute("aria-pressed", "false");
    expect(
      screen.queryByRole("complementary", { name: "改动文件" }),
    ).not.toBeInTheDocument();
    await user.click(open);
    expect(
      await screen.findByRole("complementary", { name: "改动文件" }),
    ).toBeVisible();
    unmount();

    renderChanges();
    expect(
      await screen.findByRole("complementary", { name: "改动文件" }),
    ).toBeVisible();
  });

  it("比较栏直接展示当前分支和实际基准", async () => {
    renderChanges();
    expect(await screen.findByText("main", { exact: true })).toBeVisible();
    const base = screen.getByRole("button", { name: "比较基准" });
    expect(base).toHaveTextContent(/^origin\/main$/);
  });

  it("窄工具栏的显示选项可以改变补丁布局", async () => {
    const { user } = renderChanges();
    await user.click(
      await screen.findByRole("button", { name: "展开 " + changedPath }),
    );
    await user.click(screen.getByRole("button", { name: "Diff 显示选项" }));
    await user.click(screen.getByRole("menuitem", { name: "分栏" }));
    expect(await screen.findByTestId("git-patch-preview")).toHaveAttribute(
      "data-mode",
      "split",
    );
  });

  it("草稿显式目录可读取比较，选基准只更新该仓库的审查范围", async () => {
    const { user } = renderChanges({ scopeChatId: undefined });
    const base = await screen.findByRole("button", { name: "比较基准" });
    await user.click(base);
    await user.click(await screen.findByRole("option", { name: "release" }));
    expect(base).toHaveTextContent("release");
    await waitFor(() =>
      expect(getScopedGitComparison).toHaveBeenCalledWith(
        undefined,
        root,
        root,
        "release",
        undefined,
        false,
        undefined,
        expect.any(AbortSignal),
      ),
    );
    expect(stageScopedGitFile).not.toHaveBeenCalled();
    expect(unstageScopedGitFile).not.toHaveBeenCalled();
  });

  it("按搜索词按需读取有限的比较分支，键盘选择结果", async () => {
    getScopedGitBranchesForScope
      .mockResolvedValueOnce({
        isGitRepo: true,
        gitRoot: root,
        branches: [{ name: "release", isCurrent: false, isRemote: false }],
      })
      .mockResolvedValueOnce({
        isGitRepo: true,
        gitRoot: root,
        branches: [
          { name: "origin/older-release", isCurrent: false, isRemote: true },
        ],
      });
    const { user } = renderChanges();
    expect(getScopedGitBranchesForScope).not.toHaveBeenCalled();
    await user.click(await screen.findByRole("button", { name: "比较基准" }));
    await screen.findByRole("option", { name: "release" });
    expect(getScopedGitBranchesForScope).toHaveBeenLastCalledWith(root, {
      chatId,
      repository: root,
      limit: 100,
      signal: expect.any(AbortSignal),
    });

    await user.type(
      screen.getByRole("combobox", { name: "搜索分支" }),
      "older",
    );
    await screen.findByRole("option", { name: "origin/older-release" });
    expect(getScopedGitBranchesForScope).toHaveBeenLastCalledWith(root, {
      chatId,
      repository: root,
      limit: 100,
      signal: expect.any(AbortSignal),
      query: "older",
    });
    expect(getScopedGitBranchesForScope).toHaveBeenCalledTimes(2);
    await user.keyboard("{ArrowDown}{Enter}");
    expect(screen.getByRole("button", { name: "比较基准" })).toHaveTextContent(
      "origin/older-release",
    );
  });

  it("未提交仅读取 HEAD 到工作树净差异，不将相互抵消的两份补丁显示出来", async () => {
    gitState.staged = true;
    const { user } = renderChanges();
    await user.click(await screen.findByRole("button", { name: "Diff 来源" }));
    await user.click(screen.getByRole("menuitemradio", { name: "未提交" }));
    const view = screen.getByRole("main", { name: "Git Diff 预览" });
    expect(
      await within(view).findByText("没有可显示的差异"),
    ).toBeInTheDocument();
    expect(getScopedGitComparison).toHaveBeenCalledWith(
      chatId,
      root,
      root,
      "HEAD",
      undefined,
      false,
      undefined,
      expect.any(AbortSignal),
    );
    expect(
      getScopedGitComparison.mock.calls.some(
        (args) => args[3] === ":staged" || args[3] === ":unstaged",
      ),
    ).toBe(false);
    expect(
      within(view).queryByRole("button", { name: "取消暂存 " + changedPath }),
    ).toBeNull();
  });

  it("暂存与工作区保留暂存、取消暂存和确认丢弃", async () => {
    const { user } = renderChanges();
    await user.click(await screen.findByRole("button", { name: "Diff 来源" }));
    await user.click(
      screen.getByRole("menuitemradio", { name: "暂存与工作区" }),
    );

    await user.click(
      await screen.findByRole("button", { name: "暂存 " + changedPath }),
    );
    await user.click(
      await screen.findByRole("button", { name: "取消暂存 " + changedPath }),
    );
    await waitFor(() =>
      expect(unstageScopedGitFile).toHaveBeenCalledWith(
        chatId,
        root,
        root,
        changedPath,
        {
          expectedBranch: "main",
          expectedHead: "observed-head",
          expectedIndex: "observed-index",
        },
      ),
    );
    const discard = await screen.findByRole("button", {
      name: "丢弃 " + changedPath + " 的未暂存改动",
    });
    await user.click(discard);
    expect(discardScopedGitFile).not.toHaveBeenCalled();
    await user.click(await screen.findByRole("button", { name: "确认丢弃" }));
    await waitFor(() =>
      expect(discardScopedGitFile).toHaveBeenCalledWith(
        chatId,
        root,
        root,
        changedPath,
        {
          expectedWorktreeVersion: "observed-worktree-version",
          ignoreWhitespace: false,
        },
      ),
    );
    expect(screen.queryByRole("textbox", { name: "提交说明" })).toBeNull();
  });

  it("丢弃失败后再次确认收起的改动时重新读取文件版本", async () => {
    discardScopedGitFile.mockRejectedValueOnce(new Error("版本已变化"));
    const { user } = renderChanges();
    await user.click(await screen.findByRole("button", { name: "Diff 来源" }));
    await user.click(
      screen.getByRole("menuitemradio", { name: "暂存与工作区" }),
    );
    const discard = await screen.findByRole("button", {
      name: "丢弃 " + changedPath + " 的未暂存改动",
    });
    await user.click(discard);
    gitState.worktreeVersion = "later-worktree-version";
    await user.click(await screen.findByRole("button", { name: "确认丢弃" }));
    await screen.findByText("版本已变化");
    await waitFor(() => expect(discard).toBeEnabled());
    await user.click(discard);
    await user.click(await screen.findByRole("button", { name: "确认丢弃" }));
    expect(discardScopedGitFile.mock.calls.map((args) => args[4])).toEqual([
      {
        expectedWorktreeVersion: "observed-worktree-version",
        ignoreWhitespace: false,
      },
      {
        expectedWorktreeVersion: "later-worktree-version",
        ignoreWhitespace: false,
      },
    ]);
  });

  it("提交选择的文件差异单独加载", async () => {
    const { user } = renderChanges();
    await user.click(await screen.findByRole("button", { name: "Diff 来源" }));
    await user.click(screen.getByRole("menuitemradio", { name: "提交历史" }));
    await waitFor(() =>
      expect(getScopedGitLog).toHaveBeenCalledWith(
        chatId,
        root,
        root,
        100,
        0,
        ":auto",
      ),
    );
    await user.click(await screen.findByRole("button", { name: "提交历史" }));
    await user.click(
      await screen.findByRole("menuitemradio", { name: /initial/ }),
    );

    const main = screen.getByRole("main", { name: "Git Diff 预览" });
    await user.click(
      await within(main).findByRole("button", { name: "展开 " + changedPath }),
    );
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    expect(getScopedGitComparison).toHaveBeenCalledWith(
      chatId,
      root,
      root,
      ":commit:" + hash,
      changedPath,
      false,
      "",
      expect.any(AbortSignal),
    );
  });
});
