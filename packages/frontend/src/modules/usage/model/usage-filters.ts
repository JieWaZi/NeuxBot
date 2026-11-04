export type UsageGroupBy = "day" | "model" | "waker";
export type UsageMetric = "tokens" | "cost";

export type UsageFilters = {
  from: number;
  to: number;
  timezone: string;
  wakerIds: string[];
  groupBy: UsageGroupBy;
  metric: UsageMetric;
};

const day = 86_400_000;

function dateFormatter(timeZone: string) {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    calendar: "iso8601",
    numberingSystem: "latn",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}

function isoDate(value: number, formatter: Intl.DateTimeFormat) {
  const parts = formatter.formatToParts(value);
  const part = (type: string) =>
    parts.find((item) => item.type === type)?.value;
  return `${part("year")?.padStart(4, "0")}-${part("month")}-${part("day")}`;
}

export function usageDate(value: number, timezone: string) {
  return isoDate(value, dateFormatter(timezone));
}

function dateShift(date: string, days: number) {
  const value = new Date(`${date}T00:00:00Z`);
  value.setUTCDate(value.getUTCDate() + days);
  return value.toISOString().slice(0, 10);
}

function dayStart(date: string, timezone: string) {
  const approximate = Date.parse(`${date}T00:00:00Z`);
  const formatter = dateFormatter(timezone);
  let low = approximate - 2 * day;
  let high = approximate + 2 * day;
  // Search the local date boundary, including midnight offset changes.
  while (low < high) {
    const middle = low + Math.floor((high - low) / 2);
    if (isoDate(middle, formatter) < date) low = middle + 1;
    else high = middle;
  }
  return low;
}

export function rangeFromUsageDates(
  first: string,
  last: string,
  timezone: string,
) {
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(first) ||
    !/^\d{4}-\d{2}-\d{2}$/.test(last) ||
    first > last
  ) {
    return undefined;
  }
  const firstUTC = Date.parse(`${first}T00:00:00Z`);
  const lastUTC = Date.parse(`${last}T00:00:00Z`);
  if (
    !Number.isFinite(firstUTC) ||
    !Number.isFinite(lastUTC) ||
    lastUTC - firstUTC >= 366 * day ||
    new Date(firstUTC).toISOString().slice(0, 10) !== first ||
    new Date(lastUTC).toISOString().slice(0, 10) !== last
  ) {
    return undefined;
  }
  const from = dayStart(first, timezone);
  const to = dayStart(dateShift(last, 1), timezone);
  return from > 0 && from < to ? { from, to } : undefined;
}

function timezoneValue(value: string | null) {
  const local = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  if (!value || /^[+-]/.test(value) || value === "Local") return local;
  try {
    dateFormatter(value);
    return value;
  } catch {
    return local;
  }
}

export function parseUsageFilters(
  search: URLSearchParams,
  now = new Date(),
): UsageFilters {
  const timezone = timezoneValue(search.get("timezone"));
  const today = usageDate(now.getTime(), timezone);
  const from = Number(search.get("from"));
  const to = Number(search.get("to"));
  const group = search.get("groupBy");
  const valid =
    Number.isSafeInteger(from) &&
    Number.isSafeInteger(to) &&
    from > 0 &&
    to > from &&
    to <= 253402300799999 &&
    to - from < 368 * day &&
    Date.parse(usageDate(to - 1, timezone)) -
      Date.parse(usageDate(from, timezone)) <
      366 * day;
  const range = valid
    ? { from, to }
    : {
        from: dayStart(dateShift(today, -29), timezone),
        to: dayStart(dateShift(today, 1), timezone),
      };
  return {
    ...range,
    timezone,
    wakerIds: Array.from(
      new Set(
        search
          .getAll("waker")
          .filter(
            (id) =>
              id.length > 0 &&
              id.length <= 128 &&
              id.trim() === id &&
              !id.includes(","),
          ),
      ),
    )
      .sort()
      .slice(0, 100),
    groupBy: group === "model" || group === "waker" ? group : "day",
    metric: search.get("metric") === "cost" ? "cost" : "tokens",
  };
}

export function usageFiltersSearch(filters: UsageFilters) {
  const search = new URLSearchParams({
    from: String(filters.from),
    to: String(filters.to),
    timezone: filters.timezone,
    groupBy: filters.groupBy,
    metric: filters.metric,
  });
  for (const id of filters.wakerIds) search.append("waker", id);
  return search;
}
