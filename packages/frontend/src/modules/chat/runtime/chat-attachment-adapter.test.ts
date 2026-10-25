import { describe, expect, it, vi } from "vitest";
import { chatAttachmentAdapter, pathFile } from "./chat-attachment-adapter";

const stageAttachment = vi.hoisted(() =>
  vi.fn(async () => "/tmp/ally-paste/image.png"),
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
      file: { allySourcePath: "/tmp/ally-paste/image.png" },
    });
    const complete = await chatAttachmentAdapter.send(pending);
    expect(complete.content).toEqual([
      {
        type: "file",
        filename: "image.png",
        data: "/tmp/ally-paste/image.png",
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
});
