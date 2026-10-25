import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { SkillVersionDiffDialog } from "./skill-version-diff-dialog";

const viewer = vi.hoisted(() => ({
  load: vi.fn(),
  resolve:
    vi.fn<(module: { SkillVersionDiffView: () => React.ReactNode }) => void>(),
}));

vi.mock("./skill-version-diff-view", () => {
  viewer.load();
  return new Promise((resolve) => {
    viewer.resolve.mockImplementation(resolve);
  });
});

it("关闭时不加载差异库，打开后保留弹窗操作并只在差异区显示 Skeleton", async () => {
  const user = userEvent.setup();
  const onOpenChange = vi.fn();
  const onApply = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const props = {
    currentContent: "当前指令",
    currentResources: [{ path: "SKILL.md", digest: "current" }],
    currentVersion: "1.1.0",
    currentVersionId: "current",
    selectedVersion: {
      id: "previous",
      instructions: "旧指令",
      resources: [{ path: "SKILL.md", digest: "previous" }],
      version: "1.0.0",
    },
    skillId: "skill-1",
    wakerId: "waker-1",
    theme: "light" as const,
    t: (key: string) => key,
    onOpenChange,
    onApply,
  };
  const dialog = (open: boolean) => (
    <QueryClientProvider client={client}>
      <SkillVersionDiffDialog {...props} open={open} />
    </QueryClientProvider>
  );
  const { rerender } = render(dialog(false));
  expect(viewer.load).not.toHaveBeenCalled();

  rerender(dialog(true));
  await waitFor(() => expect(viewer.load).toHaveBeenCalledTimes(1));
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(
    screen.getByRole("heading", { name: "detail.diffTitle" }),
  ).toBeVisible();
  expect(screen.getByText("SKILL.md")).toBeVisible();
  expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
  expect(
    screen.getByRole("status").querySelector(".animate-pulse"),
  ).not.toBeNull();
  expect(
    screen.getByRole("status").querySelector(".agent-grid-spinner"),
  ).toBeNull();
  await user.click(screen.getByRole("button", { name: "common:cancel" }));
  expect(onOpenChange).toHaveBeenCalledWith(false);

  await act(async () => {
    viewer.resolve({ SkillVersionDiffView: () => <div>版本差异已就绪</div> });
  });
  expect(await screen.findByText("版本差异已就绪")).toBeVisible();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  await user.click(
    screen.getByRole("button", { name: "detail.restoreVersion" }),
  );
  expect(onApply).toHaveBeenCalledOnce();
  client.clear();
});
