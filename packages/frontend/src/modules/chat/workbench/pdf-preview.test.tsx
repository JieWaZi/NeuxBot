import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const { open, read } = vi.hoisted(() => ({
  open: vi.fn(async () => ({ id: "scope", root: "/workspace" })),
  read: vi.fn(async () => ({
    bytes: new Uint8Array([37, 80, 68, 70]),
    version: "v1",
  })),
}));
vi.mock("@/shared/platform", () => ({
  openWorkspaceResourceScope: open,
  readWorkspaceResource: read,
}));

import { PdfPreview } from "./pdf-preview";

describe("PDF 原生预览", () => {
  const createDescriptor = Object.getOwnPropertyDescriptor(
    URL,
    "createObjectURL",
  );
  const revokeDescriptor = Object.getOwnPropertyDescriptor(
    URL,
    "revokeObjectURL",
  );

  afterEach(() => {
    vi.clearAllMocks();
    if (createDescriptor)
      Object.defineProperty(URL, "createObjectURL", createDescriptor);
    else Reflect.deleteProperty(URL, "createObjectURL");
    if (revokeDescriptor)
      Object.defineProperty(URL, "revokeObjectURL", revokeDescriptor);
    else Reflect.deleteProperty(URL, "revokeObjectURL");
  });

  it("经 Chat scope 读取完整 bytes，卸载时释放 Blob URL", async () => {
    const create = vi.fn(() => "blob:pdf");
    const revoke = vi.fn();
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: create,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: revoke,
    });
    const view = render(
      <PdfPreview
        chatId="chat-1"
        rootPath="/workspace"
        path="报告 #1.pdf"
        title="报告 #1.pdf"
      />,
    );
    const frame = await screen.findByTitle("报告 #1.pdf");
    expect(frame).toHaveAttribute("src", "blob:pdf");
    expect(open).toHaveBeenCalledWith("chat-1");
    expect(read).toHaveBeenCalledWith(
      "scope",
      "报告 #1.pdf",
      expect.any(AbortSignal),
      "/workspace",
    );
    expect(create).toHaveBeenCalledWith(expect.any(Blob));
    view.unmount();
    expect(revoke).toHaveBeenCalledWith("blob:pdf");
  });
});
