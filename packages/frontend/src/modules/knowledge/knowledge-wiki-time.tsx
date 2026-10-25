import { useTranslation } from "react-i18next";

/** 仅显示接口提供的有效时间；不以当前时间补齐缺失元数据。 */
export function WikiTime({
  value,
  label,
}: {
  value?: number;
  label?: (time: string) => string;
}) {
  const { i18n } = useTranslation();
  if (!value || !Number.isFinite(value)) return null;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return null;
  const text = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
  return (
    <time
      className="type-caption text-content-secondary"
      dateTime={date.toISOString()}
    >
      {label ? label(text) : text}
    </time>
  );
}
