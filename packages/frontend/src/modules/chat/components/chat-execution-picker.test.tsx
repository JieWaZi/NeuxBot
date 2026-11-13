import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ExecutionPicker } from "@/shared/ui/execution-picker";

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: () => ({}),
}));

it.each([false, true])(
  "只有一个强度时隐藏选择框与强度摘要，允许默认值为 %s",
  async (allowDefaultReasoning) => {
    const user = userEvent.setup();
    const runtime = { id: "qoder", enabled: true };
    render(
      <ExecutionPicker
        runtimes={[runtime]}
        accounts={[]}
        runtime={runtime}
        models={[{ id: "hy4", name: "Hy4" }]}
        modelId="hy4"
        modelName="Hy4"
        reasoning="high"
        reasoningOptions={[{ id: "high", name: "高" }]}
        allowDefaultReasoning={allowDefaultReasoning}
        onModel={vi.fn()}
        onReasoning={vi.fn()}
        onFast={vi.fn()}
      />,
    );
    const trigger = screen.getByTestId("composer-model-control");
    expect(trigger).not.toHaveTextContent("高");
    await user.click(trigger);
    expect(screen.getByRole("radio", { name: "Hy4" })).toBeVisible();
    expect(
      screen.queryByRole("combobox", { name: "选择强度" }),
    ).not.toBeInTheDocument();
  },
);
