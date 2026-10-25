import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/badge";
import { cn } from "@/shared/lib/cn";
import type { IMBinding, IMConnection } from "../types";

export function ConnectionStatus({
  connection,
  appearance = "badge",
}: {
  connection: IMConnection;
  appearance?: "badge" | "inline";
}) {
  const { t } = useTranslation("im");
  const state = connection.enabled
    ? connection.state || "disconnected"
    : "disabled";
  const variant =
    state === "connected"
      ? "success"
      : state === "failed"
        ? "destructive"
        : state === "login_required"
          ? "warning"
          : state === "connecting"
            ? "default"
            : "secondary";
  const label = t(
    state === "disabled" ? "disabled" : "connectionState." + state,
  );
  if (appearance === "inline") {
    return (
      <span className="text-content-secondary type-caption inline-flex shrink-0 items-center gap-2 whitespace-nowrap">
        <span
          aria-hidden="true"
          className={cn("size-2 shrink-0 rounded-full", {
            "bg-success": variant === "success",
            "bg-destructive": variant === "destructive",
            "bg-warning": variant === "warning",
            "bg-primary": variant === "default",
            "bg-content-secondary": variant === "secondary",
          })}
        />
        {label}
      </span>
    );
  }
  return <Badge variant={variant}>{label}</Badge>;
}

export function BindingStatus({ binding }: { binding: IMBinding }) {
  const { t } = useTranslation("im");
  return (
    <Badge
      variant={
        binding.state === "approved"
          ? "success"
          : binding.state === "rejected"
            ? "secondary"
            : "warning"
      }
    >
      {t("bindingState." + (binding.state || "pending"))}
    </Badge>
  );
}
