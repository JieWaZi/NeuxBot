import { describe, expect, it } from "vitest";

import { chatPrimaryAction } from "./chat-primary-action";

describe("chatPrimaryAction", () => {
  it.each([
    {
      isRunning: false,
      isWorkRunning: false,
      isComposerEmpty: true,
      expected: "send",
    },
    {
      isRunning: true,
      isWorkRunning: false,
      isComposerEmpty: true,
      expected: "stop",
    },
    {
      isRunning: true,
      isWorkRunning: false,
      isComposerEmpty: false,
      expected: "queue",
    },
    {
      isRunning: false,
      isWorkRunning: true,
      isComposerEmpty: true,
      expected: "stop",
    },
    {
      isRunning: false,
      isWorkRunning: true,
      isComposerEmpty: false,
      expected: "queue",
    },
  ])("由权威运行态得到唯一 $expected 操作", ({ expected, ...state }) => {
    expect(chatPrimaryAction(state)).toBe(expected);
  });
});
