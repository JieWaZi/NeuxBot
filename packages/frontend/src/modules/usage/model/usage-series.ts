import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Database,
  Sparkles,
} from "lucide-react";

export const usageSeries = [
  { key: "input", color: "text-status-info-content", icon: ArrowDownToLine },
  {
    key: "output",
    color: "text-status-success-content",
    icon: ArrowUpFromLine,
  },
  { key: "cacheWrite", color: "text-status-warning-content", icon: Database },
  { key: "cacheRead", color: "text-accent-secondary-strong", icon: Sparkles },
] as const;
