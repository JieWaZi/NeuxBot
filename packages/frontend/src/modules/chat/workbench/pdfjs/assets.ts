/** Vite emits the version-matched PDF.js worker and local CMaps/fonts/WASM. */
import workerSource from "pdfjs-dist/build/pdf.worker.min.mjs?raw";

export { workerSource };

export function pdfAssetUrl(kind: "cmaps" | "standard_fonts" | "wasm"): string {
  return new URL(
    `pdfjs-assets/${kind}/`,
    new URL(import.meta.env.BASE_URL, document.baseURI),
  ).href;
}
