import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { ProjectCard } from "../components/project-card";
import { ProjectDetailPage } from "./project-detail-page";

vi.mock("../components/project-form-dialog", () => ({
  ProjectFormDialog: () => null,
}));
vi.mock("../components/project-delete-dialog", () => ({
  ProjectDeleteDialog: () => null,
}));
vi.mock("../api/project-queries", () => ({
  getProjectOptions: () => ({
    queryKey: ["project-detail", "desk"],
    queryFn: async () => ({
      id: "desk",
      name: "Research Desk",
      available: true,
      sources: [],
    }),
  }),
}));
function Location() {
  const location = useLocation();
  return (
    <output aria-label="Current route">
      {location.pathname}
      {location.search}
    </output>
  );
}
function show(initial: { pathname: string; search?: string; state?: unknown }) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[initial]}>
        <Location />
        <Routes>
          <Route
            path="/wakers/:wakerId/projects"
            element={
              <ProjectCard
                wakerId="ada"
                project={{ id: "desk", name: "Research Desk", available: true }}
              />
            }
          />
          <Route
            path="/wakers/:wakerId/projects/:projectId"
            element={<ProjectDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it("restores project list search through the card's actual detail navigation", async () => {
  const user = userEvent.setup();
  show({ pathname: "/wakers/ada/projects", search: "?search=desk" });
  await user.click(
    screen.getByRole("link", { name: "查看项目 Research Desk" }),
  );
  expect(
    await screen.findByRole("heading", { name: "Research Desk" }),
  ).toBeVisible();
  const ancestor = screen.getByRole("link", { name: "项目" });
  expect(ancestor).toHaveAttribute("href", "/wakers/ada/projects?search=desk");
  await user.click(ancestor);
  expect(screen.getByLabelText("Current route")).toHaveTextContent(
    "/wakers/ada/projects?search=desk",
  );
});
it.each([
  "/wakers/grace/projects?search=desk",
  "https://example.com/wakers/ada/projects?search=desk",
  "/wakers/ada/projects/other",
])(
  "uses the current owner's project list for invalid ancestor %s",
  async (projectBack) => {
    show({ pathname: "/wakers/ada/projects/desk", state: { projectBack } });
    expect(
      await screen.findByRole("heading", { name: "Research Desk" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "项目" })).toHaveAttribute(
      "href",
      "/wakers/ada/projects",
    );
  },
);
