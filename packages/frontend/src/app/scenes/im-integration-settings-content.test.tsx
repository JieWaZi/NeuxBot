import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { expect, it, vi } from "vitest";

import { IMIntegrationSettingsContent } from "./im-integration-settings-content";

vi.mock("@/modules/im", () => ({
  IMPage: () => <h1>IM 平台列表</h1>,
  IMConnectionDetailDialog: ({
    connectionId,
    initialBindingId,
    onClose,
  }: {
    connectionId: string;
    initialBindingId?: string;
    onClose: () => void;
  }) => (
    <div role="dialog" aria-label="连接详情">
      <p>
        {connectionId}/{initialBindingId}
      </p>
      <button onClick={onClose}>关闭</button>
    </div>
  ),
}));

function LocationProbe() {
  const location = useLocation();
  return (
    <output aria-label="当前地址">
      {location.pathname}
      {location.search}
    </output>
  );
}

it("旧深链在列表上打开详情，关闭只移除详情定位参数", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter
      initialEntries={[
        "/settings/integration-im?connection=one&binding=chat-one&keep=value",
      ]}
    >
      <IMIntegrationSettingsContent />
      <LocationProbe />
    </MemoryRouter>,
  );
  expect(screen.getByRole("heading", { name: "IM 平台列表" })).toBeVisible();
  expect(screen.getByRole("dialog", { name: "连接详情" })).toHaveTextContent(
    "one/chat-one",
  );
  await user.click(screen.getByRole("button", { name: "关闭" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("status", { name: "当前地址" })).toHaveTextContent(
    "/settings/integration-im?keep=value",
  );
});
