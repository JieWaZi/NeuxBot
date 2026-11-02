import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as Platform from "@/shared/platform";
import {
  emptyFileReferenceContext,
  type FileReferenceContext,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { AssistantMarkdown } from "./chat-message-content";

const { resolveFile, resolveResource } = vi.hoisted(() => ({
  resolveFile: vi.fn<typeof Platform.resolveFileReferencePath>(),
  resolveResource: vi.fn<typeof Platform.resolveWorkspaceResourceReference>(),
}));
vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof Platform>()),
  resolveFileReferencePath: resolveFile,
  resolveWorkspaceResourceReference: resolveResource,
}));

const clients: QueryClient[] = [];
function renderMarkdown(
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

describe("chat Markdown reference integration", () => {
  it.each([false, true])(
    "keeps implicit local inline code inert without resolving files (streaming=%s)",
    async (streaming) => {
      const values = [
        "3.14",
        "user.name",
        "@scope/pkg",
        "./report.md",
        "/workspace/report.md",
      ];
      const { container } = renderMarkdown(
        <AssistantMarkdown
          text={values.map((value) => `\`${value}\``).join("，")}
          isTerminal={!streaming}
        />,
        {
          context: {
            chatId: "chat-1",
            baseDirectory: "/workspace",
            workspaceRoot: "/workspace",
            sourceRoots: [],
          },
          onOpenFile: vi.fn(),
        },
      );
      expect(
        [...container.querySelectorAll("code")].map((code) => code.textContent),
      ).toEqual(values);
      for (const value of values) fireEvent.click(screen.getByText(value));
      await Promise.resolve();
      expect(screen.queryByRole("link")).not.toBeInTheDocument();
      expect(screen.queryByRole("button")).not.toBeInTheDocument();
      expect(resolveFile).not.toHaveBeenCalled();
      expect(resolveResource).not.toHaveBeenCalled();
    },
  );

  it("resolves explicit Markdown file links on activation and keeps inline HTTP links available", async () => {
    resolveResource.mockResolvedValueOnce({
      status: "resolved",
      canonicalPath: "/workspace/report.md",
      baseDirectory: "/workspace",
      workspaceRoot: "/workspace",
      sourceRoots: [],
    });
    const open = vi.fn();
    const openURL = vi.fn();
    renderMarkdown(
      <AssistantMarkdown text="[报告](./report.md#L4) 与 `https://example.com/report`" />,
      {
        context: {
          chatId: "chat-1",
          baseDirectory: "/workspace",
          workspaceRoot: "/workspace",
          sourceRoots: [],
        },
        onOpenFile: open,
        onOpenURL: openURL,
      },
    );
    expect(resolveFile).not.toHaveBeenCalled();
    expect(resolveResource).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("link", { name: "报告" }));
    await waitFor(() =>
      expect(open).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          canonicalPath: "/workspace/report.md",
          line: 4,
        }),
      ),
    );
    expect(resolveResource).toHaveBeenCalledExactlyOnceWith(
      "chat-1",
      "./report.md",
      "/workspace",
    );
    fireEvent.click(
      screen.getByRole("link", { name: "https://example.com/report" }),
    );
    expect(openURL).toHaveBeenCalledExactlyOnceWith(
      "https://example.com/report",
    );
    expect(resolveResource).toHaveBeenCalledTimes(1);
  });
});
