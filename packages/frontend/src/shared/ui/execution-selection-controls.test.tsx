import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { ExecutionRuntimeControl } from "./execution-selection-controls";

it("智能体账号子菜单按实际 Profile 归属筛选", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const root = { id: "codex", enabled: true, state: "ready" };
  const profile = {
    id: "profile:work",
    baseRuntimeId: "codex",
    name: "Work Codex",
    enabled: true,
    state: "ready",
  };
  render(
    <ExecutionRuntimeControl
      runtimes={[root, profile]}
      accounts={[
        {
          id: "root-account",
          runtimeId: "codex",
          name: "Root Account",
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.example.test/v1",
            },
          ],
        },
        {
          id: "profile-account",
          runtimeId: "profile:work",
          name: "Profile Account",
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.example.test/v1",
            },
          ],
        },
      ]}
      selectedRuntime={root}
      selectedAccountId="root-account"
      onSelect={onSelect}
    />,
  );
  await user.click(screen.getByTestId("composer-runtime-control"));
  await user.click(screen.getByRole("menuitemradio", { name: /Work Codex/ }));
  expect(
    screen.queryByRole("menuitemradio", { name: "Root Account" }),
  ).not.toBeInTheDocument();
  await user.click(
    screen.getByRole("menuitemradio", { name: "Profile Account" }),
  );
  expect(onSelect).toHaveBeenCalledExactlyOnceWith(profile, "profile-account");
});
