/** 验证 PDF 文档页的身份与资源生命周期。 */
import type { PDFPageProxy } from "pdfjs-dist";
import { describe, expect, it, vi } from "vitest";

import { renderPdfPage } from "./document";
import { deferred } from "./deferred";

describe("PDF.js page rendering", () => {
  it("does not write a canvas after a delayed page lookup is cancelled", async () => {
    const lookup = deferred<PDFPageProxy>();
    const controller = new AbortController();
    const canvas = document.createElement("canvas");
    const cleanup = vi.fn();
    const render = vi.fn();
    const pending = renderPdfPage(
      { numPages: 1, getPage: () => lookup.promise },
      1,
      canvas,
      controller.signal,
      1,
    );
    const rejection = expect(pending).rejects.toMatchObject({
      name: "AbortError",
    });
    controller.abort();
    lookup.resolve({ cleanup, render } as unknown as PDFPageProxy);
    await rejection;
    expect(render).not.toHaveBeenCalled();
    expect(cleanup).toHaveBeenCalledOnce();
    expect(canvas.style.getPropertyValue("--pdf-page-width")).toBe("");
  });

  it("bounds raster pixels while retaining the page's display size", async () => {
    const cleanup = vi.fn();
    const page = {
      getViewport: () => ({ width: 8192, height: 8192 }),
      render: () => ({ promise: Promise.resolve(), cancel: vi.fn() }),
      cleanup,
    } as unknown as PDFPageProxy;
    const canvas = document.createElement("canvas");
    await expect(
      renderPdfPage(
        { numPages: 1, getPage: async () => page },
        1,
        canvas,
        new AbortController().signal,
        2,
      ),
    ).resolves.toEqual({ width: 8192, height: 8192 });
    expect(canvas.width * canvas.height).toBe(16_777_216);
    expect(cleanup).toHaveBeenCalledOnce();
  });
});
