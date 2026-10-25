import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ComponentProps } from "react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ExecutionModelControl } from "./execution-model-control";

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
beforeAll(() => vi.stubGlobal("ResizeObserver", ResizeObserverStub));
afterAll(() => vi.unstubAllGlobals());

const models = [
  { id: "model-a", name: "模型 A" },
  { id: "model-b", name: "模型 B" },
];
const reasoningOptions = [
  { id: "low", name: "低" },
  { id: "medium", name: "中" },
  { id: "high", name: "高" },
];

function renderPicker(
  overrides: Partial<ComponentProps<typeof ExecutionModelControl>> = {},
) {
  const onModelChange = vi.fn();
  const onReasoningChange = vi.fn();
  function Picker() {
    const [model, setModel] = useState(overrides.selectedModel ?? "model-a");
    const [reasoning, setReasoning] = useState(
      overrides.selectedReasoning ?? "high",
    );
    return (
      <ExecutionModelControl
        models={models}
        reasoningOptions={reasoningOptions}
        defaultReasoning="medium"
        {...overrides}
        selectedModel={model}
        selectedModelName={
          (overrides.models ?? models).find((item) => item.id === model)
            ?.name ?? (overrides.allowDefaultModel ? "默认模型" : "")
        }
        selectedReasoning={reasoning}
        onModelChange={(value) => {
          onModelChange(value);
          setModel(value);
        }}
        onReasoningChange={(value) => {
          onReasoningChange(value);
          setReasoning(value);
        }}
      />
    );
  }
  const result = render(<Picker />);
  return { ...result, onModelChange, onReasoningChange };
}

describe("执行模型与强度选择", () => {
  it.each([10, 11])("%i 个实际模型时仅超过十个显示搜索框", async (count) => {
    const user = userEvent.setup();
    renderPicker({
      models: Array.from({ length: count }, (_, index) => ({
        id: `model-${index}`,
        name: `模型 ${index}`,
      })),
      selectedModel: "model-0",
      reasoningOptions: [],
      allowDefaultModel: true,
    });
    await user.click(screen.getByTestId("composer-model-control"));
    const search = screen.queryByRole("searchbox", { name: "搜索模型" });
    if (count === 10) {
      expect(search).not.toBeInTheDocument();
    } else {
      expect(search).toBeVisible();
      await waitFor(() => expect(search).toHaveFocus());
    }
    expect(screen.getAllByRole("radio")).toHaveLength(count + 1);
  });

  it("模型搜索匹配名称与 ID，保留空结果提示、清除和重新打开行为", async () => {
    const user = userEvent.setup();
    renderPicker({
      models: [
        ...models,
        { id: "provider/claude-sonnet", name: "快速模型" },
        ...Array.from({ length: 8 }, (_, index) => ({
          id: `other-${index}`,
          name: `其他模型 ${index}`,
        })),
      ],
      reasoningOptions: [],
    });
    await user.click(screen.getByTestId("composer-model-control"));
    const search = screen.getByRole("searchbox", { name: "搜索模型" });
    await user.type(search, "快速");
    expect(screen.getAllByRole("radio")).toHaveLength(1);
    expect(screen.getByRole("radio", { name: "快速模型" })).toBeVisible();
    await user.clear(search);
    await user.type(search, "  CLAUDE-SONNET  ");
    expect(screen.getAllByRole("radio")).toHaveLength(1);
    expect(screen.getByRole("radio", { name: "快速模型" })).toBeVisible();
    await user.clear(search);
    await user.type(search, "不存在的模型");
    expect(screen.queryByRole("radio")).not.toBeInTheDocument();
    expect(screen.getByText("没有匹配的模型")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "清除搜索" }));
    expect(search).toHaveValue("");
    expect(screen.getAllByRole("radio")).toHaveLength(11);
    await user.type(search, "model-a");
    await user.keyboard("{Escape}");
    await user.click(screen.getByTestId("composer-model-control"));
    expect(screen.getByRole("searchbox", { name: "搜索模型" })).toHaveValue("");
    expect(screen.getAllByRole("radio")).toHaveLength(11);
  });

  it("从强度面板进入搜索并用键盘选择模型，返回强度面板后再次进入会清空筛选", async () => {
    const user = userEvent.setup();
    const { onModelChange } = renderPicker({
      models: [
        ...models,
        ...Array.from({ length: 9 }, (_, index) => ({
          id: `other-${index}`,
          name: `其他模型 ${index}`,
        })),
      ],
    });
    await user.click(screen.getByTestId("composer-model-control"));
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    const search = screen.getByRole("searchbox", { name: "搜索模型" });
    await waitFor(() => expect(search).toHaveFocus());
    await user.type(search, "model-b");
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("radio", { name: "模型 B" })).toHaveFocus();
    await user.keyboard(" ");
    expect(onModelChange).toHaveBeenLastCalledWith("model-b");
    await waitFor(() => expect(screen.getByRole("slider")).toHaveFocus());
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    expect(screen.getByRole("searchbox", { name: "搜索模型" })).toHaveValue("");
    expect(screen.getAllByRole("radio")).toHaveLength(11);
  });

  it("胶囊显示当前强度，滑条只在真实档位间切换且保持浮层打开", async () => {
    const user = userEvent.setup();
    const { onReasoningChange } = renderPicker();
    await user.click(screen.getByTestId("composer-model-control"));
    const slider = screen.getByRole("slider", { name: "选择强度" });
    expect(slider).toHaveAttribute("aria-valuemax", "2");
    expect(slider).toHaveAttribute("aria-valuetext", "高");
    await waitFor(() => expect(slider).toHaveFocus());
    await user.keyboard("{ArrowLeft}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("medium");
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      "模型 A中",
    );
    expect(slider).toHaveAttribute("aria-valuetext", "中");
    await user.keyboard("{Home}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("low");
    expect(screen.getByTestId("composer-model-control")).not.toHaveAttribute(
      "data-effort-max",
    );
    await user.keyboard("{End}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("high");
    expect(screen.getByTestId("composer-model-control")).toHaveAttribute(
      "data-effort-max",
    );
    expect(screen.getByRole("dialog", { name: "选择强度" })).toBeVisible();
  });

  it("点击模型名称切换同一浮层的模型列表，选择后提交标识并返回强度面板", async () => {
    const user = userEvent.setup();
    const { onModelChange } = renderPicker();
    await user.click(screen.getByTestId("composer-model-control"));
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
    const selected = screen.getByRole("radio", { name: "模型 A" });
    expect(selected).toBeChecked();
    await waitFor(() => expect(selected).toHaveFocus());
    await user.click(screen.getByRole("radio", { name: "模型 B" }));
    expect(onModelChange).toHaveBeenLastCalledWith("model-b");
    expect(
      screen.getByRole("dialog", { name: "选择强度" }),
    ).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("slider")).toHaveFocus());
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await user.click(screen.getByRole("radio", { name: "模型 B" }));
    expect(onModelChange).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("dialog", { name: "选择强度" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      "模型 B",
    );
  });

  it("从模型列表返回强度面板并恢复键盘焦点", async () => {
    const user = userEvent.setup();
    renderPicker();
    await user.click(screen.getByTestId("composer-model-control"));
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await user.click(screen.getByRole("button", { name: "返回强度设置" }));
    await waitFor(() => expect(screen.getByRole("slider")).toHaveFocus());
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("无推理能力时直接展示模型列表，不显示强度或恢复入口", async () => {
    const user = userEvent.setup();
    renderPicker({ reasoningOptions: [] });
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      /^模型 A$/,
    );
    await user.click(screen.getByTestId("composer-model-control"));
    await waitFor(() =>
      expect(screen.getByRole("radio", { name: "模型 B" })).toBeVisible(),
    );
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "恢复默认强度" }),
    ).not.toBeInTheDocument();
  });

  it("探测期间保留模型切换入口并禁用强度操作", async () => {
    const user = userEvent.setup();
    const { onReasoningChange } = renderPicker({ thinkingPending: true });
    await user.click(screen.getByTestId("composer-model-control"));
    expect(screen.getByRole("status")).toHaveTextContent("正在探测");
    const slider = screen.getByRole("slider");
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    fireEvent.keyUp(slider, { key: "ArrowLeft" });
    expect(onReasoningChange).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "恢复默认强度" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await waitFor(() =>
      expect(screen.getByRole("radio", { name: "模型 B" })).toBeVisible(),
    );
  });

  it("真实探测显示共享加载动效，目录返回后恢复强度选择", async () => {
    const user = userEvent.setup();
    const props = {
      selectedModelName: "模型 A",
      selectedModel: "model-a",
      selectedReasoning: "high",
      models,
      onModelChange: vi.fn(),
      onReasoningChange: vi.fn(),
    };
    const { rerender } = render(
      <ExecutionModelControl
        {...props}
        thinkingPending
        reasoningOptions={[]}
      />,
    );
    const trigger = screen.getByTestId("composer-model-control");
    expect(trigger).toHaveAttribute("aria-busy", "true");
    expect(trigger.querySelector(".agent-grid-spinner")).toBeInTheDocument();
    await user.click(trigger);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("正在探测");
    expect(status.querySelector(".agent-grid-spinner")).toBeInTheDocument();
    expect(screen.queryByRole("slider")).not.toBeInTheDocument();
    rerender(
      <ExecutionModelControl {...props} reasoningOptions={reasoningOptions} />,
    );
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(trigger).not.toHaveAttribute("aria-busy");
    expect(
      trigger.querySelector(".agent-grid-spinner"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("slider")).toHaveAttribute("aria-valuetext", "高");
  });

  it("账号默认强度以独立档位呈现，选择默认提交空值", async () => {
    const user = userEvent.setup();
    const { onReasoningChange } = renderPicker({ allowDefaultReasoning: true });
    await user.click(screen.getByTestId("composer-model-control"));
    await user.keyboard("{Home}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("");
    const slider = screen.getByRole("slider");
    expect(slider).toHaveAttribute("aria-valuetext", "默认");
    expect(slider).toHaveAttribute("aria-valuenow", "0");
    slider.focus();
    await user.keyboard("{ArrowRight}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("low");
  });

  it("内置账号切换强度，不修改模型", async () => {
    const user = userEvent.setup();
    const { onReasoningChange, onModelChange } = renderPicker();
    await user.click(screen.getByTestId("composer-model-control"));
    await user.keyboard("{ArrowLeft}");
    expect(onReasoningChange).toHaveBeenLastCalledWith("medium");
    expect(onModelChange).not.toHaveBeenCalled();
  });

  it.each(["high", ""])(
    "单档位模型以满格展示并禁用滑条，选择值为 %s",
    async (selectedReasoning) => {
      const user = userEvent.setup();
      const { onReasoningChange } = renderPicker({
        selectedReasoning,
        reasoningOptions: [{ id: "high", name: "High" }],
      });
      await user.click(screen.getByTestId("composer-model-control"));
      const dialog = screen.getByRole("dialog", { name: "选择强度" });
      const slider = within(dialog).getByRole("slider");
      fireEvent.keyDown(slider, { key: "End" });
      expect(onReasoningChange).not.toHaveBeenCalled();
      expect(slider).toHaveAttribute("aria-valuetext", "High");
      expect(slider).toHaveAttribute("aria-valuenow", "1");
      expect(slider).toHaveAttribute("aria-valuemax", "1");
      expect(dialog.querySelector('[data-slot="slider-range"]')).toHaveStyle({
        right: "0%",
      });
      expect(screen.getByTestId("composer-model-control")).toHaveAttribute(
        "data-effort-max",
      );
    },
  );

  it("只有 On 开关时不显示满格或最高强度效果", async () => {
    const user = userEvent.setup();
    renderPicker({
      selectedReasoning: "enabled",
      reasoningOptions: [{ id: "enabled", name: "On (default)" }],
    });
    await user.click(screen.getByTestId("composer-model-control"));
    expect(screen.getByRole("slider")).toHaveAttribute("aria-valuenow", "0");
    expect(screen.getByTestId("composer-model-control")).not.toHaveAttribute(
      "data-effort-max",
    );
  });
  it("执行配置的默认模型提交空值，返回强度面板且保持浮层", async () => {
    const user = userEvent.setup();
    const { onModelChange } = renderPicker({ allowDefaultModel: true });
    await user.click(screen.getByTestId("composer-model-control"));
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await user.click(screen.getByRole("radio", { name: "默认模型" }));
    expect(onModelChange).toHaveBeenLastCalledWith("");
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      "默认模型",
    );
    expect(
      screen.getByRole("dialog", { name: "选择强度" }),
    ).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("slider")).toHaveFocus());
  });

  it("执行配置禁用期间不展开或提交选择", async () => {
    const user = userEvent.setup();
    const { onModelChange, onReasoningChange } = renderPicker({
      disabled: true,
    });
    const trigger = screen.getByTestId("composer-model-control");
    expect(trigger).toBeDisabled();
    await user.click(trigger);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(onModelChange).not.toHaveBeenCalled();
    expect(onReasoningChange).not.toHaveBeenCalled();
  });
});

it("表单型模型入口沿用搜索阈值，选择后回填并关闭列表", async () => {
  const user = userEvent.setup();
  const models = Array.from({ length: 11 }, (_, index) => ({
    id: `model-${index}`,
    name: `模型 ${index}`,
  }));
  const result = renderPicker({
    variant: "field",
    label: "默认模型",
    id: "model-field",
    models,
    selectedModel: "model-0",
    reasoningOptions: [],
  });
  await user.click(screen.getByRole("button", { name: "默认模型" }));
  await user.type(
    screen.getByRole("searchbox", { name: "搜索模型" }),
    "模型 10",
  );
  await user.click(screen.getByRole("radio", { name: "模型 10" }));
  expect(result.onModelChange).toHaveBeenCalledWith("model-10");
  expect(screen.getByRole("button", { name: "默认模型" })).toHaveTextContent(
    "模型 10",
  );
  expect(
    screen.queryByRole("searchbox", { name: "搜索模型" }),
  ).not.toBeInTheDocument();
});

it("表单入口有推理选项时也直接打开模型列表，选择后关闭", async () => {
  const user = userEvent.setup();
  const picker = renderPicker({ variant: "field", label: "模型" });
  await user.click(screen.getByRole("button", { name: "模型" }));
  expect(screen.getByRole("radio", { name: "模型 B" })).toBeVisible();
  await user.click(screen.getByRole("radio", { name: "模型 B" }));
  expect(picker.onModelChange).toHaveBeenCalledWith("model-b");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
