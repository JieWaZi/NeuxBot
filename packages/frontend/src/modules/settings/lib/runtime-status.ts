import type {
  GetRuntimeInstallationResponse,
  GetRuntimeAuthenticationResponse,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption,
} from "@/shared/api/generated/types.gen";
import type { StatusPillTone } from "@/shared/ui/status-pill";

/** 安装、程序可用与认证是独立事实，标题状态优先呈现当前需要处理的事实。 */
export function runtimeHeaderState(
  runtime: RuntimeOption,
  installation?: GetRuntimeInstallationResponse,
  authentication?: GetRuntimeAuthenticationResponse,
): {
  tone: StatusPillTone;
  label:
    | "notInstalled"
    | "disabled"
    | "loginRequired"
    | "ready"
    | "notReady"
    | "installing"
    | "removing"
    | "removalFailed"
    | "installFailed"
    | "signingIn"
    | "configurationRequired"
    | "available";
} {
  if (installation?.state === "installing")
    return {
      tone: "connecting",
      label: installation.operation === "uninstall" ? "removing" : "installing",
    };
  if (installation?.state === "failed")
    return {
      tone: "attention",
      label:
        installation.operation === "uninstall"
          ? "removalFailed"
          : "installFailed",
    };
  if (
    authentication?.state === "authenticating" ||
    (authentication?.state === "checking" && authentication.jobId)
  )
    return { tone: "connecting", label: "signingIn" };
  if (!(installation?.installed ?? runtime.installed))
    return { tone: "idle", label: "notInstalled" };
  if (!runtime.enabled) return { tone: "unavailable", label: "disabled" };
  if (
    runtime.auth === "missing" &&
    !runtime.defaultAccountId &&
    authentication?.configurationReason &&
    !authentication.canAuthenticate
  )
    return { tone: "attention", label: "configurationRequired" };
  if (runtime.auth === "missing")
    return { tone: "attention", label: "loginRequired" };
  if (
    runtime.state !== "ready" ||
    runtime.diagnostics?.some((item) => item.level === "error")
  )
    return { tone: "attention", label: "notReady" };
  if (runtime.auth === "ok") return { tone: "connected", label: "ready" };
  return { tone: "idle", label: "available" };
}
