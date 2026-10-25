export function workflowStatusAppearance(status?: string) {
  switch (status) {
    case "succeeded":
    case "answered":
      return {
        badge: "success",
        surface:
          "bg-status-success-surface hover:bg-status-success-surface data-[state=selected]:bg-status-success-surface",
      } as const;
    case "failed":
    case "unknown":
    case "cancelled":
    case "timed_out":
      return {
        badge: "destructive",
        surface:
          "bg-status-danger-surface hover:bg-status-danger-surface data-[state=selected]:bg-status-danger-surface",
      } as const;
    case "waiting":
      return {
        badge: "warning",
        surface:
          "bg-status-warning-surface hover:bg-status-warning-surface data-[state=selected]:bg-status-warning-surface",
      } as const;
    case "running":
      return {
        badge: "default",
        surface:
          "bg-accent-surface hover:bg-accent-surface data-[state=selected]:bg-accent-surface",
      } as const;
    default:
      return { badge: "secondary", surface: "" } as const;
  }
}

export function nodeExecutionStatus(statuses: (string | undefined)[]) {
  return [
    "failed",
    "timed_out",
    "unknown",
    "waiting",
    "running",
    "prepared",
    "ready",
    "cancelled",
    "skipped",
    "succeeded",
    "answered",
  ].find((status) => statuses.includes(status));
}
