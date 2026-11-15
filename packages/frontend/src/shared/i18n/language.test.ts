import { act } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  getLanguageMode,
  i18n,
  initializeLanguage,
  setLanguageMode,
} from "./index";
import { readLanguageMode, resolveLanguage } from "./language";
import { localizedDateTimeFormat, localizedRelativeTimeFormat } from "./format";

afterEach(() => {
  vi.restoreAllMocks();
  setLanguageMode("system");
  localStorage.removeItem("bot.ui.language");
});

it("语言选择立即生效并保存，重新读取沿用显式选择", async () => {
  const stop = initializeLanguage();
  await act(() => setLanguageMode("en-US"));
  expect(getLanguageMode()).toBe("en-US");
  expect(readLanguageMode()).toBe("en-US");
  expect(i18n.t("appSettings:general.appearance.language")).toBe("Language");
  expect(document.documentElement.lang).toBe("en-US");
  stop();
});

it("跟随系统解析首个受支持语言，其他语言回退中文", () => {
  vi.spyOn(navigator, "languages", "get").mockReturnValue([
    "fr-FR",
    "en-GB",
    "zh-TW",
  ]);
  expect(resolveLanguage("system")).toBe("en-US");
  expect(resolveLanguage("zh-CN")).toBe("zh-CN");
  vi.spyOn(navigator, "languages", "get").mockReturnValue(["ja-JP"]);
  expect(resolveLanguage("system")).toBe("zh-CN");
});

it("仅跟随系统模式响应系统语言变化，并释放监听", async () => {
  const languages = vi
    .spyOn(navigator, "languages", "get")
    .mockReturnValue(["zh-TW"]);
  const stop = initializeLanguage();
  await act(() => setLanguageMode("system"));
  languages.mockReturnValue(["en-GB"]);
  await act(() => window.dispatchEvent(new Event("languagechange")));
  expect(i18n.resolvedLanguage).toBe("en-US");
  await act(() => setLanguageMode("zh-CN"));
  await act(() => window.dispatchEvent(new Event("languagechange")));
  expect(i18n.resolvedLanguage).toBe("zh-CN");
  stop();
});

it("模块级日期和相对时间格式器随语言切换", async () => {
  const date = localizedDateTimeFormat({ month: "long" });
  const relative = localizedRelativeTimeFormat({ numeric: "auto" });
  await i18n.changeLanguage("zh-CN");
  expect(date.format(new Date(2026, 10, 14))).toBe("十一月");
  expect(relative.format(-1, "day")).toBe("昨天");
  await i18n.changeLanguage("en-US");
  expect(date.format(new Date(2026, 10, 14))).toBe("November");
  expect(relative.format(-1, "day")).toBe("yesterday");
});
