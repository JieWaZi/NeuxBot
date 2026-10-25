import { useQueryEvents } from "@/shared/api/use-query-events";
import { useQuery } from "@tanstack/react-query";
import { Clock3 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { listAutomaticTaskAttentionOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { automaticRunHref } from "../model/task-navigation";
import { Card } from "@/shared/ui/card";
import { RunState } from "./task-presentation";

export function AutomaticTaskAttentionSection({
  wakerId,
}: {
  wakerId: string;
}) {
  const { t } = useTranslation("automaticTask");
  const query = useQuery({
    ...listAutomaticTaskAttentionOptions(
      withLocalErrorSurface({ path: { wakerId } }),
    ),
  });
  useQueryEvents({
    topic: "automatic-tasks",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [
      listAutomaticTaskAttentionOptions({ path: { wakerId } }).queryKey,
    ],
    enabled: Boolean(wakerId),
  });
  const pending = (query.data?.runs || []).filter(
    (run) => run.state === "waiting_approval" || run.state === "waiting_input",
  );
  if (!pending.length) return null;
  return (
    <Card variant="section">
      <h2 className="type-title font-semibold">{t("pendingTitle")}</h2>
      <div className="mt-4 grid gap-2">
        {pending.map((run) => (
          <div
            key={run.id}
            className="bg-surface-tertiary flex flex-wrap items-center gap-3 rounded-md p-3"
          >
            <Clock3 className="size-icon-md text-content-secondary" />
            <Link
              className="text-link type-body min-w-0 flex-1 break-words hover:underline"
              to={automaticRunHref(run)}
            >
              {run.configuration?.name}
            </Link>
            <RunState state={run.state} />
          </div>
        ))}
      </div>
    </Card>
  );
}
