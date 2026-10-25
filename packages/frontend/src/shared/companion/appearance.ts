import styles from "../../../../../vendor/companion/styles.json";

const prefix = "companion:v2:";
const pattern = /^companion:v[12]:[0-9a-f]{32}$/;
const bodyStyles = Object.keys(styles) as (keyof typeof styles)[];

export function randomCompanionAvatar(): string {
  return prefix + crypto.randomUUID().replaceAll("-", "");
}

/** 从当前身体风格切到下一款，固定身份保证保存及再次编辑时顺序一致。 */
export function nextCompanionAvatar(avatar: string): string {
  const current = bodyStyles.indexOf(companionAppearance(avatar).style);
  const next = bodyStyles[(current + 1) % bodyStyles.length] ?? "original";
  return styles[next].avatar;
}

/** 未提供合法形象时，以调用方身份生成稳定展示，不执行外部图片地址。 */
export function companionAvatar(
  value: string | undefined,
  identity: string,
): string {
  if (value && pattern.test(value)) return value;
  let seed = 2166136261;
  for (const character of `${identity}:${value?.slice(0, 128) ?? ""}`)
    seed = Math.imul(seed ^ character.charCodeAt(0), 16777619);
  return prefix + (seed >>> 0).toString(16).padStart(8, "0").repeat(4);
}

/** 身体风格随身份稳定还原，材质与颜色由图片决定。 */
export function companionAppearance(avatar: string) {
  let seed = 2166136261;
  for (const character of avatar)
    seed = Math.imul(seed ^ character.charCodeAt(0), 16777619);
  const sample = (length: number) => {
    seed += 0x6d2b79f5;
    let value = Math.imul(seed ^ (seed >>> 15), seed | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return Math.floor((((value ^ (value >>> 14)) >>> 0) / 4294967296) * length);
  };
  const selected = bodyStyles.find((style) => styles[style].avatar === avatar);
  const style = selected ?? bodyStyles[sample(bodyStyles.length)] ?? "original";
  return {
    style,
    glow: styles[style].glow,
  };
}
