import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { expect, it } from "vitest";
import { PartnerManagementPage } from "./partner-management-page";

it.each(["/wakers", "/wakers?tab=teams", "/teams"])(
  "旧目录 %s 直接进入聊天，不挂载目录页",
  async (href) => {
    render(
      <MemoryRouter initialEntries={[href]}>
        <Routes>
          <Route path="/wakers" element={<PartnerManagementPage />} />
          <Route path="/teams" element={<PartnerManagementPage />} />
          <Route path="/chat" element={<textarea aria-label="输入消息" />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole("textbox", { name: "输入消息" }),
    ).toBeVisible();
  },
);
