import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap, lineNumbers } from "@codemirror/view";
import { useEffect, useRef, useState } from "react";

import { cmSurfaceTheme, CmThemeCompartment } from "./cm-themes";
import { languageForPath } from "./lang";

type CodeMirrorEditorProps = {
  initialValue: string;
  path: string;
  label: string;
  dark: boolean;
  readOnly?: boolean;
  targetLine?: number;
  onChange?: (value: string) => void;
  onSave?: () => void;
  onCancel?: () => void;
};

/** CodeMirror 编辑核心；文档缓冲和保存协议由外层持有。 */
export function CodeMirrorEditor({
  initialValue,
  path,
  label,
  dark,
  readOnly = false,
  targetLine,
  onChange,
  onSave,
  onCancel,
}: CodeMirrorEditorProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const themeRef = useRef<CmThemeCompartment | null>(null);
  const languageRef = useRef<Compartment | null>(null);
  const [mountedValue] = useState(initialValue);
  const callbacks = useRef({ onChange, onSave, onCancel });

  useEffect(() => {
    callbacks.current = { onChange, onSave, onCancel };
  }, [onCancel, onChange, onSave]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const theme = new CmThemeCompartment();
    const language = new Compartment();
    themeRef.current = theme;
    languageRef.current = language;
    const state = EditorState.create({
      doc: mountedValue,
      extensions: [
        EditorView.lineWrapping,
        lineNumbers(),
        history(),
        EditorState.readOnly.of(readOnly),
        EditorView.editable.of(!readOnly),
        highlightSelectionMatches(),
        EditorState.tabSize.of(2),
        EditorView.contentAttributes.of({
          role: "textbox",
          "aria-label": label,
          spellcheck: "false",
        }),
        cmSurfaceTheme,
        theme.of(dark),
        language.of(languageForPath(path) ?? []),
        EditorView.updateListener.of((update) => {
          if (update.docChanged)
            callbacks.current.onChange?.(update.state.doc.toString());
        }),
        keymap.of([
          {
            key: "Mod-s",
            preventDefault: true,
            run: () => {
              callbacks.current.onSave?.();
              return true;
            },
          },
          {
            key: "Mod-Enter",
            preventDefault: true,
            run: () => {
              callbacks.current.onSave?.();
              return true;
            },
          },
          {
            key: "Escape",
            run: () => {
              callbacks.current.onCancel?.();
              return true;
            },
          },
          ...searchKeymap,
          ...defaultKeymap,
          ...historyKeymap,
        ]),
        EditorView.theme({
          ".cm-content": { padding: "16px" },
          ".cm-gutters": { paddingTop: "16px" },
        }),
      ],
    });
    const view = new EditorView({ state, parent: host });
    viewRef.current = view;
    if (!readOnly) view.focus();
    return () => {
      view.destroy();
      viewRef.current = null;
      themeRef.current = null;
      languageRef.current = null;
    };
    // Theme, language and label updates never recreate the document or undo history.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mountedValue]);

  useEffect(() => {
    const view = viewRef.current;
    const theme = themeRef.current;
    if (view && theme) view.dispatch({ effects: theme.reconfigure(dark) });
  }, [dark]);

  useEffect(() => {
    const view = viewRef.current;
    const language = languageRef.current;
    if (view && language)
      view.dispatch({
        effects: language.reconfigure(languageForPath(path) ?? []),
      });
  }, [path]);

  useEffect(() => {
    viewRef.current?.contentDOM.setAttribute("aria-label", label);
  }, [label]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !targetLine) return;
    const line = view.state.doc.line(
      Math.min(Math.max(1, targetLine), view.state.doc.lines),
    );
    view.dispatch({
      selection: { anchor: line.from, head: line.to },
      effects: EditorView.scrollIntoView(line.from, { y: "center" }),
    });
  }, [targetLine]);

  return <div ref={hostRef} className="h-full min-h-0 w-full" />;
}
