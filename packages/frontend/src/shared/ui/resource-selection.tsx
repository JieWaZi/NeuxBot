import { Trash2 } from "lucide-react";
import { useId } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "./button";
import { Checkbox } from "./checkbox";

export function ResourceSelection({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean | "indeterminate";
  disabled?: boolean;
  onChange: (value: boolean) => void;
}) {
  const { t } = useTranslation("common");
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <Checkbox
        id={id}
        size="sm"
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onChange(value === true)}
      />
      <label
        htmlFor={id}
        className="text-muted-foreground type-ui cursor-pointer"
      >
        {t("resourceSelection.select")}
      </label>
    </div>
  );
}

export function ResourceSelectionActions({
  count,
  onClear,
  onDelete,
}: {
  count: number;
  onClear: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation("common");
  if (!count) return null;
  return (
    <div className="bg-muted mt-3 flex flex-wrap items-center gap-2 rounded-md px-3 py-2">
      <p className="text-muted-foreground type-ui">
        {t("resourceSelection.count", { count })}
      </p>
      <Button size="sm" variant="danger" onClick={onDelete}>
        <Trash2 />
        {t("resourceSelection.delete")}
      </Button>
      <Button size="sm" variant="ghost" onClick={onClear}>
        {t("resourceSelection.clear")}
      </Button>
    </div>
  );
}
