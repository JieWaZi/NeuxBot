import { expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import type { CreateChatData } from "@/shared/api/generated/types.gen";
import { prepareFirstChatSubmission } from "./first-chat-submission";

it("接纳未知时保留原请求，旧附件不可读取也不能创建替代请求", async () => {
  const body: CreateChatData["body"] = {
    clientRequestId: "original",
    wakerId: "partner",
    runtimeId: "codex",
    text: "整理文件",
    permissionLevel: "full-access",
    attachments: [{ sourcePath: "upload:example" }],
  };
  const finished = vi.fn();
  const send = vi
    .fn()
    .mockRejectedValueOnce(new TypeError("network"))
    .mockRejectedValueOnce(new ApiError("附件失效", "BadRequest", 400))
    .mockResolvedValueOnce({ chat: { id: "accepted" } });
  const prepared = prepareFirstChatSubmission(body, send, finished);

  await expect(prepared.accept()).rejects.toThrow("network");
  await expect(prepared.accept()).rejects.toThrow("附件失效");
  expect(prepared.hasUnconfirmedReceipt?.()).toBe(true);
  expect(finished).not.toHaveBeenCalled();
  await expect(prepared.accept()).resolves.toMatchObject({
    chat: { id: "accepted" },
  });
  prepared.confirmAccepted?.();
  expect(finished).toHaveBeenCalledTimes(1);
  expect(send.mock.calls).toEqual([[body], [body], [body]]);
});

it("原生设置明确拒绝后结束原提交，允许修改配置", async () => {
  const body: CreateChatData["body"] = {
    clientRequestId: "original",
    wakerId: "partner",
    runtimeId: "codex",
    permissionLevel: "default",
    text: "整理文件",
  };
  const finished = vi.fn();
  const send = vi
    .fn()
    .mockRejectedValue(
      new ApiError("模型拒绝", "RuntimeModelSelectionFailed", 500),
    );
  const prepared = prepareFirstChatSubmission(body, send, finished);
  await expect(prepared.accept()).rejects.toThrow("模型拒绝");
  expect(prepared.hasUnconfirmedReceipt?.()).toBe(false);
  expect(finished).toHaveBeenCalledTimes(1);
});

it("缺少权威对话标识的成功响应仍需确认，原请求明确缺失附件时可结束", async () => {
  const body: CreateChatData["body"] = {
    clientRequestId: "original",
    wakerId: "partner",
    runtimeId: "codex",
    permissionLevel: "default",
    text: "整理文件",
  };
  const finished = vi.fn();
  const send = vi
    .fn()
    .mockResolvedValueOnce({})
    .mockRejectedValueOnce(new ApiError("附件已过期", "40000000017", 400));
  const prepared = prepareFirstChatSubmission(body, send, finished);
  await expect(prepared.accept()).rejects.toThrow();
  expect(prepared.hasUnconfirmedReceipt?.()).toBe(true);
  await expect(prepared.accept()).rejects.toThrow("附件已过期");
  expect(prepared.hasUnconfirmedReceipt?.()).toBe(false);
  expect(finished).toHaveBeenCalledTimes(1);
});

it.each(["WakerNotFound", "ProjectNotFound"])(
  "回执未确认后收到 %s，允许重新选择伙伴或项目",
  async (code) => {
    const body: CreateChatData["body"] = {
      clientRequestId: "original",
      wakerId: "partner",
      projectId: "project",
      runtimeId: "codex",
      permissionLevel: "default",
      text: "整理文件",
    };
    const finished = vi.fn();
    const send = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("network"))
      .mockRejectedValueOnce(new ApiError("原目标已删除", code, 404));
    const prepared = prepareFirstChatSubmission(body, send, finished);

    await expect(prepared.accept()).rejects.toThrow("network");
    expect(prepared.hasUnconfirmedReceipt?.()).toBe(true);
    await expect(prepared.accept()).rejects.toThrow("原目标已删除");
    expect(prepared.hasUnconfirmedReceipt?.()).toBe(false);
    expect(finished).toHaveBeenCalledTimes(1);
  },
);
