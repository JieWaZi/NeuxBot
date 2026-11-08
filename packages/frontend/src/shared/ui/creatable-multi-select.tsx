import { Check, Plus, X } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { cn } from "@/shared/lib/cn";
import { consumeOverlayWheelScroll } from "@/shared/lib/overlay-scroll-wheel";
import { Skeleton } from "./skeleton";
import { Badge } from "./badge";
import { Button } from "./button";
import { Popover, PopoverAnchor, PopoverContent } from "./popover";

type Props = {
  id?: string;
  "aria-describedby"?: string;
  label: string;
  placeholder: string;
  value: readonly string[];
  options: readonly string[];
  onValueChange: (value: string[]) => void;
  createLabel: (name: string) => string;
  removeLabel: (name: string) => string;
  emptyLabel: string;
  loading?: boolean;
  loadingLabel?: string;
  disabled?: boolean;
  appearance?: "default" | "catalog";
};

/** 多选标签仅在显式确认后加入草稿；复合字段只保留外层边界，内层不重复焦点外框。 */
export function CreatableMultiSelect({
  id,
  "aria-describedby": describedBy,
  label,
  placeholder,
  value,
  options,
  onValueChange,
  createLabel,
  removeLabel,
  emptyLabel,
  loading = false,
  loadingLabel,
  disabled = false,
  appearance = "default",
}: Props) {
  const listId = useId();
  const field = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const composing = useRef(false);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(-1);
  const name = query.trim();
  const choices = [...new Set([...options, ...value])];
  const visible = choices.filter((item) =>
    item.toLocaleLowerCase().includes(name.toLocaleLowerCase()),
  );
  const canCreate = Boolean(name && !choices.includes(name));
  const count = visible.length + Number(canCreate);
  useEffect(() => {
    if (open && active >= 0)
      document
        .getElementById(`${listId}-${active}`)
        ?.scrollIntoView({ block: "nearest" });
  }, [open, active, listId]);
  const choose = (item: string) => {
    onValueChange(
      value.includes(item)
        ? value.filter((current) => current !== item)
        : [...value, item],
    );
    setQuery("");
    setActive(-1);
    input.current?.focus();
  };
  const keepInputFocus = (event: {
    target: EventTarget | null;
    preventDefault: () => void;
  }) => {
    if (event.target instanceof Node && field.current?.contains(event.target))
      event.preventDefault();
  };
  return (
    <Popover open={open && !disabled} onOpenChange={setOpen}>
      <PopoverAnchor asChild>
        <div
          ref={field}
          className={cn(
            "flex min-w-0 flex-wrap items-center gap-1 rounded-md border py-1",
            appearance === "catalog"
              ? "border-resource-border-strong bg-resource-control focus-within:border-resource-muted min-h-control-catalog px-3"
              : "border-field-border bg-field hover:border-field-border-hover focus-within:border-field-border-active min-h-control-sm px-2",
            disabled && "opacity-50",
          )}
        >
          {value.map((item) => (
            <Badge
              key={item}
              variant="secondary"
              className="max-w-full min-w-0"
            >
              <span className="truncate" title={item}>
                {item}
              </span>
              <button
                type="button"
                className="focus-visible:ring-ring shrink-0 rounded-sm outline-none focus-visible:ring-2"
                disabled={disabled}
                aria-label={removeLabel(item)}
                title={removeLabel(item)}
                onClick={() => {
                  onValueChange(value.filter((current) => current !== item));
                  input.current?.focus();
                }}
              >
                <X className="size-icon-xs" />
              </button>
            </Badge>
          ))}
          <input
            ref={input}
            id={id}
            role="combobox"
            aria-label={label}
            aria-describedby={describedBy}
            aria-expanded={open && !disabled}
            aria-controls={open ? listId : undefined}
            aria-autocomplete="list"
            aria-activedescendant={
              open && active >= 0 ? `${listId}-${active}` : undefined
            }
            className={cn(
              "min-w-24 flex-1 bg-transparent outline-none focus-visible:outline-none",
              appearance === "catalog"
                ? "type-label text-resource-content placeholder:text-resource-placeholder"
                : "placeholder:text-content-muted type-ui text-foreground",
            )}
            placeholder={placeholder}
            disabled={disabled}
            value={query}
            onFocus={() => setOpen(true)}
            onClick={() => setOpen(true)}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(-1);
              setOpen(true);
            }}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
            }}
            onKeyDown={(event) => {
              if (
                composing.current ||
                event.nativeEvent.isComposing ||
                event.keyCode === 229
              ) {
                // 保留输入法的原生确认行为，只阻断父级快捷键。
                if (event.key === "Enter") event.stopPropagation();
                return;
              }
              if (event.key === "Enter") {
                event.preventDefault();
                event.stopPropagation();
                const item =
                  active >= 0
                    ? visible[active]
                    : choices.find((item) => item === name);
                if (item) {
                  // 精确输入已选标签时只清空搜索，不意外取消已有选择。
                  if (active < 0 && value.includes(item)) setQuery("");
                  else choose(item);
                } else if (name && (active < 0 || active === visible.length))
                  choose(name);
                setOpen(true);
              } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                setOpen(true);
                if (count)
                  setActive((current) =>
                    event.key === "ArrowDown"
                      ? (current + 1) % count
                      : current <= 0
                        ? count - 1
                        : current - 1,
                  );
              } else if (event.key === "Escape" && open) {
                event.preventDefault();
                event.stopPropagation();
                setOpen(false);
              } else if (event.key === "Tab") setOpen(false);
              else if (event.key === "Backspace" && !query && value.length)
                onValueChange(value.slice(0, -1));
            }}
          />
        </div>
      </PopoverAnchor>
      <PopoverContent
        width="trigger"
        align="start"
        className="p-1"
        onOpenAutoFocus={(event) => event.preventDefault()}
        onCloseAutoFocus={(event) => event.preventDefault()}
        onFocusOutside={keepInputFocus}
        onPointerDownOutside={keepInputFocus}
      >
        <div
          id={listId}
          role="listbox"
          aria-label={label}
          aria-multiselectable="true"
          className="max-h-56 overflow-y-auto overscroll-contain"
          onWheel={consumeOverlayWheelScroll}
        >
          {visible.map((item, index) => (
            <Button
              key={item}
              id={`${listId}-${index}`}
              type="button"
              role="option"
              tabIndex={-1}
              aria-selected={value.includes(item)}
              size="option"
              variant="ghost"
              className={cn(active === index && "bg-action-quiet-hover")}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(item)}
            >
              <Check
                className={cn("shrink-0", !value.includes(item) && "invisible")}
              />
              <span className="min-w-0 break-words">{item}</span>
            </Button>
          ))}
          {canCreate && (
            <Button
              id={`${listId}-${visible.length}`}
              type="button"
              role="option"
              tabIndex={-1}
              aria-selected="false"
              size="option"
              variant="ghost"
              className={cn(
                active === visible.length && "bg-action-quiet-hover",
              )}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(name)}
            >
              <Plus className="shrink-0" />
              <span className="min-w-0 break-words">{createLabel(name)}</span>
            </Button>
          )}
          {loading && (
            <div
              role="status"
              aria-busy="true"
              aria-label={loadingLabel}
              className="grid gap-2 p-2"
            >
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          )}
          {!count && !loading && (
            <p className="text-content-secondary type-ui px-2 py-3">
              {emptyLabel}
            </p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
