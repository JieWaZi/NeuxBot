import { useTranslation } from "react-i18next";
import {
  localizedDateTimeFormat,
  localizedNumberFormat,
} from "@/shared/i18n/format";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";

const number = localizedNumberFormat({ maximumFractionDigits: 1 });
const time = localizedDateTimeFormat({
  dateStyle: "medium",
  timeStyle: "short",
});
const uploadTime = localizedDateTimeFormat({
  dateStyle: "medium",
  timeStyle: "medium",
});
export function storageDate(value: number) {
  return time.format(value);
}
export function storageUploadDate(value: number) {
  return uploadTime.format(value);
}
export function storageSize(bytes: number) {
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const index = Math.min(
    units.length - 1,
    Math.max(0, Math.floor(Math.log(Math.max(1, bytes)) / Math.log(1024))),
  );
  return number.format(bytes / 1024 ** index) + " " + units[index];
}

export function StorageLoading() {
  const { t } = useTranslation("storage");
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t("loading")}
      className="grid gap-3 py-2"
    >
      <Skeleton className="h-5 w-28" />
      <Skeleton className="h-3 w-full" />
    </div>
  );
}
export function StorageError({
  error,
  retry,
  stale = false,
}: {
  error: unknown;
  retry?: () => void;
  stale?: boolean;
}) {
  const { t } = useTranslation("storage");
  return (
    <div
      role="alert"
      className="type-body text-danger flex flex-wrap items-center gap-2"
    >
      <span>
        {localApiErrorMessage(error, t(retry ? "failed" : "actionFailed"))}{" "}
        {stale ? t("stale") : null}
      </span>
      {retry ? (
        <Button size="settings" variant="settings-outline" onClick={retry}>
          {t("retry")}
        </Button>
      ) : null}
    </div>
  );
}
