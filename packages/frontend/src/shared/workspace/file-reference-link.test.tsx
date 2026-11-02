import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FileReferenceResolution } from "@neuxbot/platform-contracts/resources";
import {
  emptyFileReferenceContext,
  localFileURL,
  type FileReferenceCandidate,
  type FileReferenceContext,
  type ResolvedFileReference,
} from "./file-reference";
import { FileReferenceProvider, ReferenceLink } from "./file-reference-link";

const { openExternalURL, resolveFile, resolveResource, showError } = vi.hoisted(
  () => ({
    openExternalURL: vi.fn(async () => undefined),
    resolveFile: vi.fn<() => Promise<FileReferenceResolution>>(),
    resolveResource: vi.fn<() => Promise<FileReferenceResolution>>(),
    showError: vi.fn(),
  }),
);
vi.mock("@/shared/platform", () => ({
  openExternalURL,
  resolveFileReferencePath: resolveFile,
  resolveWorkspaceResourceReference: resolveResource,
}));
vi.mock("@/shared/ui/sonner", () => ({ toast: { error: showError } }));

const clients: QueryClient[] = [];
function renderReference(
  children: ReactNode,
  options: {
    context?: FileReferenceContext;
    onOpenFile?: (file: ResolvedFileReference) => void;
    onOpenURL?: (url: string) => unknown;
  } = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <FileReferenceProvider
        context={options.context ?? emptyFileReferenceContext}
        onOpenFile={options.onOpenFile}
        onOpenURL={options.onOpenURL}
      >
        {children}
      </FileReferenceProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => {
  for (const client of clients.splice(0)) client.clear();
});

describe("reference link activation", () => {
  it("delegates ordinary HTTP(S) clicks and preserves native modified and auxiliary gestures", () => {
    const open = vi.fn();
    const url = "https://example.com/report?q=完整目标#section";
    renderReference(
      <ReferenceLink reference={{ kind: "web", url }} title="作者说明">
        <strong>报告标签</strong>
      </ReferenceLink>,
      { onOpenURL: open },
    );
    const link = screen.getByRole("link", { name: "报告标签" });
    expect(link).toHaveAttribute("href", url);
    expect(link).toHaveAttribute("title", "作者说明");
    expect(fireEvent.click(link)).toBe(false);
    expect(open).toHaveBeenCalledExactlyOnceWith(url);
    for (const gesture of [
      { metaKey: true },
      { ctrlKey: true },
      { shiftKey: true },
      { altKey: true },
      { button: 1 },
      { button: 2 },
    ])
      expect(fireEvent.click(link, gesture)).toBe(true);
    expect(
      fireEvent(
        link,
        new MouseEvent("auxclick", {
          button: 1,
          bubbles: true,
          cancelable: true,
        }),
      ),
    ).toBe(true);
    expect(fireEvent.contextMenu(link)).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
    expect(openExternalURL).not.toHaveBeenCalled();
    expect(resolveFile).not.toHaveBeenCalled();
    expect(resolveResource).not.toHaveBeenCalled();
  });

  it("uses the platform delegate when no owner callback exists and respects prevented activation", () => {
    const url = "http://example.com/plain";
    renderReference(
      <>
        <ReferenceLink reference={{ kind: "web", url }} title="">
          普通网页
        </ReferenceLink>
        <div onClickCapture={(event) => event.preventDefault()}>
          <ReferenceLink reference={{ kind: "web", url }}>
            已处理网页
          </ReferenceLink>
        </div>
      </>,
    );
    expect(screen.getByRole("link", { name: "普通网页" })).toHaveAttribute(
      "title",
      "",
    );
    expect(
      fireEvent.click(screen.getByRole("link", { name: "普通网页" })),
    ).toBe(false);
    expect(openExternalURL).toHaveBeenCalledExactlyOnceWith(url);
    expect(
      fireEvent.click(screen.getByRole("link", { name: "已处理网页" })),
    ).toBe(false);
    expect(openExternalURL).toHaveBeenCalledTimes(1);
  });

  it("resolves local targets only on activation and preserves the author label, title and full path", async () => {
    const user = userEvent.setup();
    const clipboard = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    const path = `/authorized/${"完整目录/".repeat(30)}report with spaces.md`;
    resolveResource.mockResolvedValueOnce({
      status: "resolved",
      canonicalPath: path,
      baseDirectory: "/workspace",
      workspaceRoot: "/workspace",
      sourceRoots: ["/authorized"],
    });
    const open = vi.fn();
    const reference: FileReferenceCandidate = {
      kind: "file",
      path: "projects/report with spaces.md",
      raw: "projects/report with spaces.md#L17-L19",
      line: 17,
      endLine: 19,
    };
    renderReference(
      <ReferenceLink reference={reference} title="作者的文件说明">
        <strong>保留标签</strong>
      </ReferenceLink>,
      {
        context: {
          chatId: "chat-1",
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: ["/authorized"],
        },
        onOpenFile: open,
      },
    );
    const link = screen.getByRole("link", { name: "保留标签" });
    expect(resolveResource).not.toHaveBeenCalled();
    await user.click(link);
    await waitFor(() =>
      expect(open).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ canonicalPath: path, line: 17, endLine: 19 }),
      ),
    );
    expect(resolveResource).toHaveBeenCalledExactlyOnceWith(
      "chat-1",
      reference.path,
      "/workspace",
    );
    expect(link).toHaveAttribute("href", localFileURL(path));
    expect(link).toHaveAttribute("data-file-path", path);
    expect(link).toHaveAttribute("title", "作者的文件说明");
    expect(link.textContent).toBe("保留标签");
    expect(link.querySelector("strong")).toHaveTextContent("保留标签");
    await user.click(screen.getByRole("button", { name: /复制完整路径/ }));
    expect(clipboard).toHaveBeenCalledExactlyOnceWith(path);
    expect(resolveFile).not.toHaveBeenCalled();
    clipboard.mockRestore();
  });

  it("blocks native local file gestures without bypassing authorization or resolving eagerly", () => {
    const open = vi.fn();
    renderReference(
      <ReferenceLink
        reference={{
          kind: "file",
          path: "/private/file.txt",
          raw: "/private/file.txt",
        }}
      >
        文件
      </ReferenceLink>,
      {
        context: {
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: [],
        },
        onOpenFile: open,
      },
    );
    const link = screen.getByRole("link", { name: "文件" });
    for (const gesture of [
      { metaKey: true },
      { ctrlKey: true },
      { shiftKey: true },
      { altKey: true },
    ])
      expect(fireEvent.click(link, gesture)).toBe(false);
    for (const button of [1, 2])
      expect(
        fireEvent(
          link,
          new MouseEvent("auxclick", {
            button,
            bubbles: true,
            cancelable: true,
          }),
        ),
      ).toBe(false);
    expect(fireEvent.contextMenu(link)).toBe(true);
    expect(resolveFile).not.toHaveBeenCalled();
    expect(resolveResource).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    expect(openExternalURL).not.toHaveBeenCalled();
  });

  it("keeps denied local references readable without opening a system URL", async () => {
    resolveFile.mockResolvedValueOnce({ status: "outside_scope" });
    const open = vi.fn();
    renderReference(
      <ReferenceLink
        reference={{
          kind: "file",
          path: "/outside/file",
          raw: "/outside/file",
        }}
      >
        原始标签
      </ReferenceLink>,
      {
        context: {
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: [],
        },
        onOpenFile: open,
      },
    );
    expect(
      fireEvent.click(screen.getByRole("link", { name: "原始标签" })),
    ).toBe(false);
    await waitFor(() => expect(showError).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("link", { name: "原始标签" })).toHaveTextContent(
      "原始标签",
    );
    expect(open).not.toHaveBeenCalled();
    expect(openExternalURL).not.toHaveBeenCalled();
  });
});
