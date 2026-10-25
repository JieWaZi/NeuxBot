import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type ComponentProps,
} from "react";

import { cn } from "@/shared/lib/cn";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

type OverflowTextProps = Omit<ComponentProps<"span">, "children"> & {
  children: string;
  tooltipSide?: ComponentProps<typeof TooltipContent>["side"];
};

export function OverflowText({
  children,
  className,
  tooltipSide = "top",
  ...props
}: OverflowTextProps) {
  const textRef = useRef<HTMLSpanElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const measure = useCallback(() => {
    const element = textRef.current;
    if (!element) return;
    setOverflowing(element.scrollWidth > element.clientWidth);
  }, []);

  useLayoutEffect(() => {
    measure();
    const element = textRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [children, measure]);

  const text = (
    <span
      ref={textRef}
      className={cn("block min-w-0 truncate", className)}
      {...props}
    >
      {children}
    </span>
  );

  if (!overflowing) return text;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{text}</TooltipTrigger>
      <TooltipContent side={tooltipSide}>{children}</TooltipContent>
    </Tooltip>
  );
}
