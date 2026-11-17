import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Link, Route, Routes } from "react-router-dom";
import { expect, it } from "vitest";

import { NavigationItem } from "./navigation-panel";

it("导航条目复用链接的地址和键盘跳转，保留图标与可访问名称", async () => {
  render(
    <MemoryRouter>
      <NavigationItem
        asChild
        density="catalog"
        active
        label="概览"
        icon={<svg aria-hidden="true" />}
      >
        <Link to="/wakers/partner" />
      </NavigationItem>
      <Routes>
        <Route path="/" element={null} />
        <Route path="/wakers/partner" element={<h1>伙伴概览</h1>} />
      </Routes>
    </MemoryRouter>,
  );
  const link = screen.getByRole("link", { name: "概览" });
  expect(link).toHaveAttribute("href", "/wakers/partner");
  expect(link).toHaveAttribute("aria-current", "page");
  expect(link.querySelector("svg")).not.toBeNull();
  expect(screen.queryByRole("button")).toBeNull();
  const user = userEvent.setup();
  await user.tab();
  expect(link).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(
    await screen.findByRole("heading", { name: "伙伴概览" }),
  ).toBeVisible();
});
