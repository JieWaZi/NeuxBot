export type OfficePreviewErrorCode =
  | "sourceTooLarge"
  | "expandedTooLarge"
  | "invalidArchivePath"
  | "activeContent"
  | "unreadableArchive"
  | "invalidFormat"
  | "scopeRequired"
  | "fileMissing";

/** Office 文件校验只抛稳定错误码，展示文案由预览界面翻译。 */
export class OfficePreviewError extends Error {
  constructor(
    readonly code: OfficePreviewErrorCode,
    options?: ErrorOptions,
  ) {
    super(code, options);
    this.name = "OfficePreviewError";
  }
}

const errorKeys = {
  sourceTooLarge: "workspace.officeError.sourceTooLarge",
  expandedTooLarge: "workspace.officeError.expandedTooLarge",
  invalidArchivePath: "workspace.officeError.invalidArchivePath",
  activeContent: "workspace.officeError.activeContent",
  unreadableArchive: "workspace.officeError.unreadableArchive",
  invalidFormat: "workspace.officeError.invalidFormat",
  scopeRequired: "workspace.officeError.scopeRequired",
  fileMissing: "workspace.officeError.fileMissing",
} as const;

export function officePreviewErrorKey(cause: unknown) {
  return cause instanceof OfficePreviewError
    ? errorKeys[cause.code]
    : ("workspace.officeError.loadFailed" as const);
}
