import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { Plus } from "lucide-react";
import { ResourceListItem, ResourceItemAction } from "./resource-list-item";
function Location() {
  const location = useLocation();
  const state: unknown = location.state;
  return (
    <>
      <output aria-label="地址">{location.pathname}</output>
      <output aria-label="返回地址">
        {state &&
        typeof state === "object" &&
        "resourceBack" in state &&
        typeof state.resourceBack === "string"
          ? state.resourceBack
          : ""}
      </output>
    </>
  );
}
it("keeps install actions separate from navigation and retains the resource back link", () => {
  const install = vi.fn();
  render(
    <MemoryRouter initialEntries={["/resources?search=review"]}>
      <Location />
      <ResourceListItem
        title="Review"
        description="Review work"
        icon={<Plus />}
        to="/resources/plugins/review"
        label="View Review"
        actions={
          <ResourceItemAction label="Install Review" onClick={install}>
            <Plus />
          </ResourceItemAction>
        }
      />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Install Review" }));
  expect(install).toHaveBeenCalledOnce();
  expect(screen.getByLabelText("地址")).toHaveTextContent("/resources");
  fireEvent.click(screen.getByRole("link", { name: "View Review" }));
  expect(screen.getByLabelText("地址")).toHaveTextContent(
    "/resources/plugins/review",
  );
  expect(screen.getByLabelText("返回地址")).toHaveTextContent(
    "/resources?search=review",
  );
});
