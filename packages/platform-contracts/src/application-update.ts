/** 软件更新仅属于桌面发行，不占用 Core 的通知与工作状态。 */
export type ApplicationUpdateStatus =
  | "unavailable"
  | "idle"
  | "checking"
  | "no-release"
  | "current"
  | "available"
  | "downloading"
  | "downloaded"
  | "error";

export type ApplicationUpdateError =
  | "network"
  | "rate-limited"
  | "invalid-release"
  | "missing-asset"
  | "checksum"
  | "download"
  | "open-installer";

export type ApplicationRelease = {
  version: string;
  url: string;
  notes: string;
  publishedAt: string;
  downloadSize: number;
};

/** 单调版本避免迟到的查询结果覆盖下载进度事件。 */
export type ApplicationUpdateState = {
  revision: number;
  currentVersion: string | null;
  platform: string;
  arch: string;
  status: ApplicationUpdateStatus;
  unavailableReason?: "development" | "platform" | "browser";
  release?: ApplicationRelease;
  checkedAt?: string;
  downloadedBytes: number;
  error?: ApplicationUpdateError;
};

export const applicationReleasesURL =
  "https://github.com/JieWaZi/NeuxBot/releases";

/** 仅由具备安装包更新能力的宿主提供，Web 不伪造安装成功。 */
export type ApplicationUpdateService = {
  application_update_state(): Promise<ApplicationUpdateState>;
  check_application_update(): Promise<ApplicationUpdateState>;
  download_application_update(): Promise<ApplicationUpdateState>;
  open_application_installer(): Promise<ApplicationUpdateState>;
  onApplicationUpdate(
    handler: (state: ApplicationUpdateState) => void,
  ): () => void;
};
