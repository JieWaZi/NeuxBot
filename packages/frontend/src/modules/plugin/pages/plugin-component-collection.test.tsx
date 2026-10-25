import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { usePluginComponents } from "../api/use-plugin-components";
import { PluginComponentCollection } from "./plugin-component-collection";

vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("../api/use-plugin-resources", () => ({
  usePluginResources: () => ({
    data: [
      {
        plugin: { id: "p", name: "GitHub" },
        resolvedVersionId: "v1",
        versions: [
          {
            id: "v1",
            package: {
              servers: [
                { key: "github", type: "http", url: "https://example.com/mcp" },
              ],
            },
          },
        ],
        installations: [{ wakerId: "w1", versionId: "v1" }],
      },
    ],
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
}));
afterEach(cleanup);
function Collection({ wakerId }: { wakerId: string }) {
  const resources = usePluginComponents("connectors", wakerId, "");
  const location = useLocation();
  return (
    <>
      <PluginComponentCollection
        kind="connectors"
        wakerId={wakerId}
        resources={resources}
        preview={false}
        onSourceSelect={() => {}}
      />
      <p data-testid="location">{location.pathname + location.search}</p>
    </>
  );
}
it.each(["", "w1"])(
  "opens the connector rather than its parent plugin in scope %s",
  (wakerId) => {
    render(
      <MemoryRouter>
        <Collection wakerId={wakerId} />
      </MemoryRouter>,
    );
    const link = screen.getByRole("link", { name: "查看连接器 github" });
    const prefix = wakerId ? "/wakers/w1" : "/resources";
    expect(link).toHaveAttribute(
      "href",
      prefix + "/plugins/p/connectors/github?version=v1",
    );
    fireEvent.click(link);
    expect(screen.getByTestId("location")).toHaveTextContent(
      prefix + "/plugins/p/connectors/github?version=v1",
    );
  },
);
