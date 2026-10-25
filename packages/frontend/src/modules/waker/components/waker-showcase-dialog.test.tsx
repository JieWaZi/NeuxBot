import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerShowcaseDialog } from "./waker-showcase-dialog";

vi.mock("../api/waker-queries", () => ({
  listWakerConnectorsOptions: ({ path }: { path: { wakerId: string } }) => ({
    queryFn: async () => [
      { connector: { id: `${path.wakerId}-c1` } },
      { connector: { id: `${path.wakerId}-c2` } },
    ],
    queryKey: ["waker-connectors", path.wakerId],
  }),
  listWakerSkillsOptions: ({ path }: { path: { wakerId: string } }) => ({
    queryFn: async () => [{ skill: { id: `${path.wakerId}-s1` } }],
    queryKey: ["waker-skills", path.wakerId],
  }),
}));

function renderDialog(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}

describe("WakerShowcaseDialog", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("收起同步淡出遮罩，结束后不再切回展开阶段", async () => {
    const onOpenChange = vi.fn();
    renderDialog(
      <WakerShowcaseDialog
        open
        waker={{ id: "waker-1", name: "林可" }}
        onOpenChange={onOpenChange}
      />,
    );
    await screen.findByText("1 个技能 · 2 个连接器");
    const card = screen.getByRole("article");
    const flight = card.querySelector(".ally-holo-card__translater");
    if (!flight) throw new Error("卡片缺少飞行动画层");
    const finishFlight = () =>
      fireEvent(flight, new Event("webkitAnimationEnd", { bubbles: true }));
    finishFlight();
    expect(card).toHaveAttribute("data-phase", "open");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(card).toHaveAttribute("data-phase", "closing");
    expect(document.querySelector('[data-motion="overlay"]')).toHaveAttribute(
      "data-closing",
      "true",
    );
    finishFlight();
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(card).not.toHaveAttribute("data-phase", "opening");
  });

  it("新建会话传递当前伙伴并关闭卡片，不直接发送消息", async () => {
    const onOpenChange = vi.fn();
    const onStartChat = vi.fn();
    renderDialog(
      <WakerShowcaseDialog
        open
        waker={{ id: "waker-1", name: "林可" }}
        onOpenChange={onOpenChange}
        onStartChat={onStartChat}
      />,
    );
    await screen.findByText("1 个技能 · 2 个连接器");
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    expect(onStartChat).toHaveBeenCalledExactlyOnceWith("waker-1");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("详情位于新建会话前，打开当前伙伴详情并关闭卡片", async () => {
    const onOpenChange = vi.fn();
    const onOpenDetail = vi.fn();
    const onStartChat = vi.fn();
    renderDialog(
      <WakerShowcaseDialog
        open
        waker={{ id: "waker-1", name: "林可" }}
        onOpenChange={onOpenChange}
        onOpenDetail={onOpenDetail}
        onStartChat={onStartChat}
      />,
    );
    await screen.findByText("1 个技能 · 2 个连接器");
    const detail = screen.getByRole("button", { name: "详情" });
    const newChat = screen.getByRole("button", { name: "新建会话" });
    expect(
      detail.compareDocumentPosition(newChat) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.click(detail);
    expect(onOpenDetail).toHaveBeenCalledExactlyOnceWith("waker-1");
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onStartChat).not.toHaveBeenCalled();
  });

  it("打开后统计当前伙伴的技能和连接器", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: query.includes("prefers-reduced-motion"),
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
      onchange: null,
    }));
    renderDialog(
      <WakerShowcaseDialog
        open
        waker={{
          expertise: [{ title: "代码审查" }],
          id: "waker-1",
          name: "林可",
        }}
        onOpenChange={() => undefined}
      />,
    );

    expect(screen.getByRole("heading", { name: "林可 的卡片" })).toHaveClass(
      "sr-only",
    );
    expect(screen.getByText("代码审查")).toBeInTheDocument();
    expect(screen.getByRole("article")).toHaveAttribute("data-phase", "open");
    await waitFor(() =>
      expect(screen.getByText("1 个技能 · 2 个连接器")).toBeInTheDocument(),
    );
  });
});
