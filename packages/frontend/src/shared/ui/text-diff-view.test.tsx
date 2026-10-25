import { render } from "@testing-library/react";
import { afterAll, beforeAll, expect, it } from "vitest";
import { TextDiffView } from "./text-diff-view";

const canvasContext = Object.getOwnPropertyDescriptor(
  HTMLCanvasElement.prototype,
  "getContext",
);
beforeAll(() =>
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    value: () => ({
      measureText: (text: string) => ({ width: text.length * 8 }),
    }),
  }),
);
afterAll(() => {
  if (canvasContext)
    Object.defineProperty(
      HTMLCanvasElement.prototype,
      "getContext",
      canvasContext,
    );
});

it.each(["light", "dark"] as const)("%s 主题展示真实新增与删除行", (theme) => {
  const { container } = render(
    <TextDiffView
      beforeContent="旧正文"
      afterContent="新正文"
      beforeLabel="版本 1"
      afterLabel="版本 2"
      language="markdown"
      theme={theme}
    />,
  );
  expect(
    container.querySelector('[data-line-old-num="1"]')?.closest("tr"),
  ).toHaveTextContent("旧正文");
  expect(
    container.querySelector('[data-line-new-num="1"]')?.closest("tr"),
  ).toHaveTextContent("新正文");
});
