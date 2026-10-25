import { useTranslation } from "react-i18next";

import { DateRangePicker } from "@/shared/ui/date-range-picker";

type ChatActivityDateRangePickerProps = {
  from: string;
  onChange: (from: string, to: string) => void;
  to: string;
};

export function ChatActivityDateRangePicker({
  from,
  onChange,
  to,
}: ChatActivityDateRangePickerProps) {
  const { t } = useTranslation("chat");
  return (
    <DateRangePicker
      from={from}
      labels={{
        clear: t("taskPage.clearRange"),
        empty: t("taskPage.allTime"),
        help: t("taskPage.activityRangeHelp"),
        title: t("taskPage.activityRange"),
        trigger: t("taskPage.selectActivityRange"),
      }}
      to={to}
      onChange={onChange}
    />
  );
}
