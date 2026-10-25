import { render, screen, fireEvent } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { MarkdownContent } from "./markdown-content";
const open = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
vi.mock("@/shared/platform", () => ({ openExternalURL: open }));
it("renders resource documentation without loading images or exposing local file links", async () => {
  const { container } = render(
    <MarkdownContent
      content={
        "# Usage\n\n[Website](https://example.com/docs)\n\n[Reference](references/guide.md)\n\n![Remote logo](https://example.com/logo.png)\n\n[Private file](file:///private/secret)\n\n<script>alert(1)</script>"
      }
    />,
  );
  expect(await screen.findByRole("heading", { name: "Usage" })).toBeVisible();
  expect(screen.getByText("Reference")).toBeVisible();
  expect(
    screen.queryByRole("link", { name: "Reference" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/\[blocked\]/)).not.toBeInTheDocument();
  expect(container.querySelector("img")).toBeNull();
  expect(container.querySelector("script")).toBeNull();
  expect(
    screen.queryByRole("link", { name: "Private file" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("link", { name: "Website" }));
  expect(open).toHaveBeenCalledWith("https://example.com/docs");
});
