// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
const noop = (): void => {};

/** One document-wide observer; activated elements leave it permanently. */
class ViewportActivation {
  private observer: IntersectionObserver | undefined;
  private readonly activators = new Map<Element, () => void>();

  observe(element: Element, activate: () => void): () => void {
    if (typeof IntersectionObserver === "undefined") {
      activate();
      return noop;
    }
    this.observer ??= new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const current = this.activators.get(entry.target);
        /* v8 ignore next -- the observer reports only elements still registered with it. */
        if (current === undefined) continue;
        this.activators.delete(entry.target);
        this.observer?.unobserve(entry.target);
        current();
      }
      this.releaseEmptyObserver();
    });
    this.activators.set(element, activate);
    this.observer.observe(element);
    return () => {
      this.activators.delete(element);
      this.observer?.unobserve(element);
      this.releaseEmptyObserver();
    };
  }

  private releaseEmptyObserver(): void {
    if (this.activators.size > 0) return;
    this.observer?.disconnect();
    this.observer = undefined;
  }
}

const viewportActivation = new ViewportActivation();

/** 共享一次进入视区的资格；不缓存内容或业务数据。 */
export function observeViewportActivation(
  element: Element,
  activate: () => void,
): () => void {
  return viewportActivation.observe(element, activate);
}
