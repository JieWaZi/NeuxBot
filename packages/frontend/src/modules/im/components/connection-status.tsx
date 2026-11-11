import { useTranslation } from "react-i18next";
import { StatusPill } from "@/shared/ui/status-pill";
import { Badge } from "@/shared/ui/badge";
import { cn } from "@/shared/lib/cn";
import type { IMBinding, IMConnection } from "../types";

export function ConnectionStatus({
  connection,
  appearance = "badge",
  stale = false,
}: {
  connection: IMConnection;
  appearance?: "badge" | "inline" | "pill";
  stale?: boolean;
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
  if (stale)
    return <StatusPill tone="attention">{t("statusStale")}</StatusPill>;
  if (appearance === "pill")
    return (
      <StatusPill
        tone={
          state === "connected"
            ? "connected"
            : state === "failed" || state === "login_required"
              ? "attention"
              : state === "connecting"
                ? "connecting"
                : state === "disabled"
                  ? "unavailable"
                  : "idle"
        }
      >
        {label}
      </StatusPill>
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
