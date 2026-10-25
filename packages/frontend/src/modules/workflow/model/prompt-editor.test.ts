import { expect, it } from "vitest";
import { editablePrompt } from "./prompt-editor";

it("拼接 Prompt 保留输入表达式，界面没有 JavaScript 外层引号", () => {
  expect(editablePrompt('"材料：\\n" + args.material')).toBe(
    "材料：\n${args.material}",
  );
  expect(editablePrompt('args.material || "空"')).toBe(
    '${args.material || "空"}',
  );
});
it("纯文字中的插值字符和路径反斜杠不会变成新的表达式", () => {
  expect(editablePrompt(JSON.stringify("C:\\draft ${literal}"))).toBe(
    "C:\\\\draft \\${literal}",
  );
  expect(editablePrompt("`复核 ${args.material}`")).toBe(
    "复核 ${args.material}",
  );
});
