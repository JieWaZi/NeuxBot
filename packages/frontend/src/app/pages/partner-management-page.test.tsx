import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { PartnerManagementPage } from "./partner-management-page";

vi.mock("@/modules/team", () => ({
  TeamCollection: () => null,
  TeamFormDialog: () => null,
}));
vi.mock("@/modules/waker/pages/waker-list-page", () => ({
  WakerListPage: () => <div>伙伴目录</div>,
}));

it("旧伙伴目录地址直接进入聊天，不挂载目录页", async () => {
  render(
    <MemoryRouter initialEntries={["/wakers"]}>
      <Routes>
        <Route path="/wakers" element={<PartnerManagementPage />} />
        <Route path="/chat" element={<textarea aria-label="输入消息" />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(
    await screen.findByRole("textbox", { name: "输入消息" }),
  ).toBeVisible();
  expect(screen.queryByText("伙伴目录")).not.toBeInTheDocument();
});
