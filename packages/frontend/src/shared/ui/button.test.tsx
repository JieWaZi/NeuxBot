import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { Button } from "./button";

it("keeps focus during loading and blocks repeated pointer and keyboard submission", async () => {
  const user = userEvent.setup();
  const submit = vi.fn((event: React.FormEvent) => event.preventDefault());
  const clicked = vi.fn();
  const view = (loading: boolean) => (
    <form onSubmit={submit}>
      <Button loading={loading} onClick={clicked}>
        保存
      </Button>
    </form>
  );
  const { rerender } = render(view(false));
  const button = screen.getByRole("button", { name: "保存" });
  await user.tab();
  expect(button).toHaveFocus();
  rerender(view(true));
  expect(button).toHaveFocus();
  expect(button).toHaveAttribute("aria-busy", "true");
  expect(button).toHaveAttribute("aria-disabled", "true");
  await user.keyboard("{Enter}");
  fireEvent.click(button);
  expect(clicked).not.toHaveBeenCalled();
  expect(submit).not.toHaveBeenCalled();
  rerender(view(false));
  await user.keyboard("{Enter}");
  expect(clicked).toHaveBeenCalledOnce();
  expect(submit).toHaveBeenCalledOnce();
});

it("blocks the child action while an asChild button is loading", () => {
  const clicked = vi.fn();
  render(
    <Button asChild loading>
      <a href="#save" onClick={clicked}>
        保存链接
      </a>
    </Button>,
  );
  fireEvent.click(screen.getByRole("link", { name: "保存链接" }));
  expect(clicked).not.toHaveBeenCalled();
});
