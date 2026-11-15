import {
  Menu,
  nativeTheme,
  type BrowserWindow,
  type BrowserWindowConstructorOptions,
} from "electron";
import { release } from "node:os";
import { readFile, rename, rm, writeFile } from "node:fs/promises";
import type { WindowAppearanceCommands } from "@neuxbot/platform-contracts/services";
import {
  interfaceScales,
  type InterfaceScale,
} from "@neuxbot/platform-contracts/services";

type Appearance = WindowAppearanceCommands["set_window_appearance"]["args"];
const languages = new WeakMap<BrowserWindow, Appearance["language"]>();
const windows = new WeakMap<
  BrowserWindow,
  { transparent: boolean; theme: Appearance["theme"] }
>();

export function supportsWindowTransparency() {
  return (
    process.platform === "darwin" ||
    (process.platform === "win32" &&
      Number(release().split(".")[2] ?? 0) >= 22621)
  );
}

function backgroundColor(theme: Appearance["theme"]) {
  const dark =
    theme === "dark" || (theme === "system" && nativeTheme.shouldUseDarkColors);
  return dark ? "#1a1a1a" : "#f5f5f6";
}

/** 创建前读取主进程偏好，macOS 的合成窗口必须从构造时就具备 vibrancy。 */
export function windowAppearanceOptions(
  transparent: boolean,
): Pick<
  BrowserWindowConstructorOptions,
  "vibrancy" | "visualEffectState" | "backgroundColor" | "backgroundMaterial"
> {
  // 保持系统材质在失焦后继续模糊；开关关闭时页面与窗口背景仍完全不透明。
  if (process.platform === "darwin") return {
    visualEffectState: "active",
    ...(transparent ? { vibrancy: "under-window" as const } : { backgroundColor: backgroundColor("system") }),
  };
  if (transparent && supportsWindowTransparency()) {
    return { backgroundMaterial: "acrylic", backgroundColor: "#00000000" };
  }
  return { backgroundColor: backgroundColor("system") };
}

/** 窗口创建后只在透明开关变化时重设材质，系统主题变化只刷新背景。 */
export function initializeWindowAppearance(
  window: BrowserWindow,
  transparent: boolean,
) {
  const state = {
    transparent: transparent && supportsWindowTransparency(),
    theme: "system" as const,
  };
  windows.set(window, state);
  const update = () => {
    const current = windows.get(window);
    if (current && !window.isDestroyed())
      window.setBackgroundColor(
        current.transparent ? "#00000000" : backgroundColor(current.theme),
      );
  };
  update();
  nativeTheme.on("updated", update);
  window.once("closed", () => {
    nativeTheme.off("updated", update);
    windows.delete(window);
  });
}

/** 透明偏好由主进程单独持久化，保证下一次窗口构造不依赖 Renderer 启动。 */
export async function loadWindowAppearance(file: string) {
  let transparent = await readFile(file, "utf8")
    .then((text) => {
      const value: unknown = JSON.parse(text);
      return Boolean(
        value &&
        typeof value === "object" &&
        "transparent" in value &&
        value.transparent === true,
      );
    })
    .catch(() => false);
  let queue: Promise<unknown> = Promise.resolve();
  return {
    get: () => ({
      transparent: transparent && supportsWindowTransparency(),
      transparencySupported: supportsWindowTransparency(),
    }),
    set(next: boolean) {
      const pending = queue
        .catch(() => undefined)
        .then(async () => {
          if (transparent === next) return;
          const temporary = `${file}.tmp`;
          try {
            await writeFile(temporary, JSON.stringify({ transparent: next }), {
              mode: 0o600,
            });
            await rename(temporary, file);
            transparent = next;
          } catch (error) {
            await rm(temporary, { force: true }).catch(() => undefined);
            throw error;
          }
        });
      queue = pending;
      return pending;
    },
  };
}

export function windowLanguage(window: BrowserWindow) {
  return languages.get(window);
}

/** 主进程只接收已校验的外观值，远端网页不参与窗口设置。 */
export function applyWindowAppearance(
  window: BrowserWindow,
  value: Appearance,
) {
  const transparencySupported = supportsWindowTransparency();
  const transparent = value.transparent && transparencySupported;
  const previous = windows.get(window);
  windows.set(window, { transparent, theme: value.theme });
  // 原生网页与菜单在不透明窗口中跟随系统；透明材质才需要应用主题。
  const nativeSource = transparent ? value.theme : "system";
  if (nativeTheme.themeSource !== nativeSource)
    nativeTheme.themeSource = nativeSource;
  if (transparent !== (previous?.transparent ?? false)) {
    if (process.platform === "darwin")
      window.setVibrancy(transparent ? "under-window" : null);
    else if (transparencySupported)
      window.setBackgroundMaterial(transparent ? "acrylic" : "none");
  }
  window.setBackgroundColor(
    transparent ? "#00000000" : backgroundColor(value.theme),
  );
  window.webContents.setZoomFactor(value.scale);
  if (languages.get(window) !== value.language) {
    languages.set(window, value.language);
    const en = value.language === "en-US";
    const item = (
      role: Electron.MenuItemConstructorOptions["role"],
      zh: string,
      english: string,
    ) => ({ role, label: en ? english : zh });
    const requestScale = (direction: -1 | 0 | 1) => {
      const current = window.webContents.getZoomFactor();
      const scale: InterfaceScale =
        direction === 0
          ? 1
          : direction > 0
            ? (interfaceScales.find((scale) => scale > current + 0.001) ?? 1.5)
            : ([...interfaceScales]
                .reverse()
                .find((scale) => scale < current - 0.001) ?? 0.8);
      window.webContents.send("bot:interface-scale", scale);
    };
    Menu.setApplicationMenu(
      Menu.buildFromTemplate([
        ...(process.platform === "darwin"
          ? [
              {
                label: "NeuxBot",
                submenu: [
                  item("about", "关于 NeuxBot", "About NeuxBot"),
                  { type: "separator" as const },
                  item("hide", "隐藏 NeuxBot", "Hide NeuxBot"),
                  item("hideOthers", "隐藏其他", "Hide Others"),
                  item("unhide", "显示全部", "Show All"),
                  { type: "separator" as const },
                  item("quit", "退出 NeuxBot", "Quit NeuxBot"),
                ],
              },
            ]
          : []),
        {
          label: en ? "Edit" : "编辑",
          submenu: [
            item("undo", "撤销", "Undo"),
            item("redo", "重做", "Redo"),
            { type: "separator" },
            item("cut", "剪切", "Cut"),
            item("copy", "复制", "Copy"),
            item("paste", "粘贴", "Paste"),
            item(
              "pasteAndMatchStyle",
              "粘贴并匹配样式",
              "Paste and Match Style",
            ),
            item("selectAll", "全选", "Select All"),
          ],
        },
        {
          label: en ? "View" : "视图",
          submenu: [
            item("reload", "重新载入", "Reload"),
            item("forceReload", "强制重新载入", "Force Reload"),
            item("toggleDevTools", "开发者工具", "Toggle Developer Tools"),
            { type: "separator" },
            {
              label: en ? "Actual Size" : "实际大小",
              accelerator: "CmdOrCtrl+0",
              click: () => requestScale(0),
            },
            {
              label: en ? "Zoom In" : "放大",
              accelerator: "CmdOrCtrl+Plus",
              click: () => requestScale(1),
            },
            {
              label: en ? "Zoom Out" : "缩小",
              accelerator: "CmdOrCtrl+-",
              click: () => requestScale(-1),
            },
            { type: "separator" },
            item("togglefullscreen", "切换全屏", "Toggle Full Screen"),
          ],
        },
        {
          label: en ? "Window" : "窗口",
          submenu: [
            item("minimize", "最小化", "Minimize"),
            item("close", "关闭窗口", "Close Window"),
            ...(process.platform === "darwin"
              ? [item("front", "前置全部窗口", "Bring All to Front")]
              : []),
          ],
        },
      ]),
    );
  }
  return { transparencySupported };
}
