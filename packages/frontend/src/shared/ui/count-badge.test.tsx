import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { CountBadge } from "./count-badge";

describe("CountBadge", () => {
  it("渲染数量并在超过上限时显示加号", () => {
    const { rerender } = render(<CountBadge count={4} />);
    expect(screen.getByText("4")).toBeInTheDocument();

    rerender(<CountBadge count={12} />);
    expect(screen.getByText("9+")).toBeInTheDocument();
  });

  it("零或负数时不渲染", () => {
    const { container, rerender } = render(<CountBadge count={0} />);
    expect(container).toBeEmptyDOMElement();

    rerender(<CountBadge count={-1} />);
    expect(container).toBeEmptyDOMElement();
  });
});
