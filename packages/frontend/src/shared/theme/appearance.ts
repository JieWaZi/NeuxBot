import {
  interfaceScales,
  type InterfaceScale,
} from "@neuxbot/platform-contracts/services";
export { interfaceScales, type InterfaceScale };
export type AppearancePreferences = {
  scale: InterfaceScale;
};
const storageKey = "bot.ui.appearance";

export function isInterfaceScale(value: unknown): value is InterfaceScale {
  return interfaceScales.some((scale) => scale === value);
}

export function readAppearance(): AppearancePreferences {
  try {
    const value: unknown = JSON.parse(
      localStorage.getItem(storageKey) ?? "null",
    );
    if (value && typeof value === "object" && "scale" in value)
      return {
        scale: isInterfaceScale(value.scale) ? value.scale : 1,
      };
  } catch {
    // 旧值或受限存储不阻止应用启动。
  }
  return { scale: 1 };
}

export function persistAppearance(value: AppearancePreferences): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify(value));
  } catch {
    // 存储不可用时仍应用当前会话的偏好。
  }
}
