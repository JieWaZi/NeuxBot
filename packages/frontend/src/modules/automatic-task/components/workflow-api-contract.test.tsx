import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import "@/shared/i18n";
import { WorkflowAPIContract } from "./workflow-api-contract";

it("展示并复制完整请求体，说明替换业务值与请求标识", async () => {
  const user = userEvent.setup();
  render(
    <WorkflowAPIContract
      contract={{
        fields: [
          {
            key: "work_item_id",
            title: "工作项 ID",
            type: "string",
            source: "api",
            required: true,
          },
          {
            key: "segments",
            title: "片段列表",
            type: "array<string>",
            source: "api",
            required: false,
          },
        ],
        schema: {
          type: "object",
          properties: {
            work_item_id: { type: "string" },
            segments: { type: "array", items: { type: "string" } },
          },
        },
        example: { work_item_id: "example_id", segments: ["a", "b"] },
        exampleValid: true,
      }}
    />,
  );
  expect(screen.getByText(/替换为实际业务值/)).toBeVisible();
  expect(screen.getByText(/UNIQUE_REQUEST_KEY/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "复制 请求参数示例" }));
  expect(JSON.parse(await navigator.clipboard.readText())).toEqual({
    args: { work_item_id: "example_id", segments: ["a", "b"] },
  });
});

it("复制的 API Schema 隐藏 UI 注解，保留业务同名字段和默认数据", async () => {
  const user = userEvent.setup();
  const schema = {
    type: "object",
    "x-ui-control": "input",
    properties: {
      work_item_id: {
        type: "string",
        pattern: "^[A-Za-z0-9_-]+$",
        "x-ui-control": "input",
      },
      payload: {
        type: "object",
        default: { "x-ui-control": "business value" },
        properties: {
          "x-ui-control": { type: "string", "x-ui-control": "textarea" },
        },
      },
    },
    $defs: { text: { type: "string", "x-ui-labels": ["label"] } },
  };
  render(<WorkflowAPIContract contract={{ schema }} />);
  await user.click(
    screen.getByText(/参数 JSON Schema/, { selector: "summary" }),
  );
  await user.click(
    screen.getByRole("button", { name: "复制 参数 JSON Schema" }),
  );
  expect(JSON.parse(await navigator.clipboard.readText())).toEqual({
    type: "object",
    properties: {
      work_item_id: { type: "string", pattern: "^[A-Za-z0-9_-]+$" },
      payload: {
        type: "object",
        default: { "x-ui-control": "business value" },
        properties: { "x-ui-control": { type: "string" } },
      },
    },
    $defs: { text: { type: "string" } },
  });
  expect(schema.properties.work_item_id["x-ui-control"]).toBe("input");
});
