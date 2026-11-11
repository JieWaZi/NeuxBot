import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import type { GetRuntimeInstallationResponse } from "@/shared/api/generated/types.gen";
import { RuntimeProgramRemoval } from "./runtime-program-removal";
import "@/shared/i18n";

const { remove } = vi.hoisted(() => ({ remove: vi.fn() }));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  uninstallRuntimeMutation: () => ({ mutationFn: remove }),
}));
vi.mock("./runtime-installation-controls", () => ({
  runtimeInstallationOptions: (runtimeId: string) => ({
    queryKey: ["installation", runtimeId],
  }),
}));
beforeEach(() => {
  remove.mockReset().mockResolvedValue({});
});
function show(
  canUninstall = true,
  snapshot: Partial<GetRuntimeInstallationResponse> = {},
) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <RuntimeProgramRemoval
        runtimeId="codex:work"
        name="Codex"
        installation={{
          installed: true,
          canUninstall,
          installationPath: "/example/cli/codex",
          ...snapshot,
        }}
      />
    </QueryClientProvider>,
  );
}

it("确认后仅按智能体身份卸载程序，不提交账号或目录删除参数", async () => {
  show();
  await userEvent.click(screen.getByRole("button", { name: "删除" }));
  expect(screen.getByRole("dialog")).toHaveTextContent(
    "账号、登录和伙伴配置会保留",
  );
  expect(screen.getByRole("dialog")).toHaveTextContent("/example/cli/codex");
  expect(remove).not.toHaveBeenCalled();
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "删除" }),
  );
  await waitFor(() =>
    expect(remove).toHaveBeenCalledWith(
      expect.objectContaining({ path: { runtimeId: "codex:work" } }),
      expect.anything(),
    ),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("无法确认归属时禁用删除并说明原安装方式，不能假装卸载成功", () => {
  show(false);
  expect(screen.getByRole("button", { name: "删除" })).toBeDisabled();
  expect(
    screen.getByText("此安装暂不支持自动删除，请通过原安装方式卸载。"),
  ).toBeVisible();
  expect(remove).not.toHaveBeenCalled();
});
it("活动工作导致拒绝时保留确认框与真实错误", async () => {
  remove.mockRejectedValue(
    new ApiError("此智能体仍有工作在运行。", "RuntimeBusy"),
  );
  show();
  await userEvent.click(screen.getByRole("button", { name: "删除" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "删除" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "此智能体仍有工作在运行。",
  );
  expect(screen.getByRole("dialog")).toBeVisible();
});

it("部分删除后主程序已不存在时仍能继续删除剩余受管文件", async () => {
  show(true, { installed: false, maintenanceReason: "cleanup_pending" });
  expect(screen.getByText("上次删除尚未完成，请继续删除。")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "删除" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "删除" }),
  );
  await waitFor(() => expect(remove).toHaveBeenCalledTimes(1));
});
