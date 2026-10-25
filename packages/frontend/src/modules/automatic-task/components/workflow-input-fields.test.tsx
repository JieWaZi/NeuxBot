import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, beforeEach, expect, it } from "vitest";
import "@/shared/i18n";
import { WorkflowInputFields } from "./workflow-input-fields";
import { apiExamples } from "./task-api-examples";
import type { InputBindings } from "../model/workflow-inputs";

beforeEach(() => {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.scrollIntoView = () => undefined;
});
afterEach(() => {
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
  Reflect.deleteProperty(Element.prototype, "setPointerCapture");
  Reflect.deleteProperty(Element.prototype, "releasePointerCapture");
  Reflect.deleteProperty(Element.prototype, "scrollIntoView");
});

it("切换 API 来源时保留草稿中的固定值，提交 API 来源不带固定值", async () => {
  const user = userEvent.setup();
  let latest: InputBindings = {};
  function Form() {
    const [bindings, setBindings] = useState<InputBindings>({
      text: { source: "fixed", value: "保留此文本" },
    });
    latest = bindings;
    return (
      <WorkflowInputFields
        schema={{
          type: "object",
          required: ["text"],
          properties: { text: { type: "string", title: "示例文本" } },
        }}
        bindings={bindings}
        allowAPI
        onChange={setBindings}
      />
    );
  }
  render(<Form />);
  expect(screen.getByDisplayValue("保留此文本")).toBeInTheDocument();
  await user.click(
    screen.getByRole("combobox", { name: "示例文本的参数来源" }),
  );
  await user.click(screen.getByRole("option", { name: "API 传入" }));
  expect(latest.text).toEqual({ source: "api" });
  expect(screen.queryByDisplayValue("保留此文本")).not.toBeInTheDocument();
  expect(
    screen.getByText("调用时必须提供 args.text，无需在此填写。"),
  ).toBeInTheDocument();
  await user.click(
    screen.getByRole("combobox", { name: "示例文本的参数来源" }),
  );
  await user.click(screen.getByRole("option", { name: "固定值" }));
  expect(latest.text).toEqual({ source: "fixed", value: "保留此文本" });
});

it("工作流调用示例保留 JSON 类型，并对单引号正确编码", () => {
  const example = apiExamples("task", "http://[::1]:8431/api/v1", true, {
    example: {
      text: "O'Reilly",
      segments: ["一", "二"],
      count: 2,
      enabled: false,
    },
    exampleValid: true,
  });
  expect(example.endpoint).toContain("http://[::1]:8431/");
  expect(example.trigger).toContain(`"text":"O'\\''Reilly"`);
  expect(example.trigger).toContain('"segments":["一","二"]');
  expect(example.trigger).toContain('"count":2,"enabled":false');
  expect(example.trigger).not.toContain('"input"');
});
