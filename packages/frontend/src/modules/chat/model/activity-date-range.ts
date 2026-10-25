import { localDateRangeBounds } from "@/shared/lib/local-date";

export {
  localDateFromInput,
  localDateToInput,
  normalizeDateInput,
} from "@/shared/lib/local-date";

export function activityDateRange(from: string, to: string) {
  const bounds = localDateRangeBounds(from, to);
  return {
    updatedAtEnd: bounds.end,
    updatedAtStart: bounds.start,
    valid: bounds.valid,
  };
}

export function pageFromSearchParam(value: string | null) {
  if (!value || !/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page > 0 ? page : 1;
}

export function chatPageSizeFromSearchParam(value: string | null) {
  if (value === "30") return 30;
  if (value === "50") return 50;
  return 10;
}
