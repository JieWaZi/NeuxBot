/** 覆盖表格格式识别、预算与解析边界。 */
import { zipSync } from "fflate";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { convertExcel } from "./convert";
import { convertDelimited } from "./delimited";
import { excelFormat } from "./format";
import { XlsxPreviewArchive } from "./xlsx-archive";

const limits = {
  maxBytes: 64 * 1024 * 1024,
  maxCells: 250_000,
  timeoutMs: 15_000,
};

describe("read-only spreadsheet formats", () => {
  it.each(["xlsx", "xls", "csv", "tsv"] as const)(
    "routes %s without case-sensitive extension matching",
    (format) => {
      expect(excelFormat(`/数据/报告.${format.toUpperCase()}`)).toBe(format);
    },
  );

  it.each(["csv", "tsv"] as const)(
    "keeps %s leading zeros, quoted newlines and formula-like text literal",
    (format) => {
      const delimiter = format === "csv" ? "," : "\t";
      const bytes = new TextEncoder().encode(
        `00123${delimiter}"第一行\n第二行"${delimiter}"=SUM(1,2)"\r\n`,
      );
      const sheet = convertDelimited(bytes, format, limits).sheets[0];
      if (!sheet?.celldata)
        throw new Error("Spreadsheet cells were not parsed");
      expect(
        sheet.celldata.filter((cell) => cell.r === 0).map((cell) => cell.v?.v),
      ).toEqual(["00123", "第一行\n第二行", "=SUM(1,2)"]);
    },
  );

  it("rejects an XLSX ZIP whose expanded content exceeds its budget", () => {
    const archive = zipSync({
      "xl/sharedStrings.xml": new Uint8Array(32 * 1024),
    });
    expect(
      () =>
        new XlsxPreviewArchive(archive, {
          ...limits,
          maxBytes: 1024,
        }),
    ).toThrow("tooLarge");
  });

  it("reads the binary Office XLS fixture", async () => {
    const source = readFileSync(
      resolve(
        process.cwd(),
        "src/modules/chat/workbench/excel/fixtures/preview.xls",
      ),
    );
    const result = await convertExcel(Uint8Array.from(source), "xls", limits);
    expect(
      result.sheets
        .flatMap((sheet) => sheet.celldata)
        .some((cell) => String(cell?.v?.v).includes("Office preview")),
    ).toBe(true);
  });

  it("reads the XLSX rich-text fixture", async () => {
    const source = readFileSync(
      resolve(
        process.cwd(),
        "src/modules/chat/workbench/excel/fixtures/rich.xlsx",
      ),
    );
    const result = await convertExcel(Uint8Array.from(source), "xlsx", limits);
    expect(result.sheets.length).toBeGreaterThan(0);
    expect(
      result.sheets.flatMap((sheet) => sheet.celldata).length,
    ).toBeGreaterThan(0);
  });
});
