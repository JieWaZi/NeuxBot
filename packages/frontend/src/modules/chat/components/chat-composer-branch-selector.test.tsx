import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";

const {
  checkoutWorkspaceGitBranch,
  hasPlatformService,
  listWorkspaceGitBranches,
  previewWorkspaceGitCheckout,
} = vi.hoisted(() => ({
  checkoutWorkspaceGitBranch: vi.fn(),
  hasPlatformService: vi.fn(() => true),
  listWorkspaceGitBranches: vi.fn(async () => ({
    isGitRepo: true,
    gitRoot: "/tmp/acp-go",
    branches: [
      { name: "main", isCurrent: true, isRemote: false },
      { name: "feature/login", isCurrent: false, isRemote: false },
      { name: "origin/main", isCurrent: false, isRemote: true },
    ],
  })),
  previewWorkspaceGitCheckout: vi.fn<
    (
      root: string,
      branch: string,
    ) => Promise<{
      canCheckout: boolean;
      conflicts: Array<{
        path: string;
        addedLines: number;
        deletedLines: number;
      }>;
    }>
  >(async () => ({
    canCheckout: true,
    conflicts: [],
  })),
}));

vi.mock("@/shared/platform", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    checkoutWorkspaceGitBranch,
    hasPlatformService,
    listWorkspaceGitBranches,
    previewWorkspaceGitCheckout,
  };
});

import { ChatComposerBranchSelector } from "./chat-composer-branch-selector";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

beforeEach(() => {
  vi.clearAllMocks();
  hasPlatformService.mockReturnValue(true);
  previewWorkspaceGitCheckout.mockResolvedValue({
    canCheckout: true,
    conflicts: [],
  });
});

function renderSelector() {
  return render(
    <QueryClientProvider client={queryClient}>
      <ChatComposerBranchSelector
        currentBranch="main"
        gitRoot="/tmp/acp-go"
        className="test-branch-control"
      />
    </QueryClientProvider>,
  );
}

it("打开分支列表并切换分支", async () => {
  const user = userEvent.setup();
  renderSelector();

  await user.click(screen.getByTestId("composer-branch-control"));
  await waitFor(() =>
    expect(listWorkspaceGitBranches).toHaveBeenCalledWith("/tmp/acp-go"),
  );
  expect(
    screen.getByRole("option", { name: "feature/login" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("option", { name: "main" })).toHaveClass(
    "bg-selection-surface",
  );
  expect(screen.getByRole("dialog")).toHaveClass("w-100");

  await user.click(screen.getByRole("option", { name: "feature/login" }));
  await waitFor(() =>
    expect(previewWorkspaceGitCheckout).toHaveBeenCalledWith(
      "/tmp/acp-go",
      "feature/login",
    ),
  );
  await waitFor(() =>
    expect(checkoutWorkspaceGitBranch).toHaveBeenCalledWith(
      "/tmp/acp-go",
      "feature/login",
      undefined,
    ),
  );
});

it("存在冲突时提示先提交再切换", async () => {
  previewWorkspaceGitCheckout.mockResolvedValueOnce({
    canCheckout: false,
    conflicts: [{ path: ".gitignore", addedLines: 1, deletedLines: 0 }],
  });
  const user = userEvent.setup();
  renderSelector();

  await user.click(screen.getByTestId("composer-branch-control"));
  await waitFor(() =>
    expect(
      screen.getByRole("option", { name: "feature/login" }),
    ).toBeInTheDocument(),
  );
  await user.click(screen.getByRole("option", { name: "feature/login" }));

  expect(await screen.findByText("提交更改后切换分支")).toBeInTheDocument();
  expect(screen.getByText(".gitignore")).toBeInTheDocument();
  expect(checkoutWorkspaceGitBranch).not.toHaveBeenCalled();
});
