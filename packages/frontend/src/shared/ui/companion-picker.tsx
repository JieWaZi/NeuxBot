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
}: {
  label: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation("common");
  const appearance = companionAppearance(value);
  const avatar = encodeCompanionAppearance(appearance);
  const change = (patch: Partial<CompanionAppearance>) =>
    onChange(encodeCompanionAppearance({ ...appearance, ...patch }));

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
          <Avatar
            name={name}
            src={avatar}
            size="xl"
            preview
            interactive={false}
          />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          variant="selection"
          aria-label={t("companion.picker.edit", { label })}
        >
          <div className="grid gap-3">
            <fieldset className="grid min-w-0 gap-1">
              <legend className="sr-only">{t("companion.picker.shape")}</legend>
              <div className="flex flex-wrap items-center justify-between gap-1">
                <span
                  aria-hidden="true"
                  className="type-ui text-content-secondary font-medium"
                >
                  {t("companion.picker.shape")}
                </span>
                <fieldset
                  aria-label={t("companion.picker.material")}
                  className="flex gap-1"
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
                      <TooltipContent>
                        {t("companion.picker.reset")}
                      </TooltipContent>
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
                      <TooltipContent>
                        {t("companion.picker.random")}
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </TooltipProvider>
              </div>
              <div className="grid grid-cols-5 gap-1">
                {avatarShapes.map(({ id }) => (
                  <Button
                    key={id}
                    type="button"
                    size="icon"
                    variant="choice"
                    className="relative w-full"
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
                      size="sm"
                      preview
                      interactive={false}
                    />
                  </Button>
                ))}
              </div>
            </fieldset>

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

            <fieldset className="grid min-w-0 gap-1">
              <legend className="type-ui text-content-secondary mb-1 font-medium">
                {t("companion.picker.color")}
              </legend>
              <div className="grid grid-cols-9 gap-0.5">
                {avatarColors.map((color) => (
                  <Button
                    key={color}
                    type="button"
                    size="icon-sm"
                    variant="choice"
                    className="relative w-full"
                    aria-label={t("companion.picker.colorOption", { color })}
                    title={t("companion.picker.colorOption", { color })}
                    aria-pressed={appearance.color === color}
                    onClick={() => change({ color })}
                  >
                    <span
                      className="size-icon-lg rounded-full"
                      style={{ backgroundColor: color }}
                    />
                  </Button>
                ))}
              </div>
            </fieldset>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
