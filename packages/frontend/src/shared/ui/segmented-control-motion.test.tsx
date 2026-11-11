import type * as Motion from "motion/react";
import { StrictMode } from "react";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { ContentVisibility } from "@/shared/lib/content-visibility";

import { SegmentedControl } from "./segmented-control";

const preference = vi.hoisted(() => ({ reduced: false }));
vi.mock("motion/react", async (importOriginal) => ({
  ...(await importOriginal<typeof Motion>()),
  useReducedMotion: () => preference.reduced,
}));

const options = [
  { label: "伙伴", value: "waker" },
  { label: "团队", value: "team" },
];
let teamWidth = 100;
let ancestorScale = 1;
let optionTop = 3;
let optionHeight = 30;
let trackHeight = 36;
let resize: (() => void) | undefined;

beforeEach(() => {
  preference.reduced = false;
  teamWidth = 100;
  ancestorScale = 1;
  optionTop = 3;
  optionHeight = 30;
  trackHeight = 36;
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(
    function (this: HTMLElement) {
      return this.getAttribute("role") === "radiogroup" ? 106 + teamWidth : 0;
    },
  );
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(
    function (this: HTMLElement) {
      return this.getAttribute("role") === "radiogroup" ? trackHeight : 0;
    },
  );
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    function (this: HTMLElement) {
      if (this.getAttribute("role") === "radiogroup") {
        return new DOMRect(
          20 * ancestorScale,
          10 * ancestorScale,
          (106 + teamWidth) * ancestorScale,
          trackHeight * ancestorScale,
        );
      }
      if (this.getAttribute("role") === "radio") {
        return this.textContent === "伙伴"
          ? new DOMRect(
              23 * ancestorScale,
              (10 + optionTop) * ancestorScale,
              100 * ancestorScale,
              optionHeight * ancestorScale,
            )
          : new DOMRect(
              123 * ancestorScale,
              (10 + optionTop) * ancestorScale,
              teamWidth * ancestorScale,
              optionHeight * ancestorScale,
            );
      }
      return new DOMRect();
    },
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        resize = callback;
      }
      observe() {}
      disconnect() {}
    },
  );
  document.documentElement.style.setProperty("--duration-standard", "220ms");
  document.documentElement.style.setProperty("--duration-quick", "140ms");
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("style");
  resize = undefined;
});

function highlight() {
  const node = screen
    .getByRole("radiogroup")
    .querySelector<HTMLElement>("[data-segmented-highlight]");
  if (!node) throw new Error("分段 Tab 高亮未挂载");
  return node;
}

it("资源表单高亮测量真实 28px 选项，不继承 shell 命中框或裁切圆角", async () => {
  optionHeight = 28;
  trackHeight = 34;
  render(
    <SegmentedControl
      appearance="catalog-form"
      ariaLabel="模式"
      options={options}
      value="waker"
      onValueChange={() => {}}
    />,
  );
  await waitFor(() => {
    expect(highlight()).toHaveStyle({ opacity: "1" });
    expect(highlight().style.clipPath).toBe(
      "inset(3px 103px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    );
  });
  expect(highlight()).not.toHaveClass("segmented-control-shell");
});

it("StrictMode 初始化时高亮仅覆盖默认选项", async () => {
  render(
    <StrictMode>
      <SegmentedControl
        appearance="shell"
        ariaLabel="模式"
        options={options}
        value="waker"
        onValueChange={() => {}}
      />
    </StrictMode>,
  );
  await waitFor(() => {
    expect(highlight()).toHaveStyle({ opacity: "1" });
    expect(highlight().style.clipPath).toBe(
      "inset(3px 103px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    );
  });
});

it("首次定位到当前选项，切换时先拉伸再收拢到目标选项", async () => {
  const { rerender } = render(
    <SegmentedControl
      ariaLabel="模式"
      options={options}
      value="waker"
      onValueChange={() => {}}
    />,
  );
  const thumb = highlight();
  expect(thumb).toHaveAttribute("aria-hidden", "true");
  await waitFor(() =>
    expect(thumb.style.clipPath).toBe(
      "inset(3px 103px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
  rerender(
    <SegmentedControl
      ariaLabel="模式"
      options={options}
      value="team"
      onValueChange={() => {}}
    />,
  );
  await waitFor(() => {
    const edges = thumb.style.clipPath
      .match(/-?\d+(?:\.\d+)?px/g)
      ?.map(Number.parseFloat);
    expect(edges).toBeDefined();
    // 两个 100px 选项间移动时，拉伸阶段高亮应覆盖超过一个选项。
    expect(206 - (edges?.[1] ?? 0) - (edges?.[3] ?? 0)).toBeGreaterThan(120);
  });
  await waitFor(() => {
    expect(thumb.style.clipPath).toBe(
      "inset(3px 3px 3px 103px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    );
  });
});

it("快速反向切换最终跟随受控值，不被上一段回弹覆盖", async () => {
  const props = { ariaLabel: "模式", options, onValueChange: () => {} };
  const { rerender } = render(<SegmentedControl {...props} value="waker" />);
  rerender(<SegmentedControl {...props} value="team" />);
  await new Promise((resolve) => setTimeout(resolve, 60));
  rerender(<SegmentedControl {...props} value="waker" />);
  await waitFor(() => {
    expect(highlight().style.clipPath).toBe(
      "inset(3px 103px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    );
  });
});

it("减少动态效果时立即定位，选项宽度变化后重新对齐", async () => {
  preference.reduced = true;
  const props = { ariaLabel: "模式", options, onValueChange: () => {} };
  const { rerender } = render(<SegmentedControl {...props} value="waker" />);
  rerender(<SegmentedControl {...props} value="team" />);
  await waitFor(() =>
    expect(highlight().style.clipPath).toBe(
      "inset(3px 3px 3px 103px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
  teamWidth = 160;
  act(() => resize?.());
  await waitFor(() =>
    expect(highlight().style.clipPath).toBe(
      "inset(3px 3px 3px 103px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
  rerender(<SegmentedControl {...props} value="waker" />);
  await waitFor(() =>
    expect(highlight().style.clipPath).toBe(
      "inset(3px 163px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
});

it("动效层不重复可访问选项，禁用整个控件后无法点击或键盘切换", () => {
  const onValueChange = vi.fn();
  render(
    <SegmentedControl
      ariaLabel="模式"
      options={options}
      value="waker"
      onValueChange={onValueChange}
      disabled
    />,
  );
  expect(screen.getAllByRole("radio")).toHaveLength(2);
  const waker = screen.getByRole("radio", { name: "伙伴" });
  fireEvent.click(waker);
  fireEvent.keyDown(waker, { key: "ArrowRight" });
  expect(onValueChange).not.toHaveBeenCalled();
});

it("隐藏 Scene 时停止高亮，恢复后直接对齐最新的受控选项", async () => {
  const content = (visible: boolean, value: string) => (
    <ContentVisibility value={visible}>
      <SegmentedControl
        ariaLabel="模式"
        options={options}
        value={value}
        onValueChange={() => {}}
      />
    </ContentVisibility>
  );
  const { rerender } = render(content(true, "waker"));
  await waitFor(() => expect(highlight()).toHaveStyle({ opacity: "1" }));
  rerender(content(false, "team"));
  await waitFor(() => expect(highlight()).toHaveStyle({ opacity: "0" }));
  rerender(content(true, "team"));
  await waitFor(() =>
    expect(highlight().style.clipPath).toBe(
      "inset(3px 3px 3px 103px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
  expect(highlight()).toHaveStyle({ opacity: "1" });
});

it("祖先缩放时仍使用控件本地坐标定位高亮", async () => {
  ancestorScale = 0.5;
  render(
    <SegmentedControl
      ariaLabel="模式"
      options={options}
      value="waker"
      onValueChange={() => {}}
    />,
  );
  await waitFor(() =>
    expect(highlight().style.clipPath).toBe(
      "inset(3px 103px 3px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
    ),
  );
});

it.each([1, 0.5])(
  "shell 在祖先缩放 %s 时保持选项横向定位与 36px 命中区",
  async (scale) => {
    preference.reduced = true;
    ancestorScale = scale;
    optionTop = 0;
    optionHeight = 36;
    const props = {
      ariaLabel: "模式",
      options,
      onValueChange: () => {},
      appearance: "shell" as const,
    };
    const { rerender } = render(<SegmentedControl {...props} value="waker" />);
    await waitFor(() =>
      expect(highlight().style.clipPath).toBe(
        "inset(0px 103px 0px 3px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
      ),
    );
    // 视觉高亮由 shell CSS 自身的 24px 表面裁切；Motion 只沿真实命中框定位横向边界。
    expect(
      screen.getByRole("radio", { name: "伙伴" }).getBoundingClientRect()
        .height / scale,
    ).toBe(36);
    rerender(<SegmentedControl {...props} value="team" />);
    await waitFor(() =>
      expect(highlight().style.clipPath).toBe(
        "inset(0px 3px 0px 103px round var(--segmented-highlight-radius, var(--radius-control-inner)))",
      ),
    );
  },
);
