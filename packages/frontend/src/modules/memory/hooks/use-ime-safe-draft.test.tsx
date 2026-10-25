import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useImeSafeDraft } from "./use-ime-safe-draft";

function DraftInput({
  committed,
  onCommit,
}: {
  committed: string;
  onCommit: (value: string) => void;
}) {
  const draft = useImeSafeDraft(committed, onCommit);
  return <input aria-label="draft" {...draft} />;
}

describe("useImeSafeDraft", () => {
  it("keeps composing pinyin local until composition ends", () => {
    const onCommit = vi.fn();
    render(<DraftInput committed="" onCommit={onCommit} />);
    const input = screen.getByRole("textbox", { name: "draft" });

    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "jilu" } });
    expect(input).toHaveValue("jilu");
    expect(onCommit).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "记录" } });
    fireEvent.compositionEnd(input);
    expect(input).toHaveValue("记录");
    expect(onCommit).toHaveBeenCalledWith("记录");
  });
});
