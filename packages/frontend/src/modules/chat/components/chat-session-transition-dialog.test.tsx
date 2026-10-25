import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatSessionTransitionDialog } from "./chat-session-transition-dialog";

beforeAll(() => {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
});

afterAll(() => {
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
  Reflect.deleteProperty(Element.prototype, "setPointerCapture");
  Reflect.deleteProperty(Element.prototype, "releasePointerCapture");
});

describe("ChatSessionTransitionDialog", () => {
  it("可选择自动总结使用的模型和推理强度，并保留失败时的最近轮数", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <ChatSessionTransitionDialog
        open
        reason="switch"
        models={[
          {
            id: "large",
            name: "大模型",
            reasoningOptions: [{ id: "high", name: "高" }],
          },
          {
            id: "small",
            name: "小模型",
            reasoningOptions: [{ id: "low", name: "低" }],
          },
        ]}
        reasoningOptions={[]}
        defaultModel="large"
        defaultReasoning="high"
        onCancel={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    expect(screen.getByLabelText("携带最近对话轮数")).toHaveValue(5);
    await user.click(screen.getByRole("radio", { name: "自动总结" }));
    await user.click(screen.getByRole("combobox", { name: "总结模型" }));
    await user.click(screen.getByRole("option", { name: "小模型" }));
    expect(
      screen.getByRole("combobox", { name: "总结推理强度" }),
    ).toHaveTextContent("低");
    await user.clear(screen.getByLabelText("总结不可用时携带的最近轮数"));
    await user.type(screen.getByLabelText("总结不可用时携带的最近轮数"), "3");
    await user.click(screen.getByRole("button", { name: "确认切换并继续" }));

    expect(onConfirm).toHaveBeenCalledWith({
      turns: 3,
      mode: "summary",
      summaryModel: "small",
      summaryReasoning: "low",
    });
  });
});
