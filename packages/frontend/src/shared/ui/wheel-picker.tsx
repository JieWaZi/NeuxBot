import {
  WheelPicker,
  WheelPickerWrapper,
  type WheelPickerOption,
} from "@ncdai/react-wheel-picker";
import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";

export function WheelPickerGroup({ children }: { children: ReactNode }) {
  return <WheelPickerWrapper className="gap-3">{children}</WheelPickerWrapper>;
}

/** 无样式滚轮的主题和无障碍适配，业务页面不依赖第三方 DOM。 */
export function WheelPickerColumn({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: WheelPickerOption<string>[];
  onChange: (value: string) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const typed = useRef({ text: "", at: 0 });
  const [itemHeight, setItemHeight] = useState(30);
  // 滚轮的几何计算需要像素值，测量共享行高 Token，避免业务层另设尺寸。
  const measureRow = useCallback((element: HTMLSpanElement | null) => {
    if (element?.offsetHeight) setItemHeight(element.offsetHeight);
  }, []);
  // 键盘立即提交数值，保证快速输入后关闭弹层也保留完整选值；
  // 滚动和拖动交给滚轮核心，左右键保留列间焦点移动。
  const handleKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== root.current?.querySelector("[data-rwp]")) return;
    const enabled = options.filter((option) => !option.disabled);
    const index = enabled.findIndex((option) => option.value === value);
    let next: string | undefined;
    if (["ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) {
      typed.current = { text: "", at: 0 };
      const position =
        event.key === "Home"
          ? 0
          : event.key === "End"
            ? enabled.length - 1
            : Math.max(
                0,
                Math.min(
                  enabled.length - 1,
                  index + (event.key === "ArrowDown" ? 1 : -1),
                ),
              );
      next = enabled[position]?.value;
    } else if (
      /^\d$/.test(event.key) &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey
    ) {
      const at = Date.now();
      const previous = at - typed.current.at < 500 ? typed.current.text : "";
      const text = previous.length < 2 ? previous + event.key : event.key;
      typed.current = { text, at };
      const candidates =
        text.length === 1
          ? [...enabled.slice(index + 1), ...enabled.slice(0, index + 1)]
          : enabled;
      next = candidates.find((option) => option.value.startsWith(text))?.value;
    } else return;
    event.preventDefault();
    event.stopPropagation();
    if (next) onChange(next);
  };
  // 上游尚未暴露 ARIA props，用其公开 data-rwp 标记补齐可聚焦的 spinbutton。
  useLayoutEffect(() => {
    const wheel = root.current?.querySelector<HTMLElement>("[data-rwp]");
    if (!wheel) return;
    wheel.setAttribute("role", "spinbutton");
    wheel.setAttribute("aria-label", label);
    wheel.setAttribute("aria-valuemin", options[0]?.value || "0");
    wheel.setAttribute("aria-valuemax", options.at(-1)?.value || "0");
    wheel.setAttribute("aria-valuenow", String(Number(value)));
    wheel.setAttribute("aria-valuetext", value);
    // 两份视觉列表（滚动、选中）不应重复朗读。
    wheel
      .querySelectorAll("ul")
      .forEach((list) => list.setAttribute("aria-hidden", "true"));
  }, [label, options, value]);
  return (
    <div
      ref={root}
      className="relative min-w-0 flex-1"
      onKeyDownCapture={handleKey}
      onBlurCapture={() => {
        typed.current = { text: "", at: 0 };
      }}
    >
      <span
        ref={measureRow}
        aria-hidden
        className="h-row-compact pointer-events-none invisible absolute block"
      />
      <p className="type-ui text-content-secondary mb-2 text-center">{label}</p>
      <div className="[&_[data-rwp]:focus-visible]:bg-action-quiet-hover [&_[data-rwp]:focus-visible]:ring-ring [&_[data-rwp]]:rounded-md [&_[data-rwp]:focus-visible]:ring-2 [&_[data-rwp]:focus-visible]:ring-inset">
        <WheelPicker
          value={value}
          options={options}
          onValueChange={onChange}
          visibleCount={16}
          optionItemHeight={itemHeight}
          scrollSensitivity={100}
          classNames={{
            optionItem: "type-body text-content-secondary tabular-nums",
            highlightWrapper: "bg-selection-surface rounded-md",
            highlightItem:
              "type-body text-content-primary font-medium tabular-nums",
          }}
        />
      </div>
    </div>
  );
}
