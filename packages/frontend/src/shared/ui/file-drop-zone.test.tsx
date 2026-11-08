import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { FileDropZone } from "./file-drop-zone";

it("supports keyboard selection and browser File drops through independent callbacks", async () => {
  const user = userEvent.setup();
  const choose = vi.fn(),
    drop = vi.fn();
  const view = render(
    <FileDropZone
      label="选择文件"
      description="点击选择或拖入文件"
      onChoose={choose}
      onFiles={drop}
    />,
  );
  const target = screen.getByRole("button", { name: "选择文件" });
  expect(target).toHaveAccessibleDescription("点击选择或拖入文件");
  await user.tab();
  await user.keyboard("{Enter}");
  expect(choose).toHaveBeenCalledOnce();
  const file = new File(["bytes"], "file.zip");
  fireEvent.drop(target, { dataTransfer: { files: [file] } });
  expect(drop).toHaveBeenCalledWith([file]);
  expect(choose).toHaveBeenCalledOnce();
  view.rerender(
    <FileDropZone
      label="选择文件"
      description="点击选择或拖入文件"
      fileName="file.zip"
      onChoose={choose}
      onFiles={drop}
    />,
  );
  expect(target).toHaveAccessibleDescription("file.zip 点击选择或拖入文件");
});

it("ignores click and drop submissions while disabled", () => {
  const choose = vi.fn(),
    drop = vi.fn();
  render(
    <FileDropZone
      label="选择文件"
      description="点击选择或拖入文件"
      disabled
      onChoose={choose}
      onFiles={drop}
    />,
  );
  const target = screen.getByRole("button", { name: "选择文件" });
  fireEvent.click(target);
  fireEvent.drop(target, {
    dataTransfer: { files: [new File(["bytes"], "file.zip")] },
  });
  expect(choose).not.toHaveBeenCalled();
  expect(drop).not.toHaveBeenCalled();
});
