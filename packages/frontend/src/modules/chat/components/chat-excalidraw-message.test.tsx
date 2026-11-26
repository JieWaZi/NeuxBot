import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { AssistantMessage } from "./chat-thread";
import { ChatMessageProjection } from "./chat-message-nodes";
import type { BotChatMessage } from "../runtime/chat-projection";

const { saveBlob } = vi.hoisted(() => ({
  saveBlob: vi
    .fn<(blob: Blob, fileName: string) => Promise<boolean>>()
    .mockResolvedValue(true),
}));

vi.mock(import("@/shared/platform"), async (importOriginal) => ({
  ...(await importOriginal()),
  saveBlob,
}));

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

vi.mock("./chat-excalidraw-render", () => ({
  renderExcalidraw: async () => ({
    image:
      "data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%2F%3E",
    width: 200,
    height: 120,
    source: '{"type":"excalidraw","elements":[]}',
    createPNG: async () => new Blob(["pixels"], { type: "image/png" }),
  }),
}));

function renderReply(elements?: string) {
  const toolPart = {
    id: "draw",
    type: "tool",
    toolStatus: "completed",
    excalidraw: { elements },
  } as const;
  const message: BotChatMessage = {
    id: "reply",
    role: "assistant",
    status: "completed",
    displayParts: [toolPart, { id: "text", type: "text", text: "图已画好。" }],
  };
  const [view] = new ChatMessageProjection().update([message]);
  if (!view) throw new Error("missing assistant message");
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <AssistantMessage view={view} name="伙伴" last />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("Excalidraw in an assistant reply", () => {
  it("opens a restored graph outside the collapsed work process", async () => {
    renderReply(
      '[{"type":"rectangle","id":"dns","x":0,"y":0,"width":100,"height":40}]',
    );

    const preview = await screen.findByRole("button", {
      name: "打开 Excalidraw 大图",
    });
    expect(screen.getByText("图已画好。")).toBeVisible();
    const toolbar = preview.closest("section")?.querySelector("header");
    expect(toolbar).toBeTruthy();
    if (!toolbar) throw new Error("missing diagram toolbar");
    expect(
      within(toolbar).getByRole("button", { name: "下载 Excalidraw 图表" }),
    ).toBeVisible();
    fireEvent.click(within(toolbar).getByRole("button", { name: "全屏查看" }));

    expect(
      screen.getByRole("dialog", { name: "Excalidraw 图表" }),
    ).toBeVisible();
  });

  it("downloads PNG and an editable Excalidraw file from the toolbar", async () => {
    const user = userEvent.setup();
    renderReply(
      '[{"type":"rectangle","id":"dns","x":0,"y":0,"width":100,"height":40}]',
    );

    const download = await screen.findByRole("button", {
      name: "下载 Excalidraw 图表",
    });
    await user.click(download);
    await user.click(screen.getByRole("menuitem", { name: "PNG 图片" }));
    await waitFor(() =>
      expect(saveBlob).toHaveBeenCalledWith(
        expect.objectContaining({ type: "image/png" }),
        "diagram.png",
      ),
    );

    await user.click(download);
    await user.click(
      screen.getByRole("menuitem", { name: "Excalidraw 源文件" }),
    );
    await waitFor(() =>
      expect(saveBlob).toHaveBeenCalledWith(
        expect.objectContaining({ type: "application/json" }),
        "diagram.excalidraw",
      ),
    );
  });

  it("reports an unavailable scene outside the collapsed work process", () => {
    renderReply();

    expect(
      screen.getByText("这次调用未提供可预览的 Excalidraw 图表。"),
    ).toBeVisible();
  });
});
