import { describe, expect, it } from "vitest";

import { resolveFileViewAdapter } from "./workspace-file-adapters";

describe("workbench document routing", () => {
  it.each(["pdf", "docx", "pptx", "xlsx", "xls", "csv", "tsv"])(
    "routes .%s to its document preview",
    (extension) => {
      const adapter = resolveFileViewAdapter(
        `目录/报告.${extension.toUpperCase()}`,
      );
      expect(adapter.defaultView).toBe("preview");
      expect(adapter.views).toEqual(["preview"]);
      expect(adapter.renderPreview).toBeTypeOf("function");
    },
  );

  it.each(["md", "markdown"])(
    "routes .%s to editable Markdown preview",
    (extension) => {
      const adapter = resolveFileViewAdapter("目录/说明." + extension);
      expect(adapter.views).toEqual(["code", "preview"]);
      expect(adapter.defaultView).toBe("preview");
      expect(adapter.canEdit).toBe(true);
    },
  );

  it.each(["htm", "html", "xhtml"])(
    "routes .%s to editable HTML",
    (extension) => {
      const adapter = resolveFileViewAdapter("目录/页面." + extension);
      expect(adapter.views).toEqual(["code", "preview"]);
      expect(adapter.defaultView).toBe("code");
      expect(adapter.canEdit).toBe(true);
    },
  );

  it("routes images to preview and ordinary text to the editor", () => {
    const image = resolveFileViewAdapter("目录/图像.png", {
      name: "图像.png",
      relativePath: "目录/图像.png",
      kind: "image",
      size: 3,
      dataUrl: "data:image/png;base64,AA==",
    });
    expect(image.views).toEqual(["preview"]);
    expect(image.canZoom).toBe(true);
    const text = resolveFileViewAdapter("目录/代码.ts");
    expect(text.views).toEqual(["code"]);
    expect(text.canEdit).toBe(true);
  });
});
