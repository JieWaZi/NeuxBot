import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { Button } from "@/shared/ui/button";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { Skeleton } from "@/shared/ui/skeleton";
import type { MainConversationWork } from "../model/main-conversation-message-group";

export function MainConversationSummary({
  activeWork,
  pending,
  onOpenSource,
}: {
  activeWork?: readonly MainConversationWork[];
  pending: boolean;
  onOpenSource: (chatId: string, messageId?: string) => void;
}) {
  const { t } = useTranslation("chat");
  const { data: runtimes } = useQuery({
    ...listRuntimesOptions(),
    enabled: false,
  });
  const running = activeWork?.flatMap((work) => {
    const { workChatId, executionId } = work;
    return workChatId &&
      executionId &&
      work.running === true &&
      !work.defaultSource
      ? [{ ...work, workChatId, executionId }]
      : [];
  });
  const title = t("workSummary.tasks");

  return (
    <section className="min-w-0" aria-label={title}>
      <h2 className="text-muted-foreground type-body mb-2">{title}</h2>
      {pending && (
        <div
          role="status"
          aria-busy="true"
          aria-label={t("workSummary.loadingTasks")}
        >
          <Skeleton aria-hidden="true" className="h-8 w-full" />
        </div>
      )}
      {running?.length ? (
        <div className="grid">
          {running.map((work) => {
            const title = work.title?.trim() || t("workSummary.untitled");
            const profile = runtimes?.resources?.find(
              (item) => item.id === work.runtimeId,
            );
            return (
              <Button
                key={work.executionId}
                type="button"
                size="row"
                variant="ghost"
                className="w-full min-w-0"
                title={title}
                onClick={() =>
                  onOpenSource(work.workChatId, work.inputMessageId)
                }
              >
                <RuntimeCliIcon
                  runtimeId={profile?.baseRuntimeId || work.runtimeId}
                />
                <span className="min-w-0 truncate font-normal">{title}</span>
              </Button>
            );
          })}
        </div>
      ) : activeWork ? (
        <p className="text-muted-foreground type-ui px-2 py-1">
          {t("workSummary.noTasks")}
        </p>
      ) : null}
    </section>
  );
}
