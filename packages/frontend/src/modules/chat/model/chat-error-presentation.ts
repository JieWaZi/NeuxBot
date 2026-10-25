/** 提取协议错误的可读原因，完整输入仅用于用户主动展开的诊断详情。 */
export function chatErrorPresentation(raw: string, fallback: string) {
  function reason(value: unknown, depth = 0): string | undefined {
    if (depth > 8) return;
    if (typeof value === "string" && value.trim()) {
      try {
        const parsed: unknown = JSON.parse(value);
        if (parsed && typeof parsed === "object")
          return reason(parsed, depth + 1);
      } catch {
        /* 普通错误文本直接展示。 */
      }
      return value.trim();
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) return;
    const record = value as Record<string, unknown>;
    for (const key of ["data", "cause", "error", "details", "message"]) {
      const message = reason(record[key], depth + 1);
      if (message) return message;
    }
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object") {
      return { message: reason(parsed) ?? fallback, details: raw };
    }
  } catch {
    /* 非协议包装的错误沿用可读原文。 */
  }
  return { message: raw };
}
