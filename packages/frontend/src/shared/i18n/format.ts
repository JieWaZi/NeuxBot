import { i18n } from "./index";

/** 模块级格式器也在每次使用时核对语言，避免切换后保留启动时的语言。 */
function currentFormatter<T>(create: (language: string) => T): () => T {
  let cached: { language: string; formatter: T } | undefined;
  return () => {
    const language = i18n.resolvedLanguage ?? "zh-CN";
    if (!cached || cached.language !== language)
      cached = { language, formatter: create(language) };
    return cached.formatter;
  };
}

export function localizedDateTimeFormat(options?: Intl.DateTimeFormatOptions) {
  const current = currentFormatter(
    (language) => new Intl.DateTimeFormat(language, options),
  );
  return {
    format: (value?: number | Date) => current().format(value),
    formatToParts: (value?: number | Date) => current().formatToParts(value),
  };
}

export function localizedNumberFormat(options?: Intl.NumberFormatOptions) {
  const current = currentFormatter(
    (language) => new Intl.NumberFormat(language, options),
  );
  return { format: (value: number | bigint) => current().format(value) };
}

export function localizedRelativeTimeFormat(
  options?: Intl.RelativeTimeFormatOptions,
) {
  const current = currentFormatter(
    (language) => new Intl.RelativeTimeFormat(language, options),
  );
  return {
    format: (value: number, unit: Intl.RelativeTimeFormatUnit) =>
      current().format(value, unit),
  };
}
