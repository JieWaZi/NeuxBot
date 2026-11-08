import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { SkillDeleteDialog } from "./skill-delete-dialog";

const { uninstallSkill } = vi.hoisted(() => ({
  uninstallSkill:
    vi.fn<
      (options: {
        path: { skillId: string; wakerId: string };
      }) => Promise<{ success: boolean }>
    >(),
}));

vi.mock("../api/skill-queries", () => ({
  invalidateVisibleSkillQueries: async () => undefined,
  listWakerSkillsQueryKey: () => ["waker-skills", "waker-1"],
  uninstallSkillMutation: () => ({ mutationFn: uninstallSkill }),
}));

beforeEach(() => {
  uninstallSkill.mockReset();
  let retryAttempts = 0;
  uninstallSkill.mockImplementation(async ({ path }) => {
    if (path.skillId === "skill-retry" && retryAttempts++ === 0) {
      throw new Error("temporary failure");
    }
    return { success: true };
  });
});

describe("SkillDeleteDialog", () => {
  it("keeps only failed skills for a batch retry", async () => {
    const user = userEvent.setup();
    const onDeleted = vi.fn();
    const onOpenChange = vi.fn();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <SkillDeleteDialog
          open
          skills={[
            {
              affectedWakerCount: 3,
              id: "skill-first",
              name: "代码审查",
            },
            {
              affectedWakerCount: 1,
              id: "skill-retry",
              name: "技术写作",
            },
          ]}
          wakerId="waker-1"
          onDeleted={onDeleted}
          onOpenChange={onOpenChange}
        />
      </QueryClientProvider>,
    );

    expect(
      screen.getByText("将从当前伙伴移除所选的 2 个技能，其他伙伴不受影响。"),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "移除所选" }));

    expect(
      await screen.findByText("1 个技能移除失败，已保留失败项，请重试。"),
    ).toBeInTheDocument();
    expect(onDeleted).toHaveBeenCalledWith(["skill-first"]);
    expect(uninstallSkill).toHaveBeenCalledTimes(2);

    await user.click(screen.getByRole("button", { name: "重试移除" }));

    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(3));
    expect(uninstallSkill.mock.calls[2]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "skill-retry" },
      }),
    );
    expect(onDeleted).toHaveBeenLastCalledWith(["skill-retry"]);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
