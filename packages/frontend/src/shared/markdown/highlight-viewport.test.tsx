// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
// @vitest-environment jsdom

import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MarkdownVisibility } from "./visibility";
import { CodeBlock } from "./CodeBlock";
const markdownLabels = { code: { copyLabel: "Copy", copiedLabel: "Copied" } };

function required<T>(value: T | null | undefined): T {
  if (value == null) throw new Error("缺少预期观察目标");
  return value;
}

class IntersectionObserverStub {
  static instances: IntersectionObserverStub[] = [];

  readonly observed = new Set<Element>();
  readonly unobserved = new Set<Element>();
  disconnected = false;

  constructor(private readonly callback: IntersectionObserverCallback) {
    IntersectionObserverStub.instances.push(this);
  }

  observe(element: Element): void {
    this.observed.add(element);
  }

  unobserve(element: Element): void {
    this.observed.delete(element);
    this.unobserved.add(element);
  }

  disconnect(): void {
    this.disconnected = true;
    this.observed.clear();
  }

  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }

  intersect(element: Element, isIntersecting: boolean): void {
    this.callback(
      [{ target: element, isIntersecting } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
}

beforeEach(() => {
  IntersectionObserverStub.instances = [];
  vi.stubGlobal("IntersectionObserver", IntersectionObserverStub);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("viewport-activated syntax highlighting", () => {
  it("keeps offscreen blocks plain and permanently activates only intersecting blocks", async () => {
    const view = render(
      <>
        <CodeBlock code="const first = 1" lang="ts" {...markdownLabels.code} />
        <CodeBlock code="const second = 2" lang="ts" {...markdownLabels.code} />
        <CodeBlock code="const third = 3" lang="ts" {...markdownLabels.code} />
      </>,
    );
    const blocks = [
      ...view.container.querySelectorAll('[data-streamdown="code-block"]'),
    ];
    expect(blocks).toHaveLength(3);
    expect(IntersectionObserverStub.instances).toHaveLength(1);
    const observer = required(IntersectionObserverStub.instances[0]);
    expect(observer.observed.size).toBe(3);
    expect(view.container.querySelectorAll("pre.shiki")).toHaveLength(0);

    act(() => {
      observer.intersect(required(blocks[0]), false);
    });
    expect(view.container.querySelectorAll("pre.shiki")).toHaveLength(0);

    act(() => {
      observer.intersect(required(blocks[0]), true);
      observer.intersect(required(blocks[1]), true);
    });
    await waitFor(() => {
      expect(required(blocks[0]).querySelector("pre.shiki")).not.toBeNull();
      expect(required(blocks[1]).querySelector("pre.shiki")).not.toBeNull();
    });
    expect(required(blocks[2]).querySelector("pre.shiki")).toBeNull();
    expect(observer.unobserved.has(required(blocks[0]))).toBe(true);

    act(() => {
      observer.intersect(required(blocks[0]), false);
    });
    expect(required(blocks[0]).querySelector("pre.shiki")).not.toBeNull();

    view.rerender(
      <>
        <CodeBlock code="const first = 10" lang="ts" {...markdownLabels.code} />
        <CodeBlock code="const second = 2" lang="ts" {...markdownLabels.code} />
        <CodeBlock code="const third = 3" lang="ts" {...markdownLabels.code} />
      </>,
    );
    expect(required(blocks[0]).querySelector("pre.shiki")?.textContent).toBe(
      "const first = 10",
    );

    act(() => {
      observer.intersect(required(blocks[2]), true);
    });
    await waitFor(() => {
      expect(required(blocks[2]).querySelector("pre.shiki")).not.toBeNull();
    });
    expect(observer.disconnected).toBe(true);
  });

  it("does not observe an unsupported language", () => {
    const view = render(
      <CodeBlock
        code="IDENTIFICATION DIVISION."
        lang="cobol"
        {...markdownLabels.code}
      />,
    );
    expect(view.container.querySelector("pre.shiki")).toBeNull();
    expect(IntersectionObserverStub.instances).toHaveLength(0);
  });

  it("releases the shared observer when the last pending block unmounts", () => {
    const view = render(
      <CodeBlock
        code="const pending = true"
        lang="ts"
        {...markdownLabels.code}
      />,
    );
    const block = required(
      view.container.querySelector('[data-streamdown="code-block"]'),
    );
    const observer = required(IntersectionObserverStub.instances[0]);

    view.unmount();

    expect(observer.unobserved.has(block)).toBe(true);
    expect(observer.disconnected).toBe(true);
  });

  it("highlights immediately when IntersectionObserver is unavailable", () => {
    vi.stubGlobal("IntersectionObserver", undefined);
    const view = render(
      <CodeBlock
        code="const fallback = true"
        lang="ts"
        {...markdownLabels.code}
      />,
    );
    expect(view.container.querySelector("pre.shiki")).not.toBeNull();
  });

  it("keeps an intersecting streaming block plain until its lazy grammar loads", async () => {
    const view = render(
      <CodeBlock
        code="print(1)"
        lang="python"
        streaming
        {...markdownLabels.code}
      />,
    );
    const block = required(
      view.container.querySelector('[data-streamdown="code-block"]'),
    );
    const observer = required(IntersectionObserverStub.instances[0]);

    act(() => {
      observer.intersect(block, true);
    });
    expect(block.querySelector("pre.shiki")).toBeNull();

    await waitFor(
      () => {
        expect(block.querySelector("pre.shiki")).not.toBeNull();
      },
      { timeout: 5_000 },
    );
  });
  it("隐藏呈现释放未激活的observer，恢复后重新等待可视区域", () => {
    const view = render(
      <MarkdownVisibility value={true}>
        <CodeBlock
          code="const pending = true"
          lang="ts"
          {...markdownLabels.code}
        />
      </MarkdownVisibility>,
    );
    const first = required(IntersectionObserverStub.instances[0]);
    expect(first.observed.size).toBe(1);
    view.rerender(
      <MarkdownVisibility value={false}>
        <CodeBlock
          code="const pending = true"
          lang="ts"
          {...markdownLabels.code}
        />
      </MarkdownVisibility>,
    );
    expect(first.disconnected).toBe(true);
    view.rerender(
      <MarkdownVisibility value={true}>
        <CodeBlock
          code="const pending = true"
          lang="ts"
          {...markdownLabels.code}
        />
      </MarkdownVisibility>,
    );
    expect(IntersectionObserverStub.instances).toHaveLength(2);
    expect(required(IntersectionObserverStub.instances[1]).observed.size).toBe(
      1,
    );
  });
});
