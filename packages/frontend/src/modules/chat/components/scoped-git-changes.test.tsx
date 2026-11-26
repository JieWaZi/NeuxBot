import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
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
  const state = { staged: false };
  return {
    gitState: state,
    discardScopedGitFile: vi.fn(async () => true),
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
    getScopedGitComparison: vi.fn(
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
  render(
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
  return { user: userEvent.setup(), onOpenFile };
}

describe("ScopedGitChanges", () => {
  beforeEach(() => {
    gitState.staged = false;
    vi.clearAllMocks();
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
    );

    await user.click(screen.getByRole("button", { name: "分栏" }));
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
      ),
    );
    await user.click(screen.getByRole("button", { name: "隐藏测试文件" }));
    expect(within(main).queryByRole("button", { name: testPath })).toBeNull();
  });

  it("改动树定位并展开对应行，工具栏可以折叠全部补丁", async () => {
    const { user } = renderChanges();
    const tree = await screen.findByRole("complementary", { name: "改动文件" });
    const main = screen.getByRole("main", { name: "Git Diff 预览" });
    const stop = await within(tree).findByRole("button", { name: "main.ts" });
    await user.click(stop);
    expect(stop).toHaveAttribute("aria-current", "true");
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    expect(
      within(main).getByRole("button", { name: "折叠 " + changedPath }),
    ).toHaveFocus();
    await user.click(screen.getByRole("button", { name: "折叠全部文件" }));
    expect(screen.queryByTestId("git-patch-preview")).toBeNull();
  });

  it("草稿显式目录可读取比较，选基准只更新该仓库的审查范围", async () => {
    const { user } = renderChanges({ scopeChatId: undefined });
    const base = await screen.findByRole("combobox", { name: "比较基准" });
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
      ),
    );
    expect(stageScopedGitFile).not.toHaveBeenCalled();
    expect(unstageScopedGitFile).not.toHaveBeenCalled();
  });

  it("未提交仅读取 HEAD 到工作树净差异，不将相互抵消的两份补丁显示出来", async () => {
    gitState.staged = true;
    const { user } = renderChanges();
    await user.click(
      await screen.findByRole("combobox", { name: "Diff 来源" }),
    );
    await user.click(screen.getByRole("option", { name: "未提交" }));
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
    await user.click(
      await screen.findByRole("combobox", { name: "Diff 来源" }),
    );
    await user.click(screen.getByRole("option", { name: "暂存与工作区" }));

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
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    await waitFor(() =>
      expect(discardScopedGitFile).toHaveBeenCalledWith(
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
    expect(screen.queryByRole("textbox", { name: "提交说明" })).toBeNull();
  });

  it("提交选择的文件差异单独加载", async () => {
    const { user } = renderChanges();
    await user.click(
      await screen.findByRole("combobox", { name: "Diff 来源" }),
    );
    await user.click(screen.getByRole("option", { name: "提交历史" }));
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
    await user.click(await screen.findByRole("combobox", { name: "提交历史" }));
    await user.click(await screen.findByRole("option", { name: /initial/ }));

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
    );
  });
});
