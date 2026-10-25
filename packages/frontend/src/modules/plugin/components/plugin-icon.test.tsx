import { render, cleanup } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { PluginIcon } from "./plugin-icon";

afterEach(cleanup);
it("uses only the supplied package icon for catalog, detail and install sizes", () => {
  const icon = "data:image/png;base64,iVBORw0KGgo=";
  const { container, rerender } = render(<PluginIcon pkg={{ icon }} />);
  expect(container.querySelector("img")).toHaveAttribute("src", icon);
  const legacy = JSON.parse(
    '{"manifest":{"extensions":{"com.ally":{"provider":"feishu"}}}}',
  );
  rerender(<PluginIcon pkg={legacy} hero />);
  expect(container.querySelector("img")).not.toBeInTheDocument();
});
