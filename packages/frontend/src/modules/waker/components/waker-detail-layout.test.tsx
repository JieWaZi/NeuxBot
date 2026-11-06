import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { expect, it } from "vitest";
import "@/shared/i18n";
import { getWakerQueryKey } from "../api/waker-queries";
import { WakerDetailLayout } from "./waker-detail-layout";

it("uses the existing parent's cache name without requesting another parent", async () => {
  const client = new QueryClient();
  const key = getWakerQueryKey({ path: { wakerId: "waker-1" } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/wakers/waker-1/projects/project-1"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/projects/:projectId"
            element={
              <WakerDetailLayout
                active="projects"
                breadcrumbItems={[
                  {
                    label: "项目",
                    href: "/wakers/waker-1/projects?search=desk",
                  },
                  { label: "Research Desk" },
                ]}
              >
                <p>Content</p>
              </WakerDetailLayout>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.getByText("Research Desk")).toHaveAttribute(
    "aria-current",
    "page",
  );
  expect(screen.getByRole("link", { name: "项目" })).toHaveAttribute(
    "href",
    "/wakers/waker-1/projects?search=desk",
  );
  await act(async () => {
    client.setQueryData(key, { id: "waker-1", name: "Ada" });
  });
  expect(await screen.findByRole("link", { name: "Ada" })).toHaveAttribute(
    "href",
    "/wakers/waker-1",
  );
});
