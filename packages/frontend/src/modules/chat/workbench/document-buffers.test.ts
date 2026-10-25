import { describe, expect, it } from "vitest";

import {
  preventUnloadWithDirtyDrafts,
  workbenchDocumentBuffers as buffers,
} from "./document-buffers";

describe("workbench document buffers", () => {
  it("保留未保存内容并按保存修订号承接继续输入", () => {
    const key = "test:doc:revision";
    buffers.begin(key, "original", "v1");
    expect(buffers.isDirty(key)).toBe(false);
    const saving = buffers.change(key, "first");
    buffers.change(key, "second");
    expect(buffers.saved(key, saving.revision, saving.text, "v2")).toBe(false);
    expect(buffers.get(key)).toMatchObject({
      text: "second",
      savedText: "first",
      baseVersion: "v2",
    });
    expect(buffers.isDirty(key)).toBe(true);
    const latest = buffers.get(key);
    if (!latest) throw new Error("草稿没有保留");
    expect(buffers.saved(key, latest.revision, latest.text, "v3")).toBe(true);
    expect(buffers.get(key)).toBeUndefined();
  });

  it("关闭确认取消后仍可恢复草稿；丢弃才移除", () => {
    const key = "test:doc:close";
    buffers.begin(key, "before", "v1");
    buffers.change(key, "draft");
    expect(buffers.begin(key, "fresh external content", "v2").text).toBe(
      "draft",
    );
    expect(buffers.isDirty(key)).toBe(true);
    buffers.discard(key);
    expect(buffers.get(key)).toBeUndefined();
  });

  it("文件 Tab 关闭时只检查并清理该 Tab 内全部文件草稿", () => {
    const first = "test:files:one:file:/a.md";
    const second = "test:files:one:file:/b.md";
    const other = "test:files:two:file:/c.md";
    buffers.begin(first, "a");
    buffers.change(first, "a draft");
    buffers.begin(second, "b");
    buffers.change(second, "b draft");
    buffers.begin(other, "c");
    buffers.change(other, "c draft");

    expect(buffers.hasDirtyPrefix("test:files:one:file:")).toBe(true);
    buffers.discardPrefix("test:files:one:file:");
    expect(buffers.get(first)).toBeUndefined();
    expect(buffers.get(second)).toBeUndefined();
    expect(buffers.isDirty(other)).toBe(true);
    buffers.discard(other);
  });

  it("窗口关闭或刷新仅在存在脏草稿时进入确认协议", () => {
    const key = "test:doc:window-close";
    const clean = new Event("beforeunload", { cancelable: true });
    preventUnloadWithDirtyDrafts(clean as BeforeUnloadEvent);
    expect(clean.defaultPrevented).toBe(false);
    buffers.begin(key, "before", "v1");
    buffers.change(key, "draft");
    const dirty = new Event("beforeunload", { cancelable: true });
    preventUnloadWithDirtyDrafts(dirty as BeforeUnloadEvent);
    expect(dirty.defaultPrevented).toBe(true);
    buffers.discard(key);
  });
});
