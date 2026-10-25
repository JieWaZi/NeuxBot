import { afterEach, describe, expect, it, vi } from "vitest";

const { saveBlob } = vi.hoisted(() => ({
  saveBlob: vi.fn<(blob: Blob, fileName: string) => Promise<boolean>>(
    async () => true,
  ),
}));

vi.mock("@/shared/platform", () => ({ saveBlob }));

import {
  copyPlainText,
  downloadTextFile,
  serializeTable,
} from "./chat-markdown-export";

describe("chat markdown export", () => {
  afterEach(() => {
    saveBlob.mockClear();
  });

  it("serializes a table into markdown, csv and tsv", () => {
    document.body.innerHTML = `
      <table>
        <thead>
          <tr><th>步骤</th><th>动作</th></tr>
        </thead>
        <tbody>
          <tr><td>1</td><td>访问应用</td></tr>
        </tbody>
      </table>
    `;
    const table = document.querySelector("table");
    expect(table).not.toBeNull();
    if (!table) return;

    expect(serializeTable(table, "markdown").content).toContain(
      "| 步骤 | 动作 |",
    );
    expect(serializeTable(table, "csv")).toEqual({
      content: "步骤,动作\n1,访问应用",
      fileName: "table.csv",
      mimeType: "text/csv",
    });
    expect(serializeTable(table, "tsv")).toEqual({
      content: "步骤\t动作\n1\t访问应用",
      fileName: "table.tsv",
      mimeType: "text/tab-separated-values",
    });
  });

  it("copies plain text through the clipboard API", async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    await expect(copyPlainText("hello")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("hello");
  });

  it("downloads text through the desktop save dialog", async () => {
    await expect(
      downloadTextFile("table.csv", "a,b", "text/csv"),
    ).resolves.toBe(true);

    expect(saveBlob).toHaveBeenCalledWith(expect.any(Blob), "table.csv");
    const blob = saveBlob.mock.calls[0]?.[0];
    expect(blob?.type).toBe("text/csv");
    await expect(blob?.text()).resolves.toContain("a,b");
    const bytes = new Uint8Array(
      (await blob?.arrayBuffer()) ?? new ArrayBuffer(0),
    );
    expect([...bytes.slice(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
  });
});
