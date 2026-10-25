import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";

type Props<T extends string> = {
  ariaLabel: string;
  onValueChange: (value: T) => void;
  options: ReadonlyArray<{ label: string; value: T }>;
  value: T;
};

export function ResourceFilterSelect<T extends string>({
  ariaLabel,
  onValueChange,
  options,
  value,
}: Props<T>) {
  return (
    <Select
      value={value}
      onValueChange={(next) => {
        const selected = options.find((option) => option.value === next);
        if (selected) onValueChange(selected.value);
      }}
    >
      <SelectTrigger size="sm" width="filter" aria-label={ariaLabel}>
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
