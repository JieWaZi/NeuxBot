import { cva, type VariantProps } from "class-variance-authority";
import { Search, X } from "lucide-react";
import {
  useRef,
  useState,
  type ChangeEvent,
  type InputHTMLAttributes,
} from "react";

import { cn } from "@/shared/lib/cn";

const searchFieldVariants = cva(
  "border-field-border bg-field text-content-muted duration-quick ease-ui flex items-center rounded-md border transition-[background-color,border-color] hover:border-field-border-hover hover:bg-field-hover focus-within:border-field-border-active",
  {
    variants: {
      size: {
        sm: "h-control-search-sm gap-1.5 px-2 type-ui [&_svg]:size-icon-sm",
        md: "h-control-md gap-2 px-3 type-body [&_svg]:size-icon-md",
        lg: "h-control-lg gap-2 px-3 type-label [&_svg]:size-icon-md",
      },
    },
    defaultVariants: {
      size: "sm",
    },
  },
);

const searchFieldWidthVariants = cva("", {
  variants: {
    width: {
      auto: "w-auto max-w-full",
      full: "w-full max-w-full",
      toolbar:
        "min-w-field-search-min max-w-field-search basis-field-search-min grow-[4]",
    },
  },
  defaultVariants: {
    width: "auto",
  },
});

type SearchFieldProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "size" | "type"
> &
  VariantProps<typeof searchFieldVariants> & {
    containerClassName?: string;
    /** 清除按钮的无障碍标签。 */
    clearLabel?: string;
  } & VariantProps<typeof searchFieldWidthVariants>;

export function SearchField({
  className,
  clearLabel = "Clear",
  containerClassName,
  defaultValue,
  onChange,
  size,
  value,
  width,
  ...props
}: SearchFieldProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const isControlled = value !== undefined;
  const [uncontrolledValue, setUncontrolledValue] = useState(
    () => defaultValue?.toString() ?? "",
  );
  const currentValue = isControlled
    ? (value?.toString() ?? "")
    : uncontrolledValue;
  const hasValue = currentValue.length > 0;

  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (!isControlled) {
      setUncontrolledValue(event.target.value);
    }
    onChange?.(event);
  };

  const handleClear = () => {
    const input = inputRef.current;
    if (!input) return;
    if (!isControlled) {
      input.value = "";
      setUncontrolledValue("");
    }
    onChange?.({
      target: { ...input, value: "" },
      currentTarget: { ...input, value: "" },
    } as ChangeEvent<HTMLInputElement>);
    input.focus();
  };

  return (
    <label
      className={cn(
        searchFieldVariants({ size }),
        searchFieldWidthVariants({ width }),
        containerClassName,
      )}
    >
      <Search className="shrink-0" strokeWidth={1.5} aria-hidden="true" />
      <input
        ref={inputRef}
        type="text"
        role="searchbox"
        className={cn(
          "placeholder:text-content-muted text-foreground min-w-0 flex-1 bg-transparent outline-none",
          className,
        )}
        defaultValue={defaultValue}
        onChange={handleChange}
        value={isControlled ? value : undefined}
        {...props}
      />
      {hasValue ? (
        <button
          type="button"
          aria-label={clearLabel}
          onClick={handleClear}
          className="text-content-muted hover:bg-action-quiet-hover hover:text-content-primary focus-visible:ring-ring duration-quick size-icon-md grid shrink-0 place-items-center rounded-full outline-none focus-visible:ring-2"
        >
          <X className="size-icon-xs" strokeWidth={1.5} aria-hidden="true" />
        </button>
      ) : null}
    </label>
  );
}
