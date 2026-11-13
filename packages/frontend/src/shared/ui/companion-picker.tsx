import { RotateCcw, Shuffle } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  avatarColors,
  avatarExpressions,
  avatarMaterials,
  avatarShapes,
  companionAppearance,
  defaultCompanionAvatar,
  encodeCompanionAppearance,
  randomCompanionAvatar,
  type CompanionAppearance,
} from "@/shared/companion/appearance";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "./avatar";
import { Button } from "./button";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./tooltip";

export function CompanionPicker({
  label,
  name,
  value,
  onChange,
  inline = false,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  inline?: boolean;
}) {
  const { t } = useTranslation("common");
  const appearance = companionAppearance(value);
  const avatar = encodeCompanionAppearance(appearance);
  const change = (patch: Partial<CompanionAppearance>) =>
    onChange(encodeCompanionAppearance({ ...appearance, ...patch }));

  const preview = (
    <Avatar
      name={name}
      src={avatar}
      size={inline ? "hero" : "xl"}
      preview
      interactive={false}
    />
  );
  const materials = (
    <fieldset
      aria-label={t("companion.picker.material")}
      className={cn("flex gap-1", inline && "justify-center")}
    >
      {avatarMaterials.map((material) => (
        <Button
          key={material}
          type="button"
          size="sm"
          variant="choice"
          aria-pressed={appearance.material === material}
          onClick={() => change({ material })}
        >
          {t(`companion.materials.${material}`)}
        </Button>
      ))}
    </fieldset>
  );
  const shapes = (
    <fieldset className="grid min-w-0 gap-1">
      <legend className="sr-only">{t("companion.picker.shape")}</legend>
      {!inline && (
        <div className="flex flex-wrap items-center justify-between gap-1">
          <span
            aria-hidden="true"
            className="type-ui text-content-secondary font-medium"
          >
            {t("companion.picker.shape")}
          </span>
          {materials}
          <TooltipProvider>
            <div className="flex items-center">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    aria-label={t("companion.picker.reset")}
                    onClick={() => onChange(defaultCompanionAvatar)}
                  >
                    <RotateCcw />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("companion.picker.reset")}</TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    aria-label={t("companion.picker.random")}
                    onClick={() =>
                      onChange(randomCompanionAvatar(appearance.material))
                    }
                  >
                    <Shuffle />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("companion.picker.random")}</TooltipContent>
              </Tooltip>
            </div>
          </TooltipProvider>
        </div>
      )}
      <div
        className={
          inline
            ? "flex flex-wrap justify-center gap-1"
            : "grid grid-cols-5 gap-1"
        }
      >
        {avatarShapes.map(({ id }) => (
          <Button
            key={id}
            type="button"
            size={inline ? "icon-sm" : "icon"}
            variant="choice"
            className={inline ? undefined : "relative w-full"}
            aria-label={t(`companion.shapes.${id}`)}
            title={t(`companion.shapes.${id}`)}
            aria-pressed={appearance.shape === id}
            onClick={() => change({ shape: id })}
          >
            <Avatar
              name={name}
              src={encodeCompanionAppearance({
                ...appearance,
                shape: id,
              })}
              size={inline ? "xs" : "sm"}
              animated={!inline}
              preview
              interactive={false}
            />
          </Button>
        ))}
      </div>
    </fieldset>
  );
  const colors = (
    <fieldset className="grid min-w-0 gap-1">
      <legend
        className={
          inline ? "sr-only" : "type-ui text-content-secondary mb-1 font-medium"
        }
      >
        {t("companion.picker.color")}
      </legend>
      <div
        className={
          inline
            ? "mx-auto flex max-w-[17.75rem] flex-wrap justify-center gap-1"
            : "grid grid-cols-9 gap-0.5"
        }
      >
        {avatarColors.map((color) => (
          <Button
            key={color}
            type="button"
            size="icon-sm"
            variant="choice"
            shape={inline ? "circle" : undefined}
            className={inline ? undefined : "relative w-full"}
            aria-label={t("companion.picker.colorOption", { color })}
            title={t("companion.picker.colorOption", { color })}
            aria-pressed={appearance.color === color}
            onClick={() => change({ color })}
          >
            <span
              className={cn(
                inline ? "size-icon-md" : "size-icon-lg",
                "rounded-full",
                inline &&
                  appearance.color === color &&
                  "outline-ring outline outline-offset-2",
              )}
              style={{ backgroundColor: color }}
            />
          </Button>
        ))}
      </div>
    </fieldset>
  );
  const controls = (
    <div className="grid gap-3">
      {shapes}

      <fieldset className="grid min-w-0 gap-1">
        <legend className="type-ui text-content-secondary mb-1 font-medium">
          {t("companion.picker.expression")}
        </legend>
        <div className="grid grid-cols-4 gap-1">
          {avatarExpressions.map(({ id }) => (
            <Button
              key={id}
              type="button"
              size="icon"
              variant="choice"
              className="relative w-full"
              aria-label={t(`companion.expressions.${id}`)}
              title={t(`companion.expressions.${id}`)}
              aria-pressed={appearance.expression === id}
              onClick={() => change({ expression: id })}
            >
              <Avatar
                name={name}
                src={encodeCompanionAppearance({
                  ...appearance,
                  expression: id,
                })}
                size="sm"
                preview
                interactive={false}
              />
            </Button>
          ))}
        </div>
      </fieldset>

      {colors}
    </div>
  );

  if (inline)
    return (
      <fieldset className="grid min-w-0 gap-3">
        <legend className="sr-only">{label}</legend>
        <div className="justify-self-center">{preview}</div>
        {colors}
        {shapes}
        {materials}
      </fieldset>
    );

  return (
    <div className="grid content-start gap-2">
      <span className="type-label font-medium">{label}</span>
      <Popover>
        <PopoverTrigger
          type="button"
          aria-label={t("companion.picker.edit", { label })}
          title={t("companion.picker.edit", { label })}
          className="focus-visible:ring-ring w-fit cursor-pointer rounded-md outline-none focus-visible:ring-2"
        >
          {preview}
        </PopoverTrigger>
        <PopoverContent
          align="start"
          variant="selection"
          aria-label={t("companion.picker.edit", { label })}
        >
          {controls}
        </PopoverContent>
      </Popover>
    </div>
  );
}
