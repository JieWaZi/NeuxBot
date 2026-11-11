import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { IMIntegrationSettingsContent } from "./im-integration-settings-content";

vi.mock("@/modules/im", () => ({
  IMPage: () => <h1>IM 平台列表</h1>,
  IMConnectionDetailPage: ({
    connectionId,
    initialBindingId,
    onBack,
  }: {
    connectionId: string;
    initialBindingId?: string;
    onBack: (platform: "feishu") => void;
  }) => (
    <section aria-label="三级连接">
      <h1>飞书连接</h1>
      <p>
        {connectionId}/{initialBindingId}
      </p>
      <button onClick={() => onBack("feishu")}>返回飞书</button>
    </section>
  ),
}));
afterEach(cleanup);
function LocationProbe() {
  const location = useLocation();
  return (
    <output aria-label="当前地址">
      {location.pathname}
      {location.search}
    </output>
  );
}

it.each(["binding", "chat"])(
  "旧%s深链直接挂三级页面，返回只移除连接定位并保留父平台",
  async (bindingKey) => {
    const user = userEvent.setup();
    render(
      <MemoryRouter
        initialEntries={[
          `/settings/integration-im?connection=one&${bindingKey}=chat-one&keep=value`,
        ]}
      >
        <IMIntegrationSettingsContent />
        <LocationProbe />
      </MemoryRouter>,
    );
    expect(screen.getByRole("region", { name: "三级连接" })).toHaveTextContent(
      "one/chat-one",
    );
    expect(
      screen.queryByRole("heading", { name: "IM 平台列表" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "返回飞书" }));
    expect(screen.getByRole("heading", { name: "IM 平台列表" })).toBeVisible();
    expect(screen.getByRole("status", { name: "当前地址" })).toHaveTextContent(
      "/settings/integration-im?keep=value&platform=feishu",
    );
  },
);
