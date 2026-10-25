import type { ButtonHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

type SwitchProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "aria-checked" | "role"
> & {
  checked: boolean;
  hitArea?: "default" | "comfortable";
};

export function Switch({
  checked,
  hitArea = "default",
  className,
  disabled,
  ...props
}: SwitchProps) {
  const thumb = (
    <span
      aria-hidden="true"
      className={cn(
        "bg-control-switch-thumb duration-quick ease-ui size-3 rounded-full transition-transform",
        checked ? "translate-x-3.5" : "translate-x-0.5",
      )}
    />
  );
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      className={cn(
        "focus-visible:ring-ring focus-visible:ring-offset-canvas duration-quick ease-ui relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-45",
        hitArea === "comfortable"
          ? "size-control-md justify-center bg-transparent"
          : checked
            ? "bg-control-switch-track-checked"
            : "bg-control-switch-track",
        className,
      )}
      {...props}
    >
      {hitArea === "comfortable" ? (
        <span
          aria-hidden="true"
          className={cn(
            "inline-flex h-4 w-7 shrink-0 items-center rounded-full",
            checked
              ? "bg-control-switch-track-checked"
              : "bg-control-switch-track",
          )}
        >
          {thumb}
        </span>
      ) : (
        thumb
      )}
    </button>
  );
}
