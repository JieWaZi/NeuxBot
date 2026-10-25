import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceFileViewer } from "@/modules/chat";
import type { GithubComJieWaZiNeuxBotServerApisWorkflowsWorkflowNodeResultResponse as ResultResponse } from "@/shared/api/generated/types.gen";
import "@/shared/i18n";
import { NodeResultFiles } from "./node-result-files";

vi.mock("@/modules/chat", () => ({
  WorkspaceFileViewer: ({
    path,
    rootPath,
    scopeChatId,
    fileView,
    onFileViewChange,
    readOnly,
  }: ComponentProps<typeof WorkspaceFileViewer>) => (
    <div
      data-testid="viewer"
      data-path={path}
      data-root={rootPath}
      data-chat={scopeChatId}
      data-view={fileView}
      data-readonly={readOnly}
    >
      <button onClick={() => onFileViewChange?.("code")}>查看源码</button>
    </div>
  ),
  WorkspaceDetailSplit: ({
    main,
    sidebar,
  }: {
    main: ReactNode;
    sidebar: ReactNode;
  }) => (
    <>
      {main}
      {sidebar}
    </>
  ),
}));
const single: ResultResponse = {
  present: true,
  chatId: "worker-chat",
  workingDirectory: "/workflow/run-1",
  fileView: { title: "缺陷 HTML 报告" },
  result: "/workflow/run-1/report.html",
  files: [
    {
      id: "report",
      field: "",
      name: "report.html",
      mediaType: "text/html",
      available: true,
      size: 300,
    },
  ],
};
const props = {
  active: true,
  compact: false,
  onOpen: vi.fn(),
  onDownload: vi.fn(),
  downloadError: undefined,
};
describe("workflow file deliverables", () => {
  it("opens a single root file result directly in preview without JSON report fields", () => {
    render(<NodeResultFiles {...props} data={single} />);
    const viewer = screen.getByTestId("viewer");
    expect(viewer).toHaveAttribute("data-path", "report.html");
    expect(viewer).toHaveAttribute("data-root", "/workflow/run-1");
    expect(viewer).toHaveAttribute("data-chat", "worker-chat");
    expect(viewer).toHaveAttribute("data-view", "preview");
    expect(viewer).toHaveAttribute("data-readonly", "true");
  });
  it("selects actual named files and preserves each file's viewing mode", () => {
    const multiple: ResultResponse = {
      ...single,
      result: { report: "/workflow/run-1/report.html", notes: "notes.md" },
      files: [
        { ...single.files?.[0], field: "/report" },
        {
          id: "notes",
          field: "/notes",
          name: "notes.md",
          mediaType: "text/markdown",
          available: true,
        },
        {
          id: "missing",
          field: "/missing",
          name: "missing.pdf",
          available: false,
        },
      ],
    };
    render(<NodeResultFiles {...props} data={multiple} />);
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "report.html",
    );
    expect(screen.getByRole("button", { name: /^missing.pdf/ })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /^report.html/ }));
    fireEvent.click(screen.getByText("查看源码"));
    expect(screen.getByTestId("viewer")).toHaveAttribute("data-view", "code");
    fireEvent.click(screen.getByRole("button", { name: /^notes.md/ }));
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "notes.md",
    );
    fireEvent.click(screen.getByRole("button", { name: /^report.html/ }));
    expect(screen.getByTestId("viewer")).toHaveAttribute("data-view", "code");
  });
  it("views a file array containing images and documents in the same viewer", () => {
    const data: ResultResponse = {
      ...single,
      result: ["missing.png", "long-report.png", "report.html", "second.png"],
      files: [
        { id: "missing", field: "/0", name: "missing.png", available: false },
        {
          id: "long",
          field: "/1",
          name: "long-report.png",
          mediaType: "image/png",
          available: true,
        },
        { ...single.files?.[0], field: "/2" },
        {
          id: "second",
          field: "/3",
          name: "second.png",
          mediaType: "image/png",
          available: true,
        },
      ],
    };
    render(<NodeResultFiles {...props} data={data} />);
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "long-report.png",
    );
    fireEvent.click(screen.getByRole("button", { name: /^report.html/ }));
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "report.html",
    );
    fireEvent.click(screen.getByRole("button", { name: /^second.png/ }));
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "second.png",
    );
  });
  it("opens an explicitly selected file without dropping the other files", () => {
    render(
      <NodeResultFiles
        {...props}
        selectedFileId="notes"
        data={{
          ...single,
          result: { report: "report.html", notes: "notes.md" },
          files: [
            { ...single.files?.[0], field: "/report" },
            {
              id: "notes",
              field: "/notes",
              name: "notes.md",
              available: true,
            },
          ],
        }}
      />,
    );
    expect(screen.getByTestId("viewer")).toHaveAttribute(
      "data-path",
      "notes.md",
    );
    expect(screen.getByRole("button", { name: /^report.html/ })).toBeEnabled();
  });
  it("shows a file entry on the final message and opens the full output on click", () => {
    const onOpen = vi.fn();
    render(
      <NodeResultFiles {...props} compact data={single} onOpen={onOpen} />,
    );
    expect(screen.queryByTestId("viewer")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /缺陷 HTML 报告/ }));
    expect(onOpen).toHaveBeenCalledOnce();
  });
});
