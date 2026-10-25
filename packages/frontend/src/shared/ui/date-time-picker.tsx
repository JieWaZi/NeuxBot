import { zhCN } from "date-fns/locale";
import { CalendarDays, Clock3 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { localDateFromInput, localDateToInput } from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import { Calendar } from "@/shared/ui/calendar";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { WheelPickerColumn, WheelPickerGroup } from "@/shared/ui/wheel-picker";

type PickerProps = {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  required?: boolean;
  "aria-label"?: string;
};

const timeOptions = (length: number) =>
  Array.from({ length }, (_, index) => {
    const value = String(index).padStart(2, "0");
    return { value, label: value };
  });
const hours = timeOptions(24);
const minutes = timeOptions(60);

/** 双列滚轮支持滚轮、拖动、方向键和数字定位，触发器复用表单样式。 */
export function TimePicker({
  id,
  value,
  onChange,
  disabled,
  required,
  "aria-label": ariaLabel,
}: PickerProps) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const [hour = "09", minute = "00"] = value ? value.split(":") : [];
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FieldControlTrigger
          id={id}
          disabled={disabled}
          aria-required={required}
          aria-label={ariaLabel || t("dateTimePicker.chooseTime")}
          showChevron={false}
        >
          <span>{value || t("dateTimePicker.chooseTime")}</span>
          <Clock3
            aria-hidden
            className="text-content-secondary size-icon-sm shrink-0"
          />
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent align="start" collisionPadding={12}>
        <WheelPickerGroup>
          <WheelPickerColumn
            label={t("dateTimePicker.hour")}
            value={hour}
            options={hours}
            onChange={(next) => onChange(`${next}:${minute}`)}
          />
          <WheelPickerColumn
            label={t("dateTimePicker.minute")}
            value={minute}
            options={minutes}
            onChange={(next) => onChange(`${hour}:${next}`)}
          />
        </WheelPickerGroup>
        <div className="mt-3 flex justify-end">
          <Button
            size="sm"
            type="button"
            variant="primary"
            onClick={() => {
              if (!value) onChange(`${hour}:${minute}`);
              setOpen(false);
            }}
          >
            {t("dateTimePicker.done")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** 日期只保存日历值，时区解释由调用方负责，不提前转换为浏览器 UTC。 */
export function DatePicker({
  id,
  value,
  onChange,
  disabled,
  required,
  "aria-label": ariaLabel,
}: PickerProps) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const date = value ? localDateFromInput(value) : undefined;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FieldControlTrigger
          id={id}
          disabled={disabled}
          aria-required={required}
          aria-label={ariaLabel || t("dateTimePicker.chooseDate")}
          showChevron={false}
        >
          <span>{value || t("dateTimePicker.chooseDate")}</span>
          <CalendarDays
            aria-hidden
            className="text-content-secondary size-icon-sm shrink-0"
          />
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent align="start" collisionPadding={12}>
        <Calendar
          mode="single"
          locale={zhCN}
          weekStartsOn={1}
          defaultMonth={date}
          selected={date}
          onSelect={(next) => {
            if (next) {
              onChange(localDateToInput(next));
              setOpen(false);
            }
          }}
        />
      </PopoverContent>
    </Popover>
  );
}
