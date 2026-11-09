export { TEXTURES as materialTextures } from "@neuxbot/bloub/materials";
import { BODY_SPRITES, type Material } from "@neuxbot/bloub/materials";

/** 粒子的透明微尘会缩小视觉体积；统一缩放视域，让身体、眼睛和动作保持比例。 */
export function materialViewBox(material: Material): string {
  const extent = 158 / BODY_SPRITES[material].visualScale;
  return `${-extent} ${-extent} ${extent * 2} ${extent * 2}`;
}
