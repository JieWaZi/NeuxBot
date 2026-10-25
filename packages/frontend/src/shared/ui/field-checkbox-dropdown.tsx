import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import type { FieldControlTriggerProps } from "@/shared/ui/field-control-trigger";

type FieldCheckboxDropdownProps = {
  ariaLabel: string;
  options: readonly string[];
  value: readonly string[];
  onValueChange: (value: string[]) => void;
  triggerLabel: string;
  align?: "start" | "center" | "end";
  formatOptionLabel?: (option: string) => string;
} & Pick<FieldControlTriggerProps, "className" | "size" | "width">;

/** 表单列内的多选下拉，触发器与 Select 对齐，选项使用标准 Checkbox 菜单项。 */
export function FieldCheckboxDropdown({
  align = "end",
  ariaLabel,
  className,
  formatOptionLabel = (option) => option,
  onValueChange,
  options,
  size = "sm",
  triggerLabel,
  value,
  width = "full",
}: FieldCheckboxDropdownProps) {
  const selected = new Set(value);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <FieldControlTrigger
          size={size}
          width={width}
          className={className}
          aria-label={ariaLabel}
        >
          <span className="truncate">{triggerLabel}</span>
        </FieldControlTrigger>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align}>
        {options.map((option) => (
          <DropdownMenuCheckboxItem
            key={option}
            checked={selected.has(option)}
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={(checked) =>
              onValueChange(
                checked
                  ? options.filter((item) => [...value, option].includes(item))
                  : value.filter((item) => item !== option),
              )
            }
          >
            {formatOptionLabel(option)}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
