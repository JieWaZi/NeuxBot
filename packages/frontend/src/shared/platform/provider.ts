import type { ApplicationUpdateService } from "@neuxbot/platform-contracts/application-update";
import type { CoreConnection } from "@neuxbot/platform-contracts/resources";
import type {
  HostService,
  WorkspaceService,
  GitService,
  BrowserService,
  TerminalService,
  EventsService,
} from "@neuxbot/platform-contracts/services";

export type Platform = {
  kind: "desktop" | "web";
  /** 宿主窗口控件需要的标题栏起始避让；普通浏览器使用共享默认间距。 */
  chrome?: { navigationInset: number };
  getConnection(): Promise<CoreConnection>;
  host: HostService;
  workspace?: WorkspaceService;
  git?: GitService;
  browser?: BrowserService;
  terminal?: TerminalService;
  events?: EventsService;
  applicationUpdate?: ApplicationUpdateService;
};
let current: Platform | undefined;
/** 启动入口注入宿主实现，公共应用不探测 Electron 全局对象。 */
export function configurePlatform(value: Platform): void {
  current = value;
}
export function platform(): Platform {
  if (!current) throw new Error("应用平台尚未初始化");
  return current;
}
export function requirePlatformService<
  K extends
    | "host"
    | "workspace"
    | "git"
    | "browser"
    | "terminal"
    | "events"
    | "applicationUpdate",
>(name: K): NonNullable<Platform[K]> {
  const service = platform()[name];
  if (!service) throw new Error("当前运行环境不支持此能力");
  return service;
}
export function hasPlatformService(
  name:
    | "workspace"
    | "git"
    | "browser"
    | "terminal"
    | "events"
    | "applicationUpdate",
): boolean {
  return Boolean(platform()[name]);
}
