import { undo } from "@codemirror/commands";
import { EditorView } from "@codemirror/view";
import { act, render } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { CodeMirrorEditor } from "./code-mirror-editor";

it("keeps the document and undo history when theme, language or label changes", () => {
  const props = {
    initialValue: "first",
    path: "notes.md",
    label: "文件编辑器",
    dark: false,
    onChange: vi.fn(),
    onSave: vi.fn(),
    onCancel: vi.fn(),
  };
  const result = render(<CodeMirrorEditor {...props} />);
  const editor = result.container.querySelector<HTMLElement>(".cm-editor");
  if (!editor) throw new Error("CodeMirror host was not created");
  const view = EditorView.findFromDOM(editor);
  if (!view) throw new Error("CodeMirror view was not created");
  act(() => view.dispatch({ changes: { from: 5, insert: " changed" } }));
  expect(view.state.doc.toString()).toBe("first changed");

  result.rerender(
    <CodeMirrorEditor {...props} path="notes.ts" label="Editor" dark />,
  );
  const updatedHost = result.container.querySelector<HTMLElement>(".cm-editor");
  if (!updatedHost) throw new Error("CodeMirror host was removed");
  expect(EditorView.findFromDOM(updatedHost)).toBe(view);
  expect(view.contentDOM).toHaveAttribute("aria-label", "Editor");
  expect(view.state.doc.toString()).toBe("first changed");
  act(() => expect(undo(view)).toBe(true));
  expect(view.state.doc.toString()).toBe("first");
});
