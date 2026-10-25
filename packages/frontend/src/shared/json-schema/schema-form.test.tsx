import { catalogDigests } from "@/shared/presentation/catalog-digests.generated";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import {
  SchemaForm,
  schemaDefaults,
  useSchemaFormValidity,
} from "./schema-form";

describe("workflow schema inputs", () => {
  it("keeps compact field titles, help, examples and actual defaults distinct", () => {
    const schema = {
      type: "object",
      properties: {
        context: {
          type: "string",
          title: "会议背景",
          description: "说明参会角色和会议目标，可留空。",
          examples: ["产品团队每周进度讨论"],
        },
        minutes: {
          type: "integer",
          title: "会议时长",
          description: "单位为分钟，范围 15 至 180。",
          default: 60,
          examples: [45],
        },
      },
    };
    function Form() {
      const [value, setValue] = useState<unknown>(schemaDefaults(schema));
      return (
        <>
          <SchemaForm
            compact
            schema={schema}
            value={value}
            onChange={setValue}
          />
          <output>{JSON.stringify(value)}</output>
        </>
      );
    }
    render(<Form />);
    const background = screen.getByRole("textbox", { name: "会议背景" });
    expect(background).toHaveValue("");
    expect(background).toHaveAttribute(
      "placeholder",
      "例如：产品团队每周进度讨论",
    );
    expect(background).toHaveAccessibleDescription(
      "说明参会角色和会议目标，可留空。",
    );
    expect(
      screen.getAllByText("说明参会角色和会议目标，可留空。"),
    ).toHaveLength(1);
    const minutes = screen.getByRole("spinbutton", { name: "会议时长" });
    expect(minutes).toHaveValue(60);
    expect(minutes).toHaveAttribute("placeholder", "例如：45");
    expect(screen.getByText("默认值")).toBeInTheDocument();
    expect(JSON.parse(screen.getByRole("status").textContent ?? "{}")).toEqual({
      minutes: 60,
    });
    fireEvent.change(minutes, { target: { value: "90" } });
    expect(screen.queryByText("默认值")).not.toBeInTheDocument();
    expect(JSON.parse(screen.getByRole("status").textContent ?? "{}")).toEqual({
      minutes: 90,
    });
  });
  it("submits typed values from input, radio, multiple choices, switch and textarea", () => {
    function Form() {
      const [value, setValue] = useState<unknown>({});
      return (
        <>
          <SchemaForm
            schema={{
              type: "object",
              required: ["name", "choice", "topics", "enabled", "notes"],
              properties: {
                name: {
                  type: "string",
                  title: "Name",
                  "x-ui-control": "input",
                },
                choice: {
                  type: "string",
                  title: "Choice",
                  enum: ["a", "b"],
                  "x-ui-control": "radio",
                  "x-ui-labels": ["First", "Second"],
                },
                topics: {
                  type: "array",
                  title: "Topics",
                  items: { type: "string", enum: ["agenda", "materials"] },
                  "x-ui-control": "checkboxes",
                },
                enabled: {
                  type: "boolean",
                  title: "Enabled",
                  "x-ui-control": "switch",
                },
                notes: {
                  type: "string",
                  title: "Notes",
                  "x-ui-control": "textarea",
                },
              },
            }}
            value={value}
            onChange={setValue}
          />
          <output>{JSON.stringify(value)}</output>
        </>
      );
    }
    render(<Form />);
    fireEvent.change(screen.getByLabelText(/Name/), {
      target: { value: "Weekly meeting" },
    });
    fireEvent.click(screen.getByRole("radio", { name: "Second" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "agenda" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "materials" }));
    fireEvent.click(screen.getByRole("switch", { name: "Enabled" }));
    expect(screen.getByLabelText(/Notes/).tagName).toBe("TEXTAREA");
    fireEvent.change(screen.getByLabelText(/Notes/), {
      target: { value: "Line one\nLine two" },
    });
    expect(JSON.parse(screen.getByRole("status").textContent ?? "{}")).toEqual({
      name: "Weekly meeting",
      choice: "b",
      topics: ["agenda", "materials"],
      enabled: true,
      notes: "Line one\nLine two",
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "agenda" }));
    expect(
      JSON.parse(screen.getByRole("status").textContent ?? "{}"),
    ).toMatchObject({ topics: ["materials"] });
  });
  it("keeps the source declaration order after JSON metadata serialization", () => {
    render(
      <SchemaForm
        schema={{
          type: "object",
          properties: {
            a: { type: "string", title: "First alphabetically" },
            z: { type: "string", title: "First in source" },
          },
          required: ["z", "a"],
        }}
        fieldOrder={{ "": ["z", "a"] }}
        value={{}}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getAllByRole("textbox").map((input) => input.getAttribute("id")),
    ).toEqual([
      screen.getByLabelText(/First in source/).id,
      screen.getByLabelText(/First alphabetically/).id,
    ]);
  });
  it("leaves omitted parents absent and keeps explicit null defaults", () => {
    expect(
      schemaDefaults({
        type: "object",
        properties: {
          nested: { type: "object", properties: { count: { default: 3 } } },
          nullable: { default: null },
        },
      }),
    ).toEqual({ nullable: null });
  });
  it("blocks stale submission while invalid JSON is being edited and accepts an external reset", () => {
    function Form() {
      const [value, setValue] = useState<unknown>({ initial: true });
      const { valid, onValidityChange } = useSchemaFormValidity();
      return (
        <>
          <SchemaForm
            schema={{ title: "JSON input" }}
            value={value}
            onChange={setValue}
            onValidityChange={onValidityChange}
          />
          <button disabled={!valid}>Submit input</button>
          <button onClick={() => setValue({ reset: true })}>Reset input</button>
          <output>{JSON.stringify(value)}</output>
        </>
      );
    }
    render(<Form />);
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "{" } });
    expect(screen.getByRole("button", { name: "Submit input" })).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent('{"initial":true}');
    fireEvent.click(screen.getByRole("button", { name: "Reset input" }));
    expect(screen.getByRole("textbox")).toHaveValue('{\n  "reset": true\n}');
    expect(screen.getByRole("button", { name: "Submit input" })).toBeEnabled();
  });
  it("shows inline pattern errors, blocks submission and clears the error after correction", () => {
    function Form() {
      const [value, setValue] = useState<unknown>({ client: "" });
      const { valid, onValidityChange } = useSchemaFormValidity();
      return (
        <form>
          <SchemaForm
            schema={{
              type: "object",
              required: ["client"],
              properties: {
                client: {
                  type: "string",
                  title: "Client ID",
                  description: "填写八位数字 ID。",
                  pattern: "^[0-9]{8}$",
                  "x-ui-control": "input",
                },
              },
            }}
            value={value}
            onChange={setValue}
            onValidityChange={onValidityChange}
          />
          <button disabled={!valid}>保存参数</button>
        </form>
      );
    }
    render(<Form />);
    const input = screen.getByRole("textbox", { name: "Client ID" });
    fireEvent.change(input, { target: { value: "xx" } });
    expect(screen.getByRole("alert")).toHaveTextContent("格式不符合要求");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription(
      "填写八位数字 ID。 格式不符合要求，请参考字段说明。",
    );
    expect(input).not.toHaveAttribute("pattern");
    expect(input).toBeInstanceOf(HTMLInputElement);
    if (!(input instanceof HTMLInputElement)) throw new Error("Expected input");
    expect(input.checkValidity()).toBe(true);
    expect(screen.getByRole("button", { name: "保存参数" })).toBeDisabled();
    fireEvent.change(input, { target: { value: "12345678" } });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "false");
    expect(screen.getByRole("button", { name: "保存参数" })).toBeEnabled();
  });
  it("uses JSON Schema substring matching for the PingCode nonblank credential rule", () => {
    function Form() {
      const [value, setValue] = useState<unknown>({ client: "cccc" });
      const { valid, onValidityChange } = useSchemaFormValidity();
      return (
        <>
          <SchemaForm
            compact
            schema={{
              type: "object",
              required: ["client"],
              properties: {
                client: {
                  type: "string",
                  title: "Client ID",
                  pattern: "\\S",
                  minLength: 1,
                  "x-ui-control": "input",
                },
              },
            }}
            value={value}
            onChange={setValue}
            onValidityChange={onValidityChange}
          />
          <button disabled={!valid}>保存凭据</button>
        </>
      );
    }
    render(<Form />);
    const input = screen.getByRole("textbox", { name: "Client ID" });
    expect(screen.getByRole("button", { name: "保存凭据" })).toBeEnabled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.change(input, { target: { value: "   " } });
    expect(screen.getByRole("button", { name: "保存凭据" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("格式不符合要求");
    fireEvent.change(input, { target: { value: "cc" } });
    expect(screen.getByRole("button", { name: "保存凭据" })).toBeEnabled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
  it("validates textarea length and integer bounds without native validation bubbles", () => {
    function Form() {
      const [value, setValue] = useState<unknown>({ notes: "abcd", count: 2 });
      const { onValidityChange } = useSchemaFormValidity();
      return (
        <SchemaForm
          schema={{
            type: "object",
            properties: {
              notes: {
                type: "string",
                title: "Notes",
                minLength: 3,
                maxLength: 5,
                "x-ui-control": "textarea",
              },
              count: {
                type: "integer",
                title: "Count",
                minimum: 1,
                maximum: 3,
              },
            },
          }}
          value={value}
          onChange={setValue}
          onValidityChange={onValidityChange}
        />
      );
    }
    render(<Form />);
    const notes = screen.getByRole("textbox", { name: "Notes" });
    fireEvent.change(notes, { target: { value: "ab" } });
    expect(screen.getByRole("alert")).toHaveTextContent("至少填写 3 个字符");
    fireEvent.change(notes, { target: { value: "abcdef" } });
    expect(screen.getByRole("alert")).toHaveTextContent("最多填写 5 个字符");
    fireEvent.change(notes, { target: { value: "abc" } });
    const count = screen.getByRole("spinbutton", { name: "Count" });
    fireEvent.change(count, { target: { value: "1.5" } });
    expect(screen.getByRole("alert")).toHaveTextContent("整数字段不能包含小数");
    fireEvent.change(count, { target: { value: "0" } });
    expect(screen.getByRole("alert")).toHaveTextContent("数值不能小于 1");
    fireEvent.change(count, { target: { value: "4" } });
    expect(screen.getByRole("alert")).toHaveTextContent("数值不能大于 3");
    fireEvent.change(count, { target: { value: "2" } });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
  it("renders fields across object allOf and local references", () => {
    render(
      <SchemaForm
        schema={{
          $defs: {
            detail: {
              type: "object",
              properties: { name: { type: "string", title: "Name" } },
              required: ["name"],
            },
          },
          allOf: [
            { $ref: "#/$defs/detail" },
            {
              type: "object",
              properties: { count: { type: "integer", title: "Count" } },
              required: ["count"],
            },
          ],
        }}
        value={{}}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByLabelText(/Name/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Count/)).toHaveAttribute("type", "number");
  });
});

it("renders an agent layout with schema-owned fields and preserves typed draft values", () => {
  const schema = {
    type: "object",
    properties: {
      "a/b~c": { type: "integer", title: "计数" },
      enabled: { type: "boolean", title: "开关", "x-ui": { widget: "switch" } },
    },
  };
  function Example() {
    const [value, setValue] = useState<unknown>({ "a/b~c": 0, enabled: false });
    return (
      <>
        <SchemaForm
          schema={schema}
          value={value}
          onChange={setValue}
          inputPage={{
            formatVersion: 1,
            catalogVersion: "workflow-input-v1",
            catalogDigest: catalogDigests["workflow-input-v1"],
            spec: {
              root: "grid",
              elements: {
                grid: {
                  type: "Grid",
                  props: { columns: 2 },
                  children: ["enabled", "count"],
                },
                enabled: { type: "Field", props: { path: "/args/enabled" } },
                count: { type: "Field", props: { path: "/args/a~1b~0c" } },
              },
            },
          }}
        />
        <output>{JSON.stringify(value)}</output>
      </>
    );
  }
  render(<Example />);
  expect(screen.getByRole("spinbutton", { name: "计数" })).toHaveValue(0);
  fireEvent.change(screen.getByRole("spinbutton", { name: "计数" }), {
    target: { value: "2" },
  });
  expect(screen.getByRole("status")).toHaveTextContent('"a/b~c":2');
  expect(screen.getByRole("status")).toHaveTextContent('"enabled":false');
});

it("falls back to schema-owned fields when an input catalog contract is unavailable", () => {
  render(
    <SchemaForm
      schema={{
        type: "object",
        properties: { title: { type: "string", title: "标题" } },
      }}
      value={{ title: "保留参数" }}
      onChange={vi.fn()}
      inputPage={{
        formatVersion: 1,
        catalogVersion: "workflow-input-v1",
        catalogDigest: "old-contract",
        spec: { root: "unavailable", elements: {} },
      }}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("已使用默认表单");
  expect(screen.getByRole("textbox", { name: "标题" })).toHaveValue("保留参数");
});
