import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useId, useState } from "react";
import { expect, it, vi } from "vitest";
import { ResourcePageFooter, ResourcePageSurface } from "./resource-page";

it("keeps footer drafts and submissions within their own page pane", async () => {
  const user = userEvent.setup();
  const submitted = vi.fn();
  function Pane({ name }: { name: string }) {
    const formId = useId();
    const [note, setNote] = useState("");
    return (
      <ResourcePageSurface appearance="catalog" data-testid={name}>
        <form
          id={formId}
          onSubmit={(event) => {
            event.preventDefault();
            submitted(name, note);
          }}
        >
          <p>{name}正文</p>
          <ResourcePageFooter aria-label={`${name}保存区`}>
            <input
              aria-label={`${name}备注`}
              value={note}
              onChange={(event) => setNote(event.target.value)}
            />
            <button type="submit" form={formId}>
              保存{name}
            </button>
          </ResourcePageFooter>
        </form>
      </ResourcePageSurface>
    );
  }
  render(
    <>
      <Pane name="左侧" />
      <Pane name="右侧" />
    </>,
  );
  const left = screen.getByTestId("左侧");
  const right = screen.getByTestId("右侧");
  const note = within(left).getByRole("textbox", { name: "左侧备注" });
  expect(note.closest('[data-slot="resource-page-viewport"]')).toBeNull();
  expect(right).not.toContainElement(note);
  await user.type(note, "保留草稿");
  expect(within(left).getByRole("textbox", { name: "左侧备注" })).toBe(note);
  expect(note).toHaveFocus();
  expect(within(right).getByRole("textbox", { name: "右侧备注" })).toHaveValue(
    "",
  );
  await user.click(within(left).getByRole("button", { name: "保存左侧" }));
  expect(submitted).toHaveBeenLastCalledWith("左侧", "保留草稿");
  await user.click(within(right).getByRole("button", { name: "保存右侧" }));
  expect(submitted).toHaveBeenLastCalledWith("右侧", "");
});
