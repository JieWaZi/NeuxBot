import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatElicitationCard } from "./chat-elicitation-card";

const question = "你想选哪种水果？";
const options = [
  { const: "apple", title: "苹果", description: "清甜爽脆" },
  { const: "banana", title: "香蕉", description: "软糯饱腹" },
];

function request(multiple = false, customKey = "fruit__other") {
  return {
    elicitationId: "fruit-question",
    mode: "form",
    message: question,
    requestedSchema: {
      type: "object",
      properties: {
        fruit: {
          title: "选择水果",
          description: question,
          ...(multiple
            ? { type: "array", items: { anyOf: options } }
            : { type: "string", oneOf: options }),
        },
        [customKey]: { type: "string" },
      },
    },
  };
}

describe("ChatElicitationCard", () => {
  it("提交单选枚举的原始值，不改变前后空白", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    render(
      <ChatElicitationCard
        elicitation={{
          elicitationId: "raw-choice",
          mode: "form",
          message: "选择",
          allowSkip: false,
          requestedSchema: {
            type: "object",
            properties: { choice: { type: "string", enum: [" file "] } },
            required: ["choice"],
          },
        }}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );
    await user.click(screen.getByRole("button", { name: "file" }));
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(onResolve).toHaveBeenCalledWith(
      "raw-choice",
      { choice: " file " },
      false,
    );
  });

  it.each([false, true])(
    "提交 CLI 的 enum 选项并隐藏不可跳过入口，多选=%s",
    async (multiple) => {
      const user = userEvent.setup();
      const onResolve = vi.fn();
      render(
        <ChatElicitationCard
          elicitation={{
            elicitationId: "cli-choice",
            mode: "form",
            message: "选择交付方式",
            allowSkip: false,
            requestedSchema: {
              type: "object",
              properties: {
                delivery: multiple
                  ? {
                      type: "array",
                      items: { type: "string", enum: ["文件", "正文"] },
                    }
                  : { type: "string", enum: ["文件", "正文"] },
              },
              required: ["delivery"],
            },
          }}
          submissionState="idle"
          onResolve={onResolve}
        />,
      );
      expect(
        screen.queryByRole("button", { name: "跳过" }),
      ).not.toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "文件" }));
      await user.click(screen.getByRole("button", { name: "继续" }));
      expect(onResolve).toHaveBeenCalledWith(
        "cli-choice",
        { delivery: multiple ? ["文件"] : "文件" },
        false,
      );
    },
  );

  it("题目只出现一次，切回普通选项时清除隐藏的自定义答案", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    render(
      <ChatElicitationCard
        elicitation={request()}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );
    expect(screen.getAllByText(question)).toHaveLength(1);
    expect(screen.getByText("清甜爽脆")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "其他…" }));
    await user.type(screen.getByRole("textbox", { name: "其他…" }), "芒果");
    await user.click(screen.getByRole("button", { name: /苹果/ }));
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /苹果/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(onResolve).toHaveBeenCalledWith(
      "fruit-question",
      { fruit: "apple" },
      false,
    );
  });

  it("单选的其他答案替换原选项，Enter 提交不会触发外层聊天表单", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    const onChatSubmit = vi.fn((event) => event.preventDefault());
    render(
      <form onSubmit={onChatSubmit}>
        <ChatElicitationCard
          elicitation={request()}
          submissionState="idle"
          onResolve={onResolve}
        />
      </form>,
    );
    await user.click(screen.getByRole("button", { name: /苹果/ }));
    await user.click(screen.getByRole("button", { name: "其他…" }));
    const input = screen.getByRole("textbox", { name: "其他…" });
    await user.type(input, " 芒果 ");
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    expect(onResolve).not.toHaveBeenCalled();
    await user.keyboard("{Enter}");
    expect(onResolve).toHaveBeenCalledWith(
      "fruit-question",
      { fruit__other: "芒果" },
      false,
    );
    expect(onChatSubmit).not.toHaveBeenCalled();
  });

  it("多选保留多个选择和补充答案，取消其他项后不提交旧补充", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    render(
      <ChatElicitationCard
        elicitation={request(true, "fruit_custom")}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );
    await user.click(screen.getByRole("button", { name: /苹果/ }));
    await user.click(screen.getByRole("button", { name: /香蕉/ }));
    await user.click(screen.getByRole("button", { name: "其他…" }));
    await user.type(screen.getByRole("textbox", { name: "其他…" }), "梨");
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(onResolve).toHaveBeenLastCalledWith(
      "fruit-question",
      { fruit: ["apple", "banana"], fruit_custom: "梨" },
      false,
    );
    await user.click(screen.getByRole("button", { name: "其他…" }));
    await user.click(screen.getByRole("button", { name: /香蕉/ }));
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(onResolve).toHaveBeenLastCalledWith(
      "fruit-question",
      { fruit: ["apple"] },
      false,
    );
  });

  it("等待提交确认时禁用全部输入和操作，跳过沿用拒绝协议", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    const { rerender } = render(
      <ChatElicitationCard
        elicitation={request()}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );
    await user.click(screen.getByRole("button", { name: "跳过" }));
    expect(onResolve).toHaveBeenCalledWith("fruit-question", {}, true);
    rerender(
      <ChatElicitationCard
        elicitation={request()}
        submissionState="confirming"
        onResolve={onResolve}
      />,
    );
    for (const button of screen.getAllByRole("button"))
      expect(button).toBeDisabled();
  });
});
