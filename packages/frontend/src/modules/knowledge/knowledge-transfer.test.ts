import { describe, expect, it } from "vitest";
import { encodeUpload } from "./knowledge-upload-source";
import { isKnowledgeJobBusy, resourceKey } from "./knowledge-transfer";

describe("知识数据边界", () => {
  it("保留实际二进制字节，而不是由名称伪造原件", async () => {
    const data = new Uint8Array([0, 255, 128, 37, 80, 68, 70]);
    const file = {
      name: "原件.pdf",
      type: "application/pdf",
      size: data.length,
      arrayBuffer: async () => data.buffer,
    };
    const upload = await encodeUpload(file, "request-1");
    expect(upload).toMatchObject({
      requestId: "request-1",
      filename: "原件.pdf",
      bytes: "AP+AJVBERg==",
    });
  });
  it("在读字节前拒绝超过32MiB的原件", async () => {
    let reads = 0;
    await expect(
      encodeUpload(
        {
          name: "large.pdf",
          type: "application/pdf",
          size: 32 * 1024 * 1024 + 1,
          arrayBuffer: async () => {
            reads++;
            return new ArrayBuffer(0);
          },
        },
        "r",
      ),
    ).rejects.toThrow("source_too_large");
    expect(reads).toBe(0);
  });
  it("运行中与未知状态保护文档操作，持久终态解除保护", () => {
    expect(isKnowledgeJobBusy("queued")).toBe(true);
    expect(isKnowledgeJobBusy("running")).toBe(true);
    expect(isKnowledgeJobBusy("cancel_requested")).toBe(true);
    expect(isKnowledgeJobBusy("unknown")).toBe(true);
    expect(isKnowledgeJobBusy("succeeded")).toBe(true);
    expect(isKnowledgeJobBusy("completed")).toBe(false);
    expect(isKnowledgeJobBusy("partial_failed")).toBe(false);
    expect(isKnowledgeJobBusy("failed")).toBe(false);
    expect(isKnowledgeJobBusy("cancelled")).toBe(false);
  });
  it("相同库ID来自不同连接时不会共享缓存身份", () => {
    expect(
      resourceKey({
        backend: "remote",
        connectionId: "a",
        knowledgeBaseId: "same",
      }),
    ).not.toBe(
      resourceKey({
        backend: "remote",
        connectionId: "b",
        knowledgeBaseId: "same",
      }),
    );
  });
});
