import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/shared/api/generated/sdk.gen";
import { platform } from "./provider";
import { createCoreWorkspaceServices } from "./core-workspace";
const recovery = vi.hoisted(() => ({ handler: (): void => undefined }));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: (handler: () => void) => {
    recovery.handler = handler;
    return () => undefined;
  },
  subscribeGlobalEvents: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  openScope: vi.fn(),
  readFile: vi.fn(),
  statFile: vi.fn(),
  deleteFile: vi.fn(),
  gitStatus: vi.fn(),
  resolveReference: vi.fn(),
}));
beforeEach(() => vi.resetAllMocks());
const scope = { id: "scope", root: "/workspace" };
const resource = {
  relativePath: "test.txt",
  size: 3,
  version: "v1",
  modifiedAt: 1,
  mediaType: "text/plain",
};
describe("Core 文件和 Git 适配", () => {
  it("共享进行中的范围请求，Core 恢复后重新授权", async () => {
    vi.mocked(api.openScope).mockResolvedValue({ data: scope } as never);
    const { workspace } = createCoreWorkspaceServices(platform().host);
    await Promise.all([
      workspace.open_workspace_scope({ chatId: "chat" }),
      workspace.open_workspace_scope({ chatId: "chat" }),
    ]);
    expect(api.openScope).toHaveBeenCalledTimes(1);
    recovery.handler();
    await workspace.open_workspace_scope({ chatId: "chat" });
    expect(api.openScope).toHaveBeenCalledTimes(2);
  });
  it("解码原字节并传播缺失字段错误，不伪装成空文件", async () => {
    vi.mocked(api.readFile).mockResolvedValue({
      data: { resource, bytesBase64: "AAH/" },
    } as never);
    const { workspace } = createCoreWorkspaceServices(platform().host);
    const args = {
      scopeId: "scope",
      relativePath: "test.txt",
      requestId: "read",
    };
    expect((await workspace.read_workspace_resource(args)).bytes).toEqual(
      new Uint8Array([0, 1, 255]),
    );
    vi.mocked(api.readFile).mockResolvedValue({
      data: { bytesBase64: "" },
    } as never);
    await expect(workspace.read_workspace_resource(args)).rejects.toThrow();
  });
  it("取消读取传递到请求的 AbortSignal", async () => {
    let signal: AbortSignal | null | undefined;
    vi.mocked(api.readFile).mockImplementation((args) => {
      signal = args?.signal;
      return new Promise<never>((_, reject) =>
        signal?.addEventListener("abort", () =>
          reject(new DOMException("cancelled", "AbortError")),
        ),
      );
    });
    const { workspace } = createCoreWorkspaceServices(platform().host);
    const reading = workspace.read_workspace_resource({
      scopeId: "scope",
      relativePath: "test.txt",
      requestId: "read",
    });
    const rejected = expect(reading).rejects.toThrow("cancelled");
    await workspace.cancel_workspace_resource_read({
      scopeId: "scope",
      requestId: "read",
    });
    await rejected;
    expect(signal?.aborted).toBe(true);
  });
  it("删除传递用户确认时的版本，冲突不重试也不重新取版本", async () => {
    vi.mocked(api.deleteFile).mockRejectedValue(new Error("version conflict"));
    const { workspace } = createCoreWorkspaceServices(platform().host);
    await expect(
      workspace.delete_workspace_resource({
        scopeId: "scope",
        relativePath: "test.txt",
        expectedVersion: "confirmed-v1",
      }),
    ).rejects.toThrow("version conflict");
    expect(api.statFile).not.toHaveBeenCalled();
    expect(api.deleteFile).toHaveBeenCalledTimes(1);
    expect(api.deleteFile).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({ expectedVersion: "confirmed-v1" }),
      }),
    );
  });
  it("普通聊天没有额外目录时仍能解析文件引用", async () => {
    vi.mocked(api.resolveReference).mockResolvedValue({
      data: {
        status: "resolved",
        canonicalPath: "/workspace/test.txt",
        baseDirectory: "/workspace",
        workspaceRoot: "/workspace",
      },
    } as never);
    const { workspace } = createCoreWorkspaceServices(platform().host);
    await expect(
      workspace.resolve_workspace_resource_reference({
        scopeId: "scope",
        path: "test.txt",
      }),
    ).resolves.toEqual({
      status: "resolved",
      canonicalPath: "/workspace/test.txt",
      baseDirectory: "/workspace",
      workspaceRoot: "/workspace",
      sourceRoots: [],
    });
  });
  it("Git 返回畸形数据时明确失败，不能显示为干净仓库", async () => {
    vi.mocked(api.openScope).mockResolvedValue({ data: scope } as never);
    vi.mocked(api.gitStatus).mockResolvedValue({ data: {} } as never);
    const { git } = createCoreWorkspaceServices(platform().host);
    await expect(
      git.scoped_git_status({ chatId: "chat", root: "/workspace" }),
    ).rejects.toThrow();
  });
});
