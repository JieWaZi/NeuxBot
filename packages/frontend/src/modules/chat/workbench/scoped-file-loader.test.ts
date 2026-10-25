import { beforeEach, describe, expect, it, vi } from "vitest";

const { open, stat, read } = vi.hoisted(() => ({
  open: vi.fn(async () => ({ id: "scope", root: "/workspace" })),
  stat: vi.fn(async () => ({
    relativePath: "报告 #1.txt",
    size: 5,
    version: "version-1",
    modifiedAt: 1,
    mediaType: "text/plain",
  })),
  read: vi.fn(async () => ({
    relativePath: "报告 #1.txt",
    size: 5,
    version: "version-1",
    modifiedAt: 1,
    mediaType: "text/plain",
    bytes: new TextEncoder().encode("hello"),
  })),
}));
vi.mock("@/shared/platform", () => ({
  openWorkspaceResourceScope: open,
  statWorkspaceResource: stat,
  readWorkspaceResource: read,
}));

import { readScopedFilePreview } from "./scoped-file-loader";

describe("scoped file preview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("按 Core 授权范围读取含空格与 # 的文本，并保留文件版本", async () => {
    const controller = new AbortController();
    const file = await readScopedFilePreview(
      "a".repeat(32),
      "/workspace",
      "报告 #1.txt",
      controller.signal,
    );
    expect(file).toMatchObject({
      name: "报告 #1.txt",
      kind: "text",
      content: "hello",
      version: "version-1",
    });
    expect(stat).toHaveBeenCalledWith("scope", "报告 #1.txt", "/workspace");
    expect(read).toHaveBeenCalledWith(
      "scope",
      "报告 #1.txt",
      controller.signal,
      "/workspace",
    );
  });

  it("PDF/Office 不交给文本代码视图或无谓读取 bytes", async () => {
    stat.mockResolvedValueOnce({
      relativePath: "deck.pptx",
      size: 100,
      version: "version-2",
      modifiedAt: 2,
      mediaType:
        "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    });
    const file = await readScopedFilePreview(
      "a".repeat(32),
      "/workspace",
      "deck.pptx",
    );
    expect(file.kind).toBe("binary");
    expect(read).not.toHaveBeenCalled();
  });

  it("超过文本预览上限的 PDF 仍可交给独立预览器", async () => {
    stat.mockResolvedValueOnce({
      relativePath: "报告.pdf",
      size: 20 * 1024 * 1024,
      version: "pdf-v1",
      modifiedAt: 2,
      mediaType: "application/pdf",
    });
    const file = await readScopedFilePreview(
      "a".repeat(32),
      "/workspace",
      "报告.pdf",
    );
    expect(file).toMatchObject({ kind: "binary", version: "pdf-v1" });
    expect(read).not.toHaveBeenCalled();
  });

  it("大于文本查看上限的 CSV 交给表格解析器而不先解码", async () => {
    stat.mockResolvedValueOnce({
      relativePath: "数据.csv",
      size: 12 * 1024 * 1024,
      version: "csv-v1",
      modifiedAt: 2,
      mediaType: "text/csv",
    });
    const file = await readScopedFilePreview(
      "a".repeat(32),
      "/workspace",
      "数据.csv",
    );
    expect(file).toMatchObject({ kind: "binary", version: "csv-v1" });
    expect(read).not.toHaveBeenCalled();
  });

  it("图片经受限读取转换为预览地址", async () => {
    stat.mockResolvedValueOnce({
      relativePath: "图像.png",
      size: 3,
      version: "image-1",
      modifiedAt: 2,
      mediaType: "image/png",
    });
    read.mockResolvedValueOnce({
      relativePath: "图像.png",
      size: 3,
      version: "image-1",
      modifiedAt: 2,
      mediaType: "image/png",
      bytes: new Uint8Array([255, 0, 1]),
    });
    const file = await readScopedFilePreview(
      "a".repeat(32),
      "/workspace",
      "图像.png",
    );
    expect(file).toMatchObject({
      kind: "image",
      version: "image-1",
      dataUrl: "data:image/png;base64,/wAB",
    });
  });
});
