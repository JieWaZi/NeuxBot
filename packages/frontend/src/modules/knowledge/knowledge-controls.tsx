import { cloneElement, isValidElement, useId, type ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { CircleHelp } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import { EmptyState } from "@/shared/ui/empty-state";
import { FieldLabel } from "@/shared/ui/field-label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

export function IconAction({
  icon: Icon,
  label,
  onClick,
  disabled = false,
}: {
  icon: LucideIcon;
  label: string;
  onClick?: () => void;
  disabled?: boolean;
}) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={label}
            onClick={onClick}
            disabled={disabled}
          >
            <Icon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
export function Pill({
  children,
  tone = "muted",
}: {
  children: ReactNode;
  tone?: string;
}) {
  const variant =
    tone === "pending" || tone === "queued" || tone === "processing"
      ? "warning"
      : tone === "processed"
        ? "success"
        : tone === "failed"
          ? "destructive"
          : "secondary";
  return <Badge variant={variant}>{children}</Badge>;
}
export function Empty({
  icon: Icon,
  title,
  description,
  children,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <EmptyState
      className="min-h-0 py-8"
      icon={Icon}
      title={title}
      description={description}
      actions={children}
    />
  );
}
function FieldLabelTip({ tip }: { tip: string }) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="text-content-muted hover:text-content-secondary size-control-sm focus-visible:ring-ring inline-grid shrink-0 place-items-center rounded-full outline-none focus-visible:ring-2"
            aria-label={tip}
          >
            <CircleHelp className="size-icon-sm" strokeWidth={1.5} />
          </button>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs text-pretty">
          {tip}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

export function Field({
  label,
  hint,
  labelTip,
  required,
  children,
}: {
  label: string;
  hint?: string;
  labelTip?: string;
  required?: boolean;
  children: ReactNode;
}) {
  const id = useId();
  const fieldRequired =
    required ??
    (isValidElement<{ required?: boolean }>(children) &&
      Boolean(children.props.required));
  const field = isValidElement<{
    id?: string;
    "aria-describedby"?: string;
    "aria-required"?: boolean;
  }>(children)
    ? cloneElement(children, {
        id: children.props.id ?? id,
        "aria-required": fieldRequired || children.props["aria-required"],
        "aria-describedby":
          [children.props["aria-describedby"], hint ? `${id}-hint` : undefined]
            .filter(Boolean)
            .join(" ") || undefined,
      })
    : children;
  return (
    <div className="grid min-w-0 gap-2">
      <FieldLabel
        required={fieldRequired}
        htmlFor={
          isValidElement<{ id?: string }>(children)
            ? (children.props.id ?? id)
            : undefined
        }
        className="gap-1.5"
      >
        {label}
        {labelTip ? <FieldLabelTip tip={labelTip} /> : null}
      </FieldLabel>
      {field}
      {hint && (
        <p id={`${id}-hint`} className="text-content-muted type-ui">
          {hint}
        </p>
      )}
    </div>
  );
}

/** 表单一级分区标题。 */
export function SectionTitle({ title }: { title: string }) {
  return (
    <h3 className="type-title font-semibold tracking-[-0.01em]">{title}</h3>
  );
}

export function Choice({
  value,
  onChange,
  label,
  options,
  id,
  disabled,
  "aria-describedby": describedBy,
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  options: { value: string; label: string }[];
  id?: string;
  disabled?: boolean;
  "aria-describedby"?: string;
}) {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger id={id} aria-label={label} aria-describedby={describedBy}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
