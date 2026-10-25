type LogContext = Record<string, unknown>;

function sanitize(context: LogContext): LogContext {
  const sanitized: LogContext = {};
  for (const [key, value] of Object.entries(context)) {
    if (/token|prompt|content|body/i.test(key)) {
      sanitized[key] = "[REDACTED]";
    } else {
      sanitized[key] = value;
    }
  }
  return sanitized;
}

function write(
  level: "debug" | "info" | "warn" | "error",
  message: string,
  context: LogContext = {},
): void {
  if (!import.meta.env.DEV && level === "debug") {
    return;
  }
  globalThis.console[level](`[NeuxBot] ${message}`, sanitize(context));
}

export const logger = {
  debug: (message: string, context?: LogContext) =>
    write("debug", message, context),
  info: (message: string, context?: LogContext) =>
    write("info", message, context),
  warn: (message: string, context?: LogContext) =>
    write("warn", message, context),
  error: (message: string, context?: LogContext) =>
    write("error", message, context),
};
