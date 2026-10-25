/** 验证本地 PDF Worker 启动、失败与释放。 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PdfDocument } from "./document";
import { deferred } from "./deferred";

const api = vi.hoisted(() => ({
  getDocument: vi.fn(),
  createWorker: vi.fn(),
  destroyBridge: vi.fn(),
}));
vi.mock("pdfjs-dist", () => ({
  getDocument: api.getDocument,
  PDFWorker: { create: api.createWorker },
}));
vi.mock("./assets", () => ({
  workerSource: "export const WorkerMessageHandler = {}",
  pdfAssetUrl: (kind: string) => `ally://app/pdfjs-assets/${kind}/`,
}));

import { openPdf } from "./runtime";

const NativeURL = URL;
const workers: ControlledWorker[] = [];
const createURL = vi.fn(() => "blob:pdf-worker");
const revokeURL = vi.fn();

class ControlledWorker extends EventTarget {
  readonly terminate = vi.fn();
  constructor(
    readonly url: string,
    readonly options: WorkerOptions,
  ) {
    super();
    workers.push(this);
  }
  ready() {
    this.dispatchEvent(
      new MessageEvent("message", {
        data: { type: "neuxbot-pdf-worker-ready" },
      }),
    );
  }
}

function firstWorker(): ControlledWorker {
  const worker = workers[0];
  if (!worker) throw new Error("PDF worker was not created");
  return worker;
}

beforeEach(() => {
  workers.length = 0;
  vi.clearAllMocks();
  api.createWorker.mockReturnValue({ destroy: api.destroyBridge });
  vi.stubGlobal("Worker", ControlledWorker);
  vi.stubGlobal(
    "URL",
    class extends NativeURL {
      static override createObjectURL = createURL;
      static override revokeObjectURL = revokeURL;
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("PDF.js worker ownership", () => {
  it("starts a module Worker, copies the PDF bytes and releases all native resources", async () => {
    const loading = deferred<PdfDocument>();
    const destroy = vi.fn(async () => {});
    api.getDocument.mockReturnValue({ promise: loading.promise, destroy });
    const source = new TextEncoder().encode("%PDF-test");
    const session = openPdf(source, new AbortController().signal, vi.fn());
    const worker = firstWorker();
    expect(worker.options).toEqual({ type: "module", name: "neuxbot-pdf" });
    expect(api.getDocument).not.toHaveBeenCalled();
    worker.ready();
    await vi.waitFor(() => expect(api.getDocument).toHaveBeenCalledOnce());
    const options = api.getDocument.mock.calls[0]?.[0] as {
      data: Uint8Array;
      cMapUrl: string;
      standardFontDataUrl: string;
      wasmUrl: string;
    };
    expect(options).toMatchObject({
      cMapUrl: "ally://app/pdfjs-assets/cmaps/",
      standardFontDataUrl: "ally://app/pdfjs-assets/standard_fonts/",
      wasmUrl: "ally://app/pdfjs-assets/wasm/",
    });
    expect(options.data).not.toBe(source);
    expect(options.data.buffer).not.toBe(source.buffer);
    loading.resolve({ numPages: 1, getPage: vi.fn() });
    await session.document;
    await session.dispose();
    await session.dispose();
    expect(destroy).toHaveBeenCalledOnce();
    expect(api.destroyBridge).toHaveBeenCalledOnce();
    expect(worker.terminate).toHaveBeenCalledOnce();
    expect(revokeURL).toHaveBeenCalledExactlyOnceWith("blob:pdf-worker");
  });

  it("cancels startup before the Worker handshake", async () => {
    const controller = new AbortController();
    const session = openPdf(
      new TextEncoder().encode("%PDF-test"),
      controller.signal,
      vi.fn(),
    );
    const rejection = expect(session.document).rejects.toMatchObject({
      name: "AbortError",
    });
    controller.abort();
    await rejection;
    await session.dispose();
    firstWorker().ready();
    expect(api.getDocument).not.toHaveBeenCalled();
    expect(firstWorker().terminate).toHaveBeenCalledOnce();
    expect(revokeURL).toHaveBeenCalledOnce();
  });
});
