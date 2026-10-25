import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ScopedGitChanges } from "./scoped-git-changes";

const {
  gitState,
  discardScopedGitFile,
  getScopedGitStatus,
  getScopedGitDiff,
  stageScopedGitFile,
  commitScopedGitFiles,
  getScopedGitCommitDiff,
  getScopedGitLog,
} = vi.hoisted(() => ({
  discardScopedGitFile: vi.fn(async () => true),
  gitState: { xy: " M", untracked: false },
  getScopedGitStatus: vi.fn(async () => ({
    isRepo: true,
    branch: "main",
    root: "/tmp/ally",
    repositories: ["/tmp/ally"],
    entries: [
      ...(gitState.xy
        ? [
            {
              path: "src/main.ts",
              xy: gitState.xy,
              stagedAddedLines: 1,
              stagedDeletedLines: 1,
              unstagedAddedLines: 2,
              unstagedDeletedLines: 1,
            },
          ]
        : []),
      ...(gitState.untracked
        ? [
            {
              path: "new.txt",
              xy: "??",
              stagedAddedLines: 0,
              stagedDeletedLines: 0,
              unstagedAddedLines: 3,
              unstagedDeletedLines: 0,
            },
          ]
        : []),
    ],
  })),
  getScopedGitDiff: vi.fn(
    async () =>
      "diff --git a/src/main.ts b/src/main.ts\n--- a/src/main.ts\n+++ b/src/main.ts\n@@ -1 +1 @@\n-old line\n+new line\n",
  ),
  stageScopedGitFile: vi.fn(async () => {
    gitState.xy = "M ";
  }),
  commitScopedGitFiles: vi.fn(async () => {
    gitState.xy = "";
  }),
  getScopedGitCommitDiff: vi.fn(
    async () =>
      "diff --git a/src/main.ts b/src/main.ts\n--- a/src/main.ts\n+++ b/src/main.ts\n@@ -1 +1 @@\n-old line\n+new line\n",
  ),
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
}));

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService: () => false,
  getScopedGitStatus,
  getScopedGitDiff,
  getScopedGitLog,
  getScopedGitCommitDiff,
  stageScopedGitFile,
  commitScopedGitFiles,
  unstageScopedGitFile: vi.fn(async () => {
    gitState.xy = " M";
  }),
  discardScopedGitFile,
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));
vi.mock("@git-diff-view/react", () => ({
  DiffModeEnum: { Unified: "unified", Split: "split" },
  DiffView: ({ diffViewMode }: { diffViewMode: string }) => (
    <div data-testid="git-patch-preview" data-mode={diffViewMode} />
  ),
}));

describe("ScopedGitChanges", () => {
  beforeEach(() => {
    gitState.xy = " M";
    gitState.untracked = false;
    vi.clearAllMocks();
  });
  it("文件行只显示增删行数，未跟踪文件按新增行展示", async () => {
    gitState.untracked = true;
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );

    const changes = await screen.findByRole("complementary", {
      name: "改动文件",
    });
    expect(changes).toHaveTextContent("src/main.ts+2-1");
    expect(changes).toHaveTextContent("new.txt+3-0");
    expect(changes).not.toHaveTextContent("修改");
    expect(changes).not.toHaveTextContent("未跟踪");
    await user.click(screen.getByText("new.txt"));
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    expect(getScopedGitDiff).toHaveBeenCalledWith(
      "a".repeat(32),
      "/tmp/ally",
      "/tmp/ally",
      "new.txt",
      false,
    );
  });
  it("Diff 复用顶部工具栏并占满预览区，不提供单文件全屏入口", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );

    await user.click(await screen.findByText("src/main.ts"));
    expect(
      await screen.findByTestId("git-patch-preview", {}, { timeout: 5000 }),
    ).toBeInTheDocument();
    const toolbar = screen
      .getByRole("button", { name: "历史" })
      .closest("header");
    if (!toolbar) throw new Error("缺少 Git 工具栏");
    await user.click(within(toolbar).getByRole("button", { name: "分栏" }));
    expect(
      within(toolbar).getByRole("button", { name: "统一" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "在标签中打开 Diff" }),
    ).toBeNull();
    expect(screen.getByTestId("git-patch-preview")).toHaveAttribute(
      "data-mode",
      "split",
    );
    const main = screen.getByRole("main", { name: "Git Diff 预览" });
    expect(within(main).getAllByText("src/main.ts")).toHaveLength(1);
    expect(main.querySelector(".max-h-96")).toBeNull();
    expect(getScopedGitStatus).toHaveBeenCalledWith(
      "a".repeat(32),
      "/tmp/ally",
      "/tmp/ally",
    );
    expect(getScopedGitDiff).toHaveBeenCalledWith(
      "a".repeat(32),
      "/tmp/ally",
      "/tmp/ally",
      "src/main.ts",
      false,
    );
  });

  it("暂存后才允许提交，成功后清空提交说明", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );

    const commit = await screen.findByRole("button", {
      name: "提交已暂存文件",
    });
    expect(commit).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "暂存 src/main.ts" }));
    expect(
      await screen.findByRole("button", { name: "取消暂存 src/main.ts" }),
    ).toBeInTheDocument();
    await user.type(
      screen.getByRole("textbox", { name: "提交说明" }),
      "change",
    );
    await user.click(commit);
    expect(commitScopedGitFiles).toHaveBeenCalledWith(
      "a".repeat(32),
      "/tmp/ally",
      "/tmp/ally",
      "change",
    );
    expect(
      await screen.findByText("当前工作树没有文件改动"),
    ).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "提交说明" })).toHaveValue("");
  });

  it("点击历史提交可读取提交 Diff", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );
    await screen.findByRole("button", { name: "历史" });
    expect(getScopedGitLog).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "历史" }));
    await user.click(await screen.findByRole("button", { name: /initial/ }));
    expect(await screen.findByTestId("git-patch-preview")).toBeInTheDocument();
    const main = screen.getByRole("main", { name: "Git 历史预览" });
    expect(main.querySelector(".max-h-96")).toBeNull();
    expect(getScopedGitCommitDiff).toHaveBeenCalledWith(
      "a".repeat(32),
      "/tmp/ally",
      "/tmp/ally",
      "1234567890abcdef1234567890abcdef12345678",
    );
  });

  it("单仓库只显示一条工具栏，改动列表与预览共享可调整的宽度", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );

    await screen.findByText("src/main.ts");
    expect(screen.queryByRole("combobox", { name: "Git 仓库" })).toBeNull();
    expect(screen.queryByText("/tmp/ally")).toBeNull();
    const list = screen.getByRole("complementary", { name: "改动文件" });
    expect(list).not.toHaveClass("absolute");
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "隐藏改动列表" }));
    expect(
      screen.queryByRole("complementary", { name: "改动文件" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "显示改动列表" }));
    expect(
      screen.getByRole("complementary", { name: "改动文件" }),
    ).toBeVisible();
  });

  it("提交失败时保留输入与已暂存文件", async () => {
    const user = userEvent.setup();
    gitState.xy = "M ";
    commitScopedGitFiles.mockRejectedValueOnce(new Error("hook failed"));
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );
    await user.type(
      await screen.findByRole("textbox", { name: "提交说明" }),
      "keep me",
    );
    await user.click(screen.getByRole("button", { name: "提交已暂存文件" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("hook failed");
    expect(screen.getByRole("textbox", { name: "提交说明" })).toHaveValue(
      "keep me",
    );
    expect(
      screen.getByRole("button", { name: "取消暂存 src/main.ts" }),
    ).toBeInTheDocument();
  });
  it("Git 丢弃先确认，取消不执行", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <ScopedGitChanges
          query=""
          rootPath="/tmp/ally"
          scopeChatId={"a".repeat(32)}
        />
      </QueryClientProvider>,
    );
    const discard = await screen.findByRole("button", {
      name: "丢弃 src/main.ts 的未暂存改动",
    });
    await user.click(discard);
    expect(discardScopedGitFile).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toHaveTextContent("修改将丢失");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(discardScopedGitFile).not.toHaveBeenCalled();
    await user.click(discard);
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    await waitFor(() =>
      expect(discardScopedGitFile).toHaveBeenCalledWith(
        "a".repeat(32),
        "/tmp/ally",
        "/tmp/ally",
        "src/main.ts",
      ),
    );
  });
});
