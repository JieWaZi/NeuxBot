import { zipSync } from "fflate";
import { describe, expect, it } from "vitest";

import { validateOfficeArchive } from "./office-archive";
import { OfficePreviewError } from "./office-preview-error";

const docx = () =>
  zipSync({
    "[Content_Types].xml": new TextEncoder().encode("<Types/>"),
    "word/document.xml": new TextEncoder().encode("<document/>"),
  });

describe("Office preview archive guard", () => {
  it("accepts a bounded DOCX package and rejects a renamed package", () => {
    expect(() => validateOfficeArchive(docx(), "docx")).not.toThrow();
    expect(() => validateOfficeArchive(docx(), "pptx")).toThrow(
      new OfficePreviewError("invalidFormat"),
    );
  });

  it("rejects expanded payloads and active content before rendering", () => {
    expect(() =>
      validateOfficeArchive(
        zipSync({
          "[Content_Types].xml": new Uint8Array(1),
          "word/document.xml": new Uint8Array(129 * 1024 * 1024),
        }),
        "docx",
      ),
    ).toThrow(new OfficePreviewError("expandedTooLarge"));
    expect(() =>
      validateOfficeArchive(
        zipSync({
          "[Content_Types].xml": new Uint8Array(1),
          "word/document.xml": new Uint8Array(1),
          "word/vbaProject.bin": new Uint8Array(1),
        }),
        "docx",
      ),
    ).toThrow(new OfficePreviewError("activeContent"));
  });

  it("rejects duplicate or parent-traversing package paths", () => {
    expect(() =>
      validateOfficeArchive(
        zipSync({
          "[Content_Types].xml": new Uint8Array(1),
          "word/document.xml": new Uint8Array(1),
          "word/../other.xml": new Uint8Array(1),
        }),
        "docx",
      ),
    ).toThrow(new OfficePreviewError("invalidArchivePath"));
  });
});
