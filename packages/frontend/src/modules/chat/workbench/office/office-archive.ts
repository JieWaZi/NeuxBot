import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
import { unzipSync } from "fflate";
import { OfficePreviewError } from "./office-preview-error";

const maxEntries = 4096;
export const maxOfficeSourceBytes = workspaceSingleFileBytesLimit;
const maxExpandedBytes = Math.min(128 * 1024 * 1024, maxOfficeSourceBytes * 8);

/** Bound OOXML decompression before a document renderer receives untrusted bytes. */
export function validateOfficeArchive(
  bytes: Uint8Array<ArrayBuffer>,
  kind: "docx" | "pptx",
): void {
  if (bytes.byteLength > maxOfficeSourceBytes)
    throw new OfficePreviewError("sourceTooLarge");
  let entries = 0;
  let expandedBytes = 0;
  const names = new Set<string>();
  try {
    const archive = unzipSync(bytes, {
      filter(entry) {
        entries += 1;
        expandedBytes += entry.originalSize;
        if (
          entries > maxEntries ||
          !Number.isSafeInteger(entry.originalSize) ||
          entry.originalSize < 0 ||
          expandedBytes > maxExpandedBytes
        )
          throw new OfficePreviewError("expandedTooLarge");
        const name = entry.name.replaceAll("\\", "/");
        const normalized = name.toLowerCase();
        if (
          name.startsWith("/") ||
          name.split("/").some((segment) => segment === "..") ||
          name.length > 1024 ||
          names.has(normalized)
        )
          throw new OfficePreviewError("invalidArchivePath");
        if (/(?:^|\/)(?:vbaproject\.bin|activex\/|embeddings\/)/i.test(name))
          throw new OfficePreviewError("activeContent");
        names.add(normalized);
        return false;
      },
    });
    void archive;
  } catch (cause) {
    if (cause instanceof OfficePreviewError) throw cause;
    throw new OfficePreviewError("unreadableArchive", { cause });
  }
  const required =
    kind === "docx" ? "word/document.xml" : "ppt/presentation.xml";
  if (!names.has("[content_types].xml") || !names.has(required))
    throw new OfficePreviewError("invalidFormat");
}
