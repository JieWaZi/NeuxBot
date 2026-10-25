import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerApisWakersDailyChatCount } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

export const wakerActivityHeatmapRows = 7;

const activityLevelClasses = [
  "bg-border/30",
  "bg-activity-1",
  "bg-activity-2",
  "bg-activity-3",
] as const;

const heatmapCellClass = "aspect-square w-full min-w-0 rounded-xs";

export function activityLevelForCount(count: number, maxCount: number) {
  if (count <= 0 || maxCount <= 0) {
    return 0;
  }
  const ratio = count / maxCount;
  if (ratio <= 1 / 3) {
    return 1;
  }
  if (ratio <= 2 / 3) {
    return 2;
  }
  return 3;
}

export type HeatmapCell =
  GithubComJieWaZiNeuxBotServerApisWakersDailyChatCount | null;

function parseIsoDate(value?: string) {
  if (!value) {
    return null;
  }
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) {
    return null;
  }
  return new Date(year, month - 1, day);
}

function mondayBasedWeekday(date: Date) {
  return (date.getDay() + 6) % 7;
}

export function heatmapWeekColumns(
  daily: GithubComJieWaZiNeuxBotServerApisWakersDailyChatCount[],
) {
  if (daily.length === 0) {
    return 0;
  }
  const startDate = parseIsoDate(daily[0]?.date);
  if (!startDate) {
    return 0;
  }
  const startWeekday = mondayBasedWeekday(startDate);
  return Math.floor((startWeekday + daily.length - 1) / 7) + 1;
}

export function layoutHeatmapGrid(
  daily: GithubComJieWaZiNeuxBotServerApisWakersDailyChatCount[],
) {
  const weekColumns = heatmapWeekColumns(daily);
  const grid: HeatmapCell[][] = Array.from(
    { length: wakerActivityHeatmapRows },
    () => Array.from({ length: weekColumns }, () => null),
  );
  if (daily.length === 0 || weekColumns === 0) {
    return grid;
  }

  const startDate = parseIsoDate(daily[0]?.date);
  if (!startDate) {
    return grid;
  }

  const startWeekday = mondayBasedWeekday(startDate);
  daily.forEach((entry, index) => {
    const offset = startWeekday + index;
    const column = Math.floor(offset / 7);
    const row = offset % 7;
    if (column < weekColumns) {
      const rowCells = grid[row];
      if (rowCells) {
        rowCells[column] = entry;
      }
    }
  });

  return grid;
}

export function buildHeatmapMonthLabels(grid: HeatmapCell[][]) {
  const weekColumns = grid[0]?.length ?? 0;
  const labels: Array<{ column: number; label: string }> = [];
  let previousMonth = "";

  for (let column = 0; column < weekColumns; column += 1) {
    let month = "";
    for (let row = 0; row < wakerActivityHeatmapRows; row += 1) {
      const date = grid[row]?.[column]?.date;
      if (date) {
        month = date.slice(5, 7);
        break;
      }
    }
    if (!month || month === previousMonth) {
      continue;
    }
    labels.push({
      column,
      label: `${Number(month)}月`,
    });
    previousMonth = month;
  }

  return labels;
}

function heatmapGridStyle(weekColumns: number) {
  return {
    gridTemplateColumns: `repeat(${weekColumns}, minmax(0, 1fr))`,
  } as const;
}

type WakerActivityHeatmapProps = {
  daily: GithubComJieWaZiNeuxBotServerApisWakersDailyChatCount[];
};

export function WakerActivityHeatmap({ daily }: WakerActivityHeatmapProps) {
  const { t } = useTranslation("waker");
  const weekColumns = useMemo(() => heatmapWeekColumns(daily), [daily]);
  const grid = useMemo(() => layoutHeatmapGrid(daily), [daily]);
  const monthLabels = useMemo(() => buildHeatmapMonthLabels(grid), [grid]);
  const maxCount = useMemo(
    () =>
      daily.reduce(
        (currentMax, entry) => Math.max(currentMax, entry.count ?? 0),
        0,
      ),
    [daily],
  );
  const weekGridStyle = useMemo(
    () => heatmapGridStyle(weekColumns),
    [weekColumns],
  );

  if (weekColumns === 0) {
    return null;
  }

  return (
    <TooltipProvider delayDuration={150}>
      <div className="bg-muted/60 rounded-md px-4 pt-4 pb-3">
        <div className="grid w-full gap-1">
          <div className="relative min-h-4 w-full overflow-visible">
            {monthLabels.map(({ column, label }) => (
              <span
                key={`${column}-${label}`}
                className="text-muted-foreground type-caption absolute top-0 whitespace-nowrap"
                style={{
                  left: `${(column / weekColumns) * 100}%`,
                }}
              >
                {label}
              </span>
            ))}
          </div>
          <div
            className="grid w-full grid-flow-col grid-rows-7 gap-0.5"
            style={weekGridStyle}
          >
            {Array.from({ length: weekColumns }, (_, column) =>
              Array.from({ length: wakerActivityHeatmapRows }, (_, row) => {
                const entry = grid[row]?.[column] ?? null;
                const count = entry?.count ?? 0;
                const level = activityLevelForCount(count, maxCount);
                const key = entry?.date ?? `${row}-${column}`;

                if (!entry) {
                  return (
                    <span
                      key={key}
                      className={cn(heatmapCellClass, "bg-border/30")}
                      aria-hidden
                    />
                  );
                }

                return (
                  <Tooltip key={key}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className={cn(
                          heatmapCellClass,
                          "block min-h-0 p-0",
                          activityLevelClasses[level],
                        )}
                        aria-label={t("workRecord.dailyWorkload", {
                          count,
                        })}
                      />
                    </TooltipTrigger>
                    <TooltipContent side="top" align="center">
                      <div className="grid gap-0.5">
                        <span>{entry.date}</span>
                        <span>{t("workRecord.dailyWorkload", { count })}</span>
                      </div>
                    </TooltipContent>
                  </Tooltip>
                );
              }),
            )}
          </div>
        </div>
        <div className="text-muted-foreground type-caption mt-2 flex items-center justify-center gap-1">
          <span>{t("workRecord.less")}</span>
          {activityLevelClasses.map((color) => (
            <i
              key={color}
              className={cn("size-2 rounded-xs", color)}
              aria-hidden
            />
          ))}
          <span>{t("workRecord.more")}</span>
        </div>
      </div>
    </TooltipProvider>
  );
}
