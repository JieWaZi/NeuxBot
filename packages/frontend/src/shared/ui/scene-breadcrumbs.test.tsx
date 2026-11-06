import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import {
  AppSceneWorkspaceContextProvider,
  type AppSceneWorkspace,
} from "@/shared/workspace/app-scene-workspace";
import {
  SceneChromeHost,
  SceneChromePortalProvider,
} from "./scene-chrome-portal";
import { SceneBreadcrumbs } from "./scene-breadcrumbs";

function workspace(activeTabId: string): AppSceneWorkspace {
  return {
    routeSceneId: "detail",
    dispatch: vi.fn(),
    syncWorkspace: vi.fn(),
    snapshot: {
      activeTabId,
      tabs: [
        { id: "detail", kind: "route", label: "Detail", lastUsed: 1 },
        {
          id: "child",
          kind: "workspace",
          ownerTabId: "detail",
          label: "Child",
          lastUsed: 2,
        },
        { id: "other", kind: "route", label: "Other", lastUsed: 3 },
      ],
    },
  };
}
function view(activeTabId: string, name: string) {
  return (
    <MemoryRouter>
      <AppSceneWorkspaceContextProvider value={workspace(activeTabId)}>
        <SceneChromePortalProvider>
          <SceneChromeHost />
          <SceneBreadcrumbs
            ariaLabel="Page path"
            items={[
              {
                label: "Skills",
                href: "/resources?category=skills&source=plugin&search=review",
              },
              {
                label: "Dev Tools",
                href: "/resources/plugins/plugin-1?version=v2",
              },
              { label: name },
            ]}
          />
        </SceneChromePortalProvider>
      </AppSceneWorkspaceContextProvider>
    </MemoryRouter>
  );
}

describe("SceneBreadcrumbs", () => {
  it("keeps parent destinations and marks only the current real name", () => {
    render(view("detail", "Code Review"));
    expect(screen.getByRole("link", { name: "Skills" })).toHaveAttribute(
      "href",
      "/resources?category=skills&source=plugin&search=review",
    );
    expect(screen.getByRole("link", { name: "Dev Tools" })).toHaveAttribute(
      "href",
      "/resources/plugins/plugin-1?version=v2",
    );
    expect(screen.getByText("Code Review")).toHaveAttribute(
      "aria-current",
      "page",
    );
  });
  it("allows the active workspace child to retain its owner's path", () => {
    render(view("child", "Code Review"));
    expect(
      screen.getByRole("navigation", { name: "Page path" }),
    ).toBeInTheDocument();
  });
  it("does not publish a background owner's late query name", () => {
    const result = render(view("detail", "Skill"));
    result.rerender(view("other", "Code Review"));
    expect(
      screen.queryByRole("navigation", { name: "Page path" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Code Review")).not.toBeInTheDocument();
  });
});
