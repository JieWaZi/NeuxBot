import {
  extractTableDataFromElement,
  tableDataToCSV,
  tableDataToMarkdown,
  tableDataToTSV,
} from "streamdown";

import { saveBlob } from "@/shared/platform";

export type TableExportFormat = "csv" | "markdown" | "tsv";

export type TableExportPayload = {
  content: string;
  fileName: string;
  mimeType: string;
};

export async function copyPlainText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export async function downloadTextFile(
  fileName: string,
  content: string,
  mimeType: string,
): Promise<boolean> {
  const bom = mimeType.startsWith("text/csv") ? "\uFEFF" : "";
  return saveBlob(new Blob([`${bom}${content}`], { type: mimeType }), fileName);
}

export function serializeTable(
  table: HTMLElement,
  format: TableExportFormat,
): TableExportPayload {
  const data = extractTableDataFromElement(table);
  if (format === "csv") {
    return {
      content: tableDataToCSV(data),
      fileName: "table.csv",
      mimeType: "text/csv",
    };
  }
  if (format === "tsv") {
    return {
      content: tableDataToTSV(data),
      fileName: "table.tsv",
      mimeType: "text/tab-separated-values",
    };
  }
  return {
    content: tableDataToMarkdown(data),
    fileName: "table.md",
    mimeType: "text/markdown",
  };
}
