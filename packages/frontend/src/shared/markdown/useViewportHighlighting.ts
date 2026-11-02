// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
import { useCallback, useContext, useEffect, useState } from "react";
import type { RefObject } from "react";
import { MarkdownVisibility } from "./visibility";
import { observeViewportActivation } from "@/shared/lib/viewport-activation";
import { supportsHighlighting } from "./highlight.ts";

/**
 * Activate one supported code surface when it first intersects the viewport.
 * Activation lasts for the component lifetime; browsers without
 * IntersectionObserver activate immediately.
 * @param target - Code surface whose plain rendering reserves its geometry.
 * @param lang - Optional language hint.
 * @returns Whether this component may build highlighted output.
 */
export function useViewportHighlighting(
  target: RefObject<Element | null>,
  lang: string | undefined,
): boolean {
  const visible = useContext(MarkdownVisibility);
  const supported = supportsHighlighting(lang);
  const [activated, setActivated] = useState(false);
  const activate = useCallback(() => {
    setActivated(true);
  }, []);

  useEffect(() => {
    if (activated || !supported || !visible) return;
    const element = target.current;
    /* v8 ignore next -- React attaches the host ref before running effects. */
    if (element === null) return;
    return observeViewportActivation(element, activate);
  }, [activate, activated, supported, target, visible]);

  return activated && supported && visible;
}
