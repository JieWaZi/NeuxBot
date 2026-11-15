import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as Runtime } from "@/shared/api/generated/types.gen";
import { ExecutionPicker } from "./execution-picker";

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: ({ runtime }: { runtime?: Runtime }) => ({
    models: runtime?.models ?? [],
    defaultModel: runtime?.defaultModel,
    catalogState: "fresh",
    catalogPending: false,
  }),
}));

it("only lists confirmed installations regardless of authentication and diagnostic state", async () => {
  const user = userEvent.setup();
  const installed = {
    id: "codex",
    installed: true,
    enabled: true,
    state: "unavailable",
    auth: "signed_out",
  };
  render(
    <ExecutionPicker
      runtimes={[
        installed,
        { id: "claude", installed: false, enabled: true, state: "ready" },
        {
          id: "cursor",
          enabled: true,
          state: "ready",
          models: [{ id: "cached" }],
        },
        { id: "pi", installed: true, enabled: false },
      ]}
      accounts={[]}
      runtime={installed}
      models={[]}
      modelId=""
      modelName="Codex"
      reasoning=""
      allowDefaultReasoning
      onModel={vi.fn()}
      onReasoning={vi.fn()}
    />,
  );
  await user.click(screen.getByTestId("composer-model-control"));
  const sidebar = within(screen.getByRole("group", { name: "智能体" }));
  expect(sidebar.getByRole("button", { name: "Codex" })).toBeEnabled();
  expect(sidebar.getByRole("button", { name: "Pi" })).toBeDisabled();
  expect(
    sidebar.queryByRole("button", { name: "Claude" }),
  ).not.toBeInTheDocument();
  expect(
    sidebar.queryByRole("button", { name: "Cursor" }),
  ).not.toBeInTheDocument();
  expect(sidebar.getAllByRole("button")).toHaveLength(2);
});

it("keeps an uninstalled selection in the summary and previews an installed agent with its own account", async () => {
  const user = userEvent.setup();
  const previous = { id: "claude", enabled: true, installed: false };
  const current = {
    id: "codex",
    enabled: true,
    installed: true,
    defaultAccountId: "codex-account",
    models: [{ id: "codex-model", name: "Codex Model" }],
  };
  const select = vi.fn();
  render(
    <ExecutionPicker
      runtimes={[previous, current]}
      accounts={[
        {
          id: "codex-account",
          runtimeId: "codex",
          name: "Codex Account",
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://api.example.test",
          },
        },
      ]}
      runtime={previous}
      accountId="old-account"
      models={[{ id: "old-model", name: "Old Model" }]}
      modelId="old-model"
      modelName="Old Model"
      reasoning=""
      allowDefaultReasoning
      onModel={select}
      onReasoning={vi.fn()}
    />,
  );
  const trigger = screen.getByTestId("composer-model-control");
  expect(trigger).toHaveTextContent("Old Model");
  await user.click(trigger);
  expect(
    screen.queryByRole("button", { name: "Claude" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Codex" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByRole("combobox", { name: "账号" })).toHaveTextContent(
    "Codex Account",
  );
  expect(
    screen.queryByRole("radio", { name: "Old Model" }),
  ).not.toBeInTheDocument();
  expect(select).not.toHaveBeenCalled();
  await user.click(screen.getByRole("radio", { name: "Codex Model" }));
  expect(select).toHaveBeenCalledExactlyOnceWith(
    current,
    "codex-account",
    current.models[0],
  );
});

it("shows an empty agent state without exposing cached models when no CLI is installed", async () => {
  const user = userEvent.setup();
  const previous = { id: "claude", enabled: true, installed: false };
  const select = vi.fn();
  render(
    <ExecutionPicker
      runtimes={[previous]}
      accounts={[]}
      runtime={previous}
      models={[{ id: "old-model", name: "Old Model" }]}
      modelId="old-model"
      modelName="Old Model"
      reasoning="high"
      reasoningOptions={[{ id: "low" }, { id: "high" }]}
      allowDefaultReasoning
      onModel={select}
      onReasoning={vi.fn()}
      onFast={vi.fn()}
      fastSupported
      pending
    />,
  );
  await user.click(screen.getByTestId("composer-model-control"));
  expect(
    within(screen.getByRole("group", { name: "智能体" })).queryByRole("button"),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "选择强度" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("没有可用的智能体");
  expect(select).not.toHaveBeenCalled();
});
