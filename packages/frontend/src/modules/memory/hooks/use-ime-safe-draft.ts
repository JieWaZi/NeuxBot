import { useEffect, useRef, useState } from "react";
import type { ChangeEvent, CompositionEvent } from "react";

function isComposingEvent(event: { nativeEvent: Event }) {
  return "isComposing" in event.nativeEvent && event.nativeEvent.isComposing;
}

export function useImeSafeDraft(
  committed: string,
  onCommit: (value: string) => void,
) {
  const [draft, setDraft] = useState(committed);
  const composingRef = useRef(false);

  useEffect(() => {
    if (!composingRef.current) setDraft(committed);
  }, [committed]);

  return {
    onChange: (event: ChangeEvent<HTMLInputElement>) => {
      const value = event.target.value;
      setDraft(value);
      if (composingRef.current || isComposingEvent(event)) return;
      onCommit(value);
    },
    onCompositionEnd: (event: CompositionEvent<HTMLInputElement>) => {
      composingRef.current = false;
      const value = event.currentTarget.value;
      setDraft(value);
      onCommit(value);
    },
    onCompositionStart: () => {
      composingRef.current = true;
    },
    value: draft,
  };
}
