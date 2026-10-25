import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { workflowRunLogsOptions } from "../api/workflow-queries";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { jsonText, record } from "../model/types";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";

export function RunLogs({
  id,
  runId,
  completedAt,
}: {
  id: string;
  runId: string;
  completedAt?: number;
}) {
  const { t, i18n } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const [open, setOpen] = useState(false);
  const [cursors, setCursors] = useState([0]);
  const [page, setPage] = useState(0);
  const options = workflowRunLogsOptions({
    id,
    runId,
    after: cursors[page] ?? 0,
    completedAt,
  });
  const query = useQuery({
    ...options,
    enabled: active,
  });
  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id, parentId: runId },
    queryKeys: [options.queryKey],
    active,
    enabled: Boolean(id && runId),
  });
  const logs = query.data?.resources ?? [];
  if (!logs.length && !query.isError && page === 0) return null;
  const next = logs.length === 500 ? logs.at(-1)?.sequence : undefined;
  return (
    <details
      className="grid gap-3"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="type-label cursor-pointer font-medium">
        {t("runLogs")}
      </summary>
      <div className="mt-3 grid gap-3">
        {query.isError ? (
          <WorkflowQueryError
            error={query.error}
            retrying={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        ) : null}
        {query.isPending ? <WorkflowLoadingState /> : null}
        {logs.map((event) => {
          const payload = record(event.payload);
          return (
            <div
              key={event.eventId}
              className="bg-surface-module grid gap-2 rounded-lg p-4"
            >
              <p className="text-muted-foreground type-caption">
                {event.createdAt
                  ? new Intl.DateTimeFormat(i18n.language, {
                      timeStyle: "medium",
                    }).format(event.createdAt)
                  : ""}
              </p>
              <pre className="type-caption max-h-80 overflow-auto whitespace-pre-wrap">
                {payload?.stdout !== undefined || payload?.stderr !== undefined
                  ? [payload.stdout, payload.stderr].filter(Boolean).join("\n")
                  : jsonText(payload?.value ?? event.payload)}
              </pre>
              {payload?.logsTruncated ? (
                <p className="text-muted-foreground type-caption">
                  {t("logsTruncated")}
                </p>
              ) : null}
            </div>
          );
        })}
        <div className="flex gap-2">
          {page > 0 ? (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setPage(page - 1)}
            >
              {t("previousPage")}
            </Button>
          ) : null}
          {next ? (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                setCursors((current) => [...current.slice(0, page + 1), next]);
                setPage(page + 1);
              }}
            >
              {t("nextPage")}
            </Button>
          ) : null}
        </div>
      </div>
    </details>
  );
}
