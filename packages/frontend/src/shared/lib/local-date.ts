const dateInputPattern = /^(\d{4})-(\d{2})-(\d{2})$/;

export function localDateFromInput(value: string) {
  const match = dateInputPattern.exec(value);
  if (!match) return undefined;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(year, month - 1, day);
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return undefined;
  }
  return date;
}

export function normalizeDateInput(value: string | null) {
  if (!value || !localDateFromInput(value)) return "";
  return value;
}

export function localDateToInput(date: Date | undefined) {
  if (!date) return "";
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

// localDateRangeBounds 把两个日期输入转换为闭区间的 Unix 毫秒边界，结束日包含当天全部时间。
export function localDateRangeBounds(from: string, to: string) {
  const fromDate = from ? localDateFromInput(from) : undefined;
  const toDate = to ? localDateFromInput(to) : undefined;
  const start = fromDate?.getTime();
  const end = toDate
    ? new Date(
        toDate.getFullYear(),
        toDate.getMonth(),
        toDate.getDate() + 1,
      ).getTime() - 1
    : undefined;
  return {
    end,
    start,
    valid: start === undefined || end === undefined || start <= end,
  };
}
