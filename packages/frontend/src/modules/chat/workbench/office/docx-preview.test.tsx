import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { i18n } from "@/shared/i18n";
import { OfficePreviewError } from "./office-preview-error";

const { read, renderDocument } = vi.hoisted(() => ({
  read: vi.fn(
    async (_chatId: string | undefined, _rootPath: string, path: string) =>
      new TextEncoder().encode(path).buffer,
  ),
  renderDocument: vi.fn(),
}));
vi.mock("./read-office-bytes", () => ({ readOfficeBytes: read }));
vi.mock("docx-preview", () => ({ renderAsync: renderDocument }));

import { DocxPreview } from "./docx-preview";

afterEach(async () => {
  await i18n.changeLanguage("zh-CN");
});

describe("DOCX 预览", () => {
  it("按当前语言展示 Office 错误，切换语言后同步更新", async () => {
    read.mockRejectedValueOnce(new OfficePreviewError("invalidFormat"));
    const view = render(
      <DocxPreview
        chatId="chat-1"
        rootPath="/workspace"
        path="broken.docx"
        title="broken.docx"
      />,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "文件内容与文档格式不匹配",
    );
    await act(async () => {
      await i18n.changeLanguage("en-US");
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "File content does not match its document format",
    );
    view.unmount();
  });

  it("旧 generation 的异步渲染只写入已卸载的容器", async () => {
    const pending: Array<{
      stage: HTMLElement;
      resolve: () => void;
    }> = [];
    renderDocument.mockImplementation(
      (bytes: ArrayBuffer, stage: HTMLElement) =>
        new Promise<void>((resolve) => {
          pending.push({
            stage,
            resolve: () => {
              stage.textContent = new TextDecoder().decode(bytes);
              resolve();
            },
          });
        }),
    );
    const view = render(
      <DocxPreview
        chatId="chat-1"
        rootPath="/workspace"
        path="A.docx"
        title="A.docx"
      />,
    );
    await waitFor(() => expect(pending).toHaveLength(1));
    view.rerender(
      <DocxPreview
        chatId="chat-1"
        rootPath="/workspace"
        path="B.docx"
        title="B.docx"
      />,
    );
    await waitFor(() => expect(pending).toHaveLength(2));
    await act(async () => pending[1]?.resolve());
    expect(screen.getByLabelText("B.docx")).toHaveTextContent("B.docx");
    await act(async () => pending[0]?.resolve());
    expect(screen.getByLabelText("B.docx")).toHaveTextContent("B.docx");
    expect(pending[0]?.stage.isConnected).toBe(false);
  });
});
