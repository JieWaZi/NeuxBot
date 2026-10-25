import "@/app/workspace-i18n";
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

// ProseMirror uses layout APIs that jsdom does not implement. Keep the editor
// behavior tests deterministic without changing the production editor path.
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}
if (typeof Range.prototype.getClientRects !== "function") {
  Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
}
if (typeof Range.prototype.getBoundingClientRect !== "function") {
  Range.prototype.getBoundingClientRect = () => new DOMRect();
}
if (typeof HTMLElement.prototype.scrollIntoView !== "function") {
  HTMLElement.prototype.scrollIntoView = () => undefined;
}
if (!Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, "value")) {
  Object.defineProperty(HTMLDivElement.prototype, "value", {
    configurable: true,
    get() {
      return this.getAttribute("value") || this.textContent || "";
    },
    set(value: string) {
      this.setAttribute("value", value);
    },
  });
}

afterEach(() => {
  cleanup();
});

// 公共组件测试使用显式浏览器宿主，能力由具体用例注入。
import { configurePlatform } from "@/shared/platform/provider";
beforeEach(() => {
  configurePlatform({
    kind: "web",
    getConnection: async () => ({
      baseUrl: "http://127.0.0.1:8080",
      credentials: "same-origin",
    }),
    host: {
      save_file: async () => false,
      select_directory: async () => null,
      select_files: async () => [],
      stage_attachment: async () => {
        throw new Error("测试未配置附件上传");
      },
      open_external_url: async () => undefined,
      show_notification: async () => false,
      onFileDrop: () => () => undefined,
      onNotificationAction: () => () => undefined,
    },
  });
});
