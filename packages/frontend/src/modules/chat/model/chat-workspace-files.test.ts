import { describe, expect, it } from "vitest";

import {
  workspaceFilesOf,
  workspaceFilesWithGenerated,
} from "./chat-workspace-files";

describe("chat workspace files", () => {
  it("collects unique message attachments in message order", () => {
    expect(
      workspaceFilesOf([
        {
          attachments: [
            {
              name: "brief.pdf",
              path: "attachments/message-1/brief.pdf",
              mediaType: "application/pdf",
            },
            { name: "duplicate", path: "attachments/message-1/brief.pdf" },
          ],
        },
        {
          attachments: [{ path: "attachments/message-2/notes.txt" }],
        },
      ]),
    ).toEqual([
      {
        name: "brief.pdf",
        path: "attachments/message-1/brief.pdf",
        source: "chat",
        mediaType: "application/pdf",
      },
      {
        name: "notes.txt",
        path: "attachments/message-2/notes.txt",
        source: "chat",
        mediaType: undefined,
      },
    ]);
  });

  it("merges generated files by relative path without replacing attachments", () => {
    const attachment = {
      name: "uploaded.md",
      path: "attachments/generated/report.md",
      source: "chat" as const,
      mediaType: "text/markdown",
    };

    expect(
      workspaceFilesWithGenerated(
        [attachment],
        [
          {
            name: "report.md",
            path: "/tmp/ally/attachments/generated/report.md",
            relativePath: "attachments/generated/report.md",
          },
          {
            name: "summary.txt",
            path: "/tmp/ally/attachments/generated/nested/summary.txt",
            relativePath: "attachments/generated/nested/summary.txt",
          },
        ],
      ),
    ).toEqual([
      attachment,
      {
        name: "summary.txt",
        path: "attachments/generated/nested/summary.txt",
        source: "chat",
      },
    ]);
  });

  it("已删除的历史附件和生成文件不会重新进入会话文件列表", () => {
    expect(
      workspaceFilesWithGenerated(
        [
          {
            name: "upload.txt",
            path: "attachments/message-1/upload.txt",
            source: "chat",
          },
        ],
        [
          {
            name: "report.md",
            path: "/tmp/ally/attachments/generated/report.md",
            relativePath: "attachments/generated/report.md",
          },
        ],
        ["attachments/message-1/upload.txt", "attachments/generated/report.md"],
      ),
    ).toEqual([]);
  });
});
