import { useId, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";

import type { ListUsageGroupsResponse } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { formatUsageTokens } from "../model/usage-format";
import { usageSeries } from "../model/usage-series";

type Group = NonNullable<ListUsageGroupsResponse["groups"]>[number];
const zeroMetrics: NonNullable<Group["metrics"]> = {
  totalWithCacheRead: 0,
  knownTokens: { input: 0, output: 0, cacheWrite: 0, cacheRead: 0 },
};
type Point = { x: number; y: number };
type PointRun = [Point, ...Point[]];

const dayMilliseconds = 86_400_000;
const plotWidth = 1000;
const plotHeight = 280;
const gridSteps = 4;

function dayPosition(index: number, count: number) {
  return (index + 0.5) / count;
}

function moveDayFocus(event: KeyboardEvent<HTMLDivElement>) {
  if (!(event.target instanceof HTMLButtonElement)) return;
  if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;

  event.preventDefault();
  const buttons = Array.from(event.currentTarget.querySelectorAll("button"));
  const index = buttons.indexOf(event.target);
  const direction = event.key === "ArrowLeft" ? -1 : 1;
  buttons[index + direction]?.focus();
}

function plotCeiling(max: number) {
  if (max <= gridSteps) return gridSteps;
  const magnitude = 10 ** Math.floor(Math.log10(max / gridSteps));
  const step = Math.ceil(max / gridSteps / magnitude) * magnitude;
  return step * gridSteps;
}

function pointRuns(values: (number | null)[], ceiling: number) {
  const runs: PointRun[] = [];
  let run: PointRun | undefined;
  values.forEach((value, index) => {
    if (value == null) {
      run = undefined;
      return;
    }
    const point = {
      x: dayPosition(index, values.length) * plotWidth,
      y: (1 - value / ceiling) * (plotHeight - 4) + 4,
    };
    if (run) {
      run.push(point);
    } else {
      run = [point];
      runs.push(run);
    }
  });
  return runs;
}

function curve([first, ...points]: PointRun) {
  let path = `M ${first.x} ${first.y}`;
  let previous = first;
  for (const point of points) {
    // Horizontal control points keep each segment inside its measured range.
    const middle = (previous.x + point.x) / 2;
    path += ` C ${middle} ${previous.y} ${middle} ${point.y} ${point.x} ${point.y}`;
    previous = point;
  }
  return path;
}

export function UsageTrend({
  groups,
  from,
  to,
  hasMore,
  projectionPartial = false,
  earliestRetainedDay,
}: {
  groups: readonly Group[];
  from: string;
  to: string;
  hasMore: boolean;
  projectionPartial?: boolean;
  earliestRetainedDay?: string;
}) {
  const { t, i18n } = useTranslation("usage");
  const empty = groups.length === 0 && !projectionPartial;
  const gradient = useId();
  const [focusedDay, setFocusedDay] = useState<string>();
  const locale = i18n.resolvedLanguage;
  const start = Date.parse(from);
  const rangeEnd = Date.parse(to);
  const lastLoaded = Date.parse(groups.at(-1)?.key ?? "");
  const end =
    hasMore && lastLoaded >= start && lastLoaded <= rangeEnd
      ? lastLoaded
      : rangeEnd;
  const dates = new Intl.DateTimeFormat(locale, {
    year: from.slice(0, 4) !== to.slice(0, 4) ? "numeric" : undefined,
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
  const metricsByDate = new Map(
    groups.map(({ key, metrics }) => [key, metrics]),
  );
  const days = Array.from(
    { length: (end - start) / dayMilliseconds + 1 },
    (_, index) => {
      const date = new Date(start + index * dayMilliseconds);
      const key = date.toISOString().slice(0, 10);
      const metrics = empty ? zeroMetrics : metricsByDate.get(key);
      return { key, date, observed: empty || metricsByDate.has(key), metrics };
    },
  );
  const series = usageSeries.flatMap((item) => {
    const hasData = days.some(
      ({ metrics }) => metrics?.knownTokens?.[item.key] != null,
    );
    if (!hasData) return [];
    const values = days.map(({ key, observed, metrics }) => {
      if (observed) return metrics?.knownTokens?.[item.key] ?? null;
      return !projectionPartial &&
        earliestRetainedDay &&
        key >= earliestRetainedDay
        ? 0
        : null;
    });
    return [{ ...item, values }];
  });
  const ceiling = plotCeiling(
    Math.max(
      0,
      ...series.flatMap(({ values }) => values.map((value) => value ?? 0)),
    ),
  );
  const title = t("dailyTokens");
  const tabStop =
    days.find(({ key, observed }) => observed && key === focusedDay)?.key ??
    days.find(({ observed }) => observed)?.key;
  const tickCount = Math.min(7, days.length);
  const ticks = Array.from({ length: tickCount }, (_, index) => {
    const dayIndex = Math.round(
      (index * (days.length - 1)) / Math.max(1, tickCount - 1),
    );
    const time = start + dayIndex * dayMilliseconds;
    return {
      label: dates.format(time),
      key: new Date(time).toISOString().slice(0, 10),
      position: dayPosition(dayIndex, days.length),
    };
  });

  return (
    <figure className="min-w-0 space-y-4">
      <figcaption className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="type-title font-semibold">{title}</span>
        <span className="text-content-secondary type-ui">
          {t("rangeDays", { count: days.length })}
        </span>
      </figcaption>
      <div
        role="group"
        aria-label={title}
        className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2"
      >
        <div
          aria-hidden="true"
          className="text-content-secondary type-caption flex flex-col items-end justify-between tabular-nums"
        >
          {empty ? (
            <span className="mt-auto">0</span>
          ) : series.length ? (
            Array.from({ length: gridSteps + 1 }, (_, index) => {
              const value = (ceiling * (gridSteps - index)) / gridSteps;
              return (
                <span key={value}>{formatUsageTokens(value, locale)}</span>
              );
            })
          ) : null}
        </div>
        <div className="relative h-56 min-w-0 @[42.5rem]/resource-page:h-72">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 flex flex-col justify-between"
          >
            {Array.from({ length: gridSteps + 1 }, (_, index) => (
              <div
                key={index}
                className="border-border-subtle border-t border-dashed last:border-solid"
              />
            ))}
          </div>
          <svg
            aria-hidden="true"
            viewBox={`0 0 ${plotWidth} ${plotHeight}`}
            preserveAspectRatio="none"
            className="pointer-events-none absolute inset-0 h-full w-full overflow-visible"
          >
            <defs>
              {series.map(({ key, color }) => (
                <linearGradient
                  key={key}
                  id={`${gradient}-${key}`}
                  x2="0"
                  y2="1"
                >
                  <stop
                    stopColor="currentColor"
                    stopOpacity={0.2}
                    className={color}
                  />
                  <stop
                    offset="100%"
                    stopColor="currentColor"
                    stopOpacity={0}
                    className={color}
                  />
                </linearGradient>
              ))}
            </defs>
            {[...series].reverse().map(({ key, color, values }) => (
              <g key={key} className={color}>
                {pointRuns(values, ceiling).map((points) => {
                  const first = points[0];
                  const last = points.at(-1) ?? first;
                  const line = curve(points);
                  return (
                    <g key={first.x}>
                      {points.length > 1 ? (
                        <path
                          d={`${line} L ${last.x} ${plotHeight} H ${first.x} Z`}
                          fill={`url(#${gradient}-${key})`}
                        />
                      ) : null}
                      <path
                        d={points.length > 1 ? line : `${line} h 0.01`}
                        fill="none"
                        stroke="currentColor"
                        style={{ strokeWidth: points.length > 1 ? 1.75 : 5 }}
                        strokeLinecap="round"
                        vectorEffect="non-scaling-stroke"
                      />
                    </g>
                  );
                })}
              </g>
            ))}
          </svg>
          <div
            className="absolute inset-0 grid"
            style={{
              gridTemplateColumns: `repeat(${days.length}, minmax(0, 1fr))`,
            }}
            onKeyDown={moveDayFocus}
          >
            <TooltipProvider delayDuration={0}>
              {days.map(({ key, date, observed, metrics }) => {
                if (!observed) return <div key={key} aria-hidden="true" />;
                const total = metrics?.totalWithCacheRead;
                const value = total ?? metrics?.knownTotalWithCacheRead;
                const amount =
                  value == null
                    ? t("unknown")
                    : t(
                        total == null || projectionPartial
                          ? "recordedTokenAmount"
                          : "tokenAmount",
                        {
                          value: formatUsageTokens(value, locale),
                        },
                      );
                const label = t("dailyTokenValue", {
                  date: dates.format(date),
                  value: amount,
                });
                return (
                  <Tooltip key={key}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        tabIndex={key === tabStop ? 0 : -1}
                        aria-label={label}
                        className="hover:bg-accent-surface-subtle focus-visible:bg-accent-surface-strong after:border-accent-border relative h-full min-w-0 outline-none after:pointer-events-none after:absolute after:inset-y-0 after:left-1/2 after:border-l after:border-dashed after:opacity-0 hover:after:opacity-100 focus-visible:after:opacity-100"
                        onFocus={() => setFocusedDay(key)}
                      />
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      <p className="mb-1.5 font-medium">{label}</p>
                      <dl className="grid gap-1">
                        {usageSeries.map(({ key, color }) => {
                          const count = metrics?.knownTokens?.[key];
                          return (
                            <div
                              key={key}
                              className="flex justify-between gap-4"
                            >
                              <dt className="flex items-center gap-1.5">
                                <span
                                  aria-hidden="true"
                                  className={cn(
                                    "size-1.5 rounded-full bg-current",
                                    color,
                                  )}
                                />
                                {t(key)}
                              </dt>
                              <dd className="tabular-nums">
                                {count == null
                                  ? t("unknown")
                                  : formatUsageTokens(count, locale)}
                              </dd>
                            </div>
                          );
                        })}
                      </dl>
                    </TooltipContent>
                  </Tooltip>
                );
              })}
            </TooltipProvider>
          </div>
          {series.length === 0 ? (
            <p className="text-content-secondary type-body pointer-events-none absolute inset-0 grid place-items-center">
              {t("noTrendTokens")}
            </p>
          ) : null}
        </div>
        <div
          aria-hidden="true"
          className="text-content-secondary type-caption relative col-start-2 h-4"
        >
          {ticks.map(({ label, key, position }, index) => (
            <time
              key={key}
              dateTime={key}
              className={cn(
                "absolute whitespace-nowrap",
                position < 0.12
                  ? undefined
                  : position > 0.88
                    ? "-translate-x-full"
                    : "-translate-x-1/2",
                index > 0 &&
                  index < ticks.length - 1 &&
                  "hidden @[42.5rem]/resource-page:block",
              )}
              style={{ left: `${position * 100}%` }}
            >
              {label}
            </time>
          ))}
        </div>
      </div>
      <ul className="text-content-secondary type-ui flex flex-wrap justify-center gap-x-4 gap-y-2">
        {usageSeries.map(({ key, color }) => (
          <li key={key} className="flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className={cn("h-0.5 w-3 rounded-full bg-current", color)}
            />
            {t(key)}
          </li>
        ))}
      </ul>
    </figure>
  );
}
