/** 内置代理使用产品名称，受控配置中的命令和协议名只在配置表单展示。 */
export function runtimeDisplayName(runtime?: { id?: string; name?: string }) {
  switch (runtime?.id?.toLowerCase()) {
    case "codex":
      return "Codex";
    case "claude":
      return "Claude";
    case "cursor":
      return "Cursor";
    case "kimi":
      return "Kimi";
    case "opencode":
      return "OpenCode";
    case "gemini":
      return "Gemini CLI";
    case "grok":
      return "Grok Build";
    case "qoder-cn":
      return "Qoder CN";
    case "qoder":
      return "Qoder";
    case "workbuddy":
      return "WorkBuddy";
    case "pi":
      return "Pi";
    default:
      return runtime?.name ?? runtime?.id ?? "";
  }
}

/** 版本展示只保留版本号，省去程序输出中重复的产品名称。 */
export function runtimeDisplayVersion(version?: string) {
  const value = version?.trim() ?? "";
  if (value.toLowerCase() === "unknown") return "";
  return (
    value.match(/(?:^|\s)(v?\d+(?:\.\d+)+(?:[-+][\w.+-]+)?)$/i)?.[1] ?? value
  );
}
