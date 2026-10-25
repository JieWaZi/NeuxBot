import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginOverview } from "./plugin-overview";
const read = vi.hoisted(() => vi.fn());
vi.mock("../api/plugin-queries", () => ({
  getCatalogPluginSkillOptions: (options: { query: object }) => ({
    queryKey: ["catalog-skill", options.query],
    queryFn: () => read(options.query),
  }),
  getPluginSkillOptions: (options: { query: object }) => ({
    queryKey: ["installed-skill", options.query],
    queryFn: () => read(options.query),
  }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it.each(["catalog", "installed"])(
  "shows upstream capabilities and opens full target skill before/after installation (%s)",
  async (mode) => {
    const target = "codex";
    read.mockResolvedValue({
      skill: {
        key: "skills/review",
        name: "review",
        description: "Review changes",
      },
      content:
        "# Upstream review instructions\n\n" +
        "Full instructions paragraph.\n\n".repeat(200) +
        "Last line of complete upstream SKILL.md.",
    });
    const capabilities = {
      skills: [
        { key: "skills/review", name: "review", description: "Review changes" },
      ],
      servers: [
        {
          key: "github",
          name: "github",
          transport: "streamable-http",
          endpoint: "https://api.githubcopilot.com",
        },
      ],
    };
    const sourceCommit = "a".repeat(40),
      archiveSHA256 = "b".repeat(64);
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <PluginOverview
            pkg={{
              manifest: { name: "github" },
              skills: [],
              servers: [],
              nativeImplementations: {
                codex: capabilities,
                claude: capabilities,
              },
            }}
            skillContext={
              mode === "catalog"
                ? { catalogKey: "github", sourceCommit, archiveSHA256 }
                : { pluginId: "p", versionId: "fixed-v" }
            }
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getAllByRole("heading", { name: "github" })).toHaveLength(1);
    expect(
      screen.queryByText(/适用于|Claude Code|Codex|streamable-http/),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "查看技能 review",
      }),
    );
    expect(
      await screen.findByRole(
        "heading",
        { name: "Upstream review instructions" },
        { timeout: 5000 },
      ),
    ).toBeVisible();
    expect(
      screen.getByText("Last line of complete upstream SKILL.md."),
    ).toBeVisible();
    expect(read).toHaveBeenCalledWith(
      expect.objectContaining(
        mode === "catalog"
          ? { skillKey: "skills/review", target, sourceCommit, archiveSHA256 }
          : { key: "skills/review", versionId: "fixed-v", target },
      ),
    );
    expect(screen.queryByText("原生实现")).not.toBeInTheDocument();
  },
);

it("hides empty Skill and MCP sections", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{ manifest: { name: "empty" }, skills: [], servers: [] }}
      />
    </MemoryRouter>,
  );
  expect(
    screen.queryByRole("heading", { name: /技能/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: /连接器/ }),
  ).not.toBeInTheDocument();
});

it("prefers one owned business capability across different paths, descriptions and endpoints", async () => {
  read.mockResolvedValue({
    skill: { name: "review" },
    content: "# Owned guide",
  });
  const skill = {
    key: "skills/review",
    name: "review",
    description: "Review changes",
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <PluginOverview
          pkg={{
            skills: [skill],
            servers: [
              {
                key: "github",
                url: "https://api.githubcopilot.com/mcp?token=hidden",
              },
            ],
            nativeImplementations: {
              codex: {
                skills: [skill],
                servers: [
                  {
                    key: "github",
                    name: "github",
                    endpoint: "https://api.githubcopilot.com",
                    transport: "streamable-http",
                  },
                ],
              },
              claude: {
                skills: [
                  {
                    ...skill,
                    key: "skills/another-folder",
                    description: "A target-specific explanation",
                  },
                ],
                servers: [
                  {
                    key: "github",
                    name: "github",
                    endpoint: "https://another-service.example",
                    transport: "streamable-http",
                  },
                ],
              },
            },
          }}
          skillContext={{ pluginId: "p", versionId: "v" }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    screen.getAllByRole("button", { name: "查看技能 review" }),
  ).toHaveLength(1);
  expect(screen.getAllByRole("heading", { name: "github" })).toHaveLength(1);
  expect(
    screen.queryByText(/hidden|Codex|Claude Code|streamable-http/),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "查看技能 review" }));
  expect(
    await screen.findByRole("heading", { name: "Owned guide" }),
  ).toBeVisible();
  expect(read).toHaveBeenCalledWith(
    expect.objectContaining({
      key: "skills/review",
      target: undefined,
      versionId: "v",
    }),
  );
});
