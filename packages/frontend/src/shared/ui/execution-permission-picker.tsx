import { ShieldAlert, ShieldKeyhole, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import {
  permissionLevelOf,
  permissionLevelToneClasses,
  permissionLevelTranslationKeys,
  permissionLevels,
  type PermissionLevel,
} from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

type ExecutionPermissionPickerProps = {
  value: PermissionLevel;
  supported: ReadonlySet<PermissionLevel>;
  disabled?: boolean;
  ariaLabel?: string;
  onChange: (value: PermissionLevel) => void;
};

const permissionLevelIcons = {
  default: ShieldKeyhole,
  auto: Sparkles,
  "full-access": ShieldAlert,
} satisfies Record<PermissionLevel, typeof ShieldKeyhole>;

export function ExecutionPermissionPicker({
  value,
  supported,
  disabled = false,
  ariaLabel,
  onChange,
}: ExecutionPermissionPickerProps) {
  const { t } = useTranslation("common");
  const selected = permissionLevelTranslationKeys[value];
  const selectedLabel = t(selected.label);
  const SelectedIcon = permissionLevelIcons[value];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          disabled={disabled || supported.size === 0}
          aria-label={
            ariaLabel ?? t("permission.pickerAria", { name: selectedLabel })
          }
        >
          <SelectedIcon
            className={cn(
              "size-icon-md",
              permissionLevelToneClasses[value].icon,
            )}
          />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <DropdownMenuRadioGroup
          value={value}
          onValueChange={(next) => {
            const level = permissionLevelOf(next);
            if (level && supported.has(level)) onChange(level);
          }}
        >
          {permissionLevels
            .filter((level) => supported.has(level))
            .map((level) => {
              const detail = permissionLevelTranslationKeys[level];
              const tone = permissionLevelToneClasses[level];
              const Icon = permissionLevelIcons[level];
              return (
                <DropdownMenuRadioItem
                  key={level}
                  value={level}
                  className="min-h-row-rich items-start py-2"
                >
                  <Icon className={cn("size-icon-sm mt-0.5", tone.icon)} />
                  <span className="min-w-0 flex-1">
                    <span
                      className={cn("type-ui block font-medium", tone.title)}
                    >
                      {t(detail.label)}
                    </span>
                    <span className="text-muted-foreground type-caption mt-0.5 block whitespace-normal">
                      {t(detail.description)}
                    </span>
                  </span>
                </DropdownMenuRadioItem>
              );
            })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
