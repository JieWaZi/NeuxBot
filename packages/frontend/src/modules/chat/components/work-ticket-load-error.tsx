import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";

export function WorkTicketLoadError({
  error,
  pending,
  onRetry,
}: {
  error: unknown;
  pending?: boolean;
  onRetry?: () => void;
}) {
  const { t } = useTranslation("chat");
  return (
    <div
      className="type-ui flex flex-wrap items-center gap-2 py-1"
      role="alert"
    >
      <span className="text-destructive min-w-0 break-words">
        {localApiErrorMessage(error, t("workTicket.loadFailed"))}
      </span>
      {onRetry && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={pending}
          onClick={onRetry}
        >
          {t("common:retry")}
        </Button>
      )}
    </div>
  );
}
