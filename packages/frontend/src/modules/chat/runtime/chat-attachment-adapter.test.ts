import { describe, expect, it, vi } from "vitest";
import {
  chatAttachmentAdapter,
  fileForFreshSubmission,
  pathFile,
  renewReplyAttachments,
  type BotPathFile,
} from "./chat-attachment-adapter";

const stageAttachment = vi.hoisted(() =>
  vi.fn(async () => "/tmp/bot-paste/image.png"),
);
const discardAttachment = vi.hoisted(() => vi.fn(async () => undefined));
vi.mock("@/shared/platform", () => ({ stageAttachment, discardAttachment }));

describe("chatAttachmentAdapter", () => {
  it("移除未发送附件时释放受管上传", async () => {
    const pending = await chatAttachmentAdapter.add({
      file: pathFile("upload:opaque/note.txt"),
    });
    await chatAttachmentAdapter.remove(pending);
    expect(discardAttachment).toHaveBeenCalledWith("upload:opaque/note.txt");
  });
  it("无路径图片暂存后沿用本地附件发送协议", async () => {
    const file = new File(["image"], "image.png", { type: "image/png" });
    const pending = await chatAttachmentAdapter.add({ file });
    expect(pending).toMatchObject({
      type: "image",
      name: "image.png",
      file: { botSourcePath: "/tmp/bot-paste/image.png" },
    });
    const complete = await chatAttachmentAdapter.send(pending);
    expect(complete.content).toEqual([
      {
        type: "file",
        filename: "image.png",
        data: "/tmp/bot-paste/image.png",
        mimeType: "image/png",
      },
    ]);
  });

  it("原生文件路径不重复暂存，暂存失败不能生成可发送附件", async () => {
    stageAttachment.mockClear();
    await chatAttachmentAdapter.add({ file: pathFile("/work/a.txt") });
    expect(stageAttachment).not.toHaveBeenCalled();
    stageAttachment.mockRejectedValueOnce(new Error("写入失败"));
    await expect(
      chatAttachmentAdapter.add({ file: new File(["image"], "image.png") }),
    ).rejects.toThrow("写入失败");
  });

  it("恢复的浏览器附件发送时重传字节，失败保留旧提交路径和可重试文件", async () => {
    stageAttachment.mockReset();
    stageAttachment.mockResolvedValueOnce("upload:original/image.png");
    const original = new File(["image"], "image.png", { type: "image/png" });
    const originalAttachment = await chatAttachmentAdapter.add({
      file: original,
    });
    const originalMessage =
      await chatAttachmentAdapter.send(originalAttachment);
    const restored = await chatAttachmentAdapter.add({
      file: fileForFreshSubmission(original as BotPathFile),
    });
    expect(stageAttachment).toHaveBeenCalledTimes(1);

    stageAttachment.mockRejectedValueOnce(new Error("写入失败"));
    await expect(chatAttachmentAdapter.send(restored)).rejects.toThrow(
      "写入失败",
    );
    stageAttachment.mockResolvedValueOnce("upload:fresh/image.png");
    const renewed = await chatAttachmentAdapter.send(restored);

    expect(stageAttachment).toHaveBeenLastCalledWith(
      expect.objectContaining({
        size: original.size,
        name: original.name,
        type: original.type,
      }),
    );
    expect(originalMessage.content).toMatchObject([
      { data: "upload:original/image.png" },
    ]);
    expect(renewed.content).toMatchObject([{ data: "upload:fresh/image.png" }]);
  });

  it("批量重传有附件失败时，释放随后完成的新暂存并保留原提交引用", async () => {
    stageAttachment.mockReset();
    discardAttachment.mockClear();
    stageAttachment
      .mockResolvedValueOnce("upload:original/a.txt")
      .mockResolvedValueOnce("upload:original/b.txt");
    const additions = await Promise.all([
      chatAttachmentAdapter.add({ file: new File(["a"], "a.txt") }),
      chatAttachmentAdapter.add({ file: new File(["b"], "b.txt") }),
      chatAttachmentAdapter.add({ file: pathFile("/work/c.txt") }),
    ]);
    const original = await Promise.all(
      additions.map(chatAttachmentAdapter.send),
    );
    let finishUpload: (path: string) => void = () => {
      throw new Error("missing upload");
    };
    stageAttachment
      .mockRejectedValueOnce(new Error("重传失败"))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishUpload = resolve;
          }),
      );
    const renewal = renewReplyAttachments(original);
    const failure = expect(renewal).rejects.toThrow("重传失败");
    await Promise.resolve();
    finishUpload("upload:fresh/b.txt");
    await failure;
    expect(discardAttachment).toHaveBeenCalledExactlyOnceWith(
      "upload:fresh/b.txt",
    );
    expect(original.map((item) => item.content[0])).toEqual([
      expect.objectContaining({ data: "upload:original/a.txt" }),
      expect.objectContaining({ data: "upload:original/b.txt" }),
      expect.objectContaining({ data: "/work/c.txt" }),
    ]);
  });
});
