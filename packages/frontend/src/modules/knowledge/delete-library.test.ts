import { beforeEach, describe, expect, it, vi } from "vitest";
import { emptyLibrary } from "./knowledge-model";
import { createLibraryDeleter } from "./delete-library";
import "./use-copy";
import { ApiError } from "@/shared/api/errors";
let deleteLibraryPermanently: ReturnType<typeof createLibraryDeleter>;
const api = vi.hoisted(() => ({
  getRemoteCapabilities: vi.fn(),
  getRemoteOperation: vi.fn(),
  deleteLibrary: vi.fn(),
  deleteRemoteLibrary: vi.fn(),
  getLibraryDeletion: vi.fn(),
  getRemoteLibraryDeletion: vi.fn(),
  recoverLibraryDeletion: vi.fn(),
  recoverRemoteLibraryDeletion: vi.fn(),
  removeRemoteMount: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", () => api);
const library = () => ({
  ...emptyLibrary("kb", "Knowledge", "general"),
  revision: 4,
});
describe("整库删除确认", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    deleteLibraryPermanently = createLibraryDeleter();
    api.getRemoteOperation.mockResolvedValue({
      data: undefined,
      response: { status: 404 },
    });
    api.getRemoteCapabilities.mockResolvedValue({
      data: {
        operations: { libraryDelete: true },
        details: {
          libraryDelete: {
            supported: true,
            permission: "allowed",
            condition: "ready",
          },
        },
      },
    });
    api.deleteLibrary.mockResolvedValue({ data: { state: "accepted" } });
    api.deleteRemoteLibrary.mockResolvedValue({ data: { state: "accepted" } });
  });
  it("远端挂载只移除本机入口，不删除远端知识库或恢复记录", async () => {
    const value = {
      ...library(),
      mountId: "mount-a",
      mountRevision: 2,
      ref: {
        backend: "integration",
        connectionId: "remote",
        knowledgeBaseId: "kb",
      },
    };
    api.removeRemoteMount.mockResolvedValue({ data: { removed: true } });
    await deleteLibraryPermanently(value, "remove-mount");
    expect(api.removeRemoteMount).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { mountId: "mount-a" },
        query: { revision: 2 },
      }),
    );
    expect(api.deleteRemoteLibrary).not.toHaveBeenCalled();
    expect(api.deleteLibrary).not.toHaveBeenCalled();
    expect(api.getRemoteCapabilities).not.toHaveBeenCalled();
  });
  it("未挂载远端库拒绝删除且不调用远端删除接口", async () => {
    const value = {
      ...library(),
      storage: "remote" as const,
      ref: {
        backend: "custom",
        connectionId: "remote",
        knowledgeBaseId: "kb",
      },
    };
    await expect(
      deleteLibraryPermanently(value, "forbidden"),
    ).rejects.toMatchObject({ code: "KnowledgeUnsupported" });
    expect(api.deleteRemoteLibrary).not.toHaveBeenCalled();
    expect(api.getRemoteLibraryDeletion).not.toHaveBeenCalled();
    expect(api.recoverRemoteLibraryDeletion).not.toHaveBeenCalled();
  });
  it("接纳回执不会冒充原件清理完成", async () => {
    api.getLibraryDeletion.mockResolvedValue({
      data: { state: "blocked", version: 1 },
    });
    api.recoverLibraryDeletion.mockResolvedValue({
      data: { state: "accepted" },
    });
    await expect(
      deleteLibraryPermanently(library(), "stable"),
    ).rejects.toMatchObject({ code: "KNOWLEDGE_DELETION_INCOMPLETE" });
    expect(api.recoverLibraryDeletion).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { requestId: expect.any(String), expectedVersion: 1 },
      }),
    );
  });
  it.each([408, 500, 503])(
    "清理恢复 HTTP %i 未知结果重用原请求身份",
    async (status) => {
      api.getLibraryDeletion.mockResolvedValue({
        data: { state: "pending", version: 1 },
      });
      api.recoverLibraryDeletion.mockRejectedValue(
        new ApiError("response lost", "HTTP_ERROR", status),
      );
      const remove = createLibraryDeleter();
      await expect(remove(library(), "original-delete")).rejects.toBeInstanceOf(
        ApiError,
      );
      await expect(remove(library(), "reopened-delete")).rejects.toBeInstanceOf(
        ApiError,
      );
      const [first, second] = api.recoverLibraryDeletion.mock.calls;
      expect(first?.[0].body.requestId).toBeTruthy();
      expect(second?.[0].body).toEqual(first?.[0].body);
    },
  );
  it("关闭后重开确认框仍核对原删除及其冻结版本", async () => {
    api.deleteLibrary.mockRejectedValueOnce(new ApiError("unknown", "NETWORK"));
    await expect(
      deleteLibraryPermanently(library(), "first-dialog"),
    ).rejects.toBeInstanceOf(ApiError);
    api.getLibraryDeletion.mockResolvedValue({
      data: { state: "completed", version: 2 },
    });
    await deleteLibraryPermanently(
      { ...library(), revision: 5 },
      "reopened-dialog",
    );
    expect(api.deleteLibrary.mock.calls[1]?.[0]).toMatchObject({
      body: { requestId: "first-dialog", expectedVersion: 4 },
    });
  });
  it("响应丢失时保留未知错误，不能宣称删除完成", async () => {
    api.deleteLibrary.mockRejectedValue(new Error("unknown outcome"));
    await expect(deleteLibraryPermanently(library(), "stable")).rejects.toThrow(
      "unknown outcome",
    );
    expect(api.getLibraryDeletion).not.toHaveBeenCalled();
  });
});

it("核心已删库但聊天清理失败时，关闭重开继续原删除身份", async () => {
  vi.resetAllMocks();
  const remove = createLibraryDeleter();
  api.deleteLibrary
    .mockRejectedValueOnce(
      new ApiError("chat log cleanup failed", "ChatLogError", 500),
    )
    .mockResolvedValueOnce({ data: { state: "accepted" } });
  api.getLibraryDeletion.mockResolvedValue({
    data: { state: "completed", version: 2 },
  });
  await expect(remove(library(), "original-delete")).rejects.toBeInstanceOf(
    ApiError,
  );
  await remove({ ...library(), revision: 5 }, "reopened-delete");
  expect(api.deleteLibrary.mock.calls[1]?.[0]).toMatchObject({
    body: { requestId: "original-delete", expectedVersion: 4 },
  });
});
