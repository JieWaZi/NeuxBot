import { localizedNumberFormat } from "@/shared/i18n/format";
const integer = localizedNumberFormat();
const decimal = localizedNumberFormat({
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

export function formatUsageTokens(value: number, locale = "zh-CN"): string {
  if (!locale.startsWith("zh")) {
    return new Intl.NumberFormat(locale, {
      notation: value >= 10_000 ? "compact" : "standard",
      maximumFractionDigits: 1,
    }).format(value);
  }
  if (value >= 100_000_000) return `${decimal.format(value / 100_000_000)}亿`;
  if (value >= 10_000) return `${decimal.format(value / 10_000)}万`;
  return integer.format(value);
}
