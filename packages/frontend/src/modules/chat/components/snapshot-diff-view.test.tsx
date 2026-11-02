import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import "@/shared/i18n";
import { SnapshotDiffView, snapshotDiffText } from "./snapshot-diff-view";
const comparison = {
  state: "text",
  beforeExists: true,
  afterExists: true,
  coarse: true,
  truncated: true,
  hunks: [
    {
      oldStart: 10,
      newStart: 12,
      oldLines: 1,
      newLines: 1,
      lines: [
        { kind: "delete", text: "before" },
        { kind: "add", text: "after", noNewline: true },
      ],
    },
  ],
};
describe("persisted bounded diff", () => {
  it("直接渲染服务端hunk，切换布局不比较正文", () => {
    const view = render(<SnapshotDiffView comparison={comparison} />);
    expect(screen.getByText("差异已简化为整段替换")).toBeTruthy();
    expect(screen.getByText("差异内容已达到展示上限")).toBeTruthy();
    expect(screen.getByText("文件末尾无换行")).toBeTruthy();
    view.rerender(<SnapshotDiffView comparison={comparison} split />);
    expect(screen.getByText("before")).toBeTruthy();
    expect(screen.getByText("after")).toBeTruthy();
    expect(snapshotDiffText(comparison)).toContain(
      "-before\n+after\n\\ No newline at end of file",
    );
  });
  it("超限及二进制只展示明确状态", () => {
    render(<SnapshotDiffView comparison={{ state: "oversized" }} />);
    expect(screen.getByText("超过文本预览上限")).toBeTruthy();
  });
  it("空文件与仅权限变化有明确内容，不展示空白diff", () => {
    const view = render(
      <SnapshotDiffView
        comparison={{
          state: "text",
          beforeExists: false,
          afterExists: true,
          hunks: [],
        }}
      />,
    );
    expect(screen.getByText("新增空文件")).toBeTruthy();
    view.rerender(
      <SnapshotDiffView
        comparison={{
          state: "text",
          beforeExists: true,
          afterExists: true,
          beforeMode: 0o100644,
          afterMode: 0o100755,
          hunks: [],
        }}
      />,
    );
    expect(screen.getByText("文件权限：100644 → 100755")).toBeTruthy();
    expect(screen.getByText("文件内容未改变")).toBeTruthy();
  });
});
