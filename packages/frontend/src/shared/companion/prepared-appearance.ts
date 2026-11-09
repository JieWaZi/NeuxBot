import { useEffect, useState } from "react";
import { tinted } from "@neuxbot/bloub/tint";
import { companionAppearance, type CompanionAppearance } from "./appearance";
import { materialTextures } from "./materials";

type PreparedTexture =
  | { status: "ready"; url: string; image: HTMLImageElement }
  | { status: "error" };
type TextureEntry = {
  pending: Promise<PreparedTexture>;
  ready?: PreparedTexture;
};
type PreparedAppearance = {
  request: string;
  appearance: CompanionAppearance;
  status: "pending" | "ready" | "error";
  texture?: string;
};

// 图集地址包含素材版本；在途准备共享，就绪图像仅保留最近使用的12份。
const textures = new Map<string, TextureEntry>();
const readyKeys = new Set<string>();
const READY_TEXTURE_LIMIT = 12;
const textureKey = (source: string, color: string) => `${source}|${color}`;

function retainReady(key: string) {
  readyKeys.delete(key);
  readyKeys.add(key);
  if (readyKeys.size <= READY_TEXTURE_LIMIT) return;
  const oldest = readyKeys.values().next().value;
  if (oldest === undefined) return;
  readyKeys.delete(oldest);
  textures.delete(oldest);
}

function decode(url: string): Promise<HTMLImageElement> {
  const image = new Image();
  if (typeof image.decode === "function") {
    image.src = url;
    return image.decode().then(() => image);
  }
  return new Promise((resolve, reject) => {
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("companion: texture unavailable"));
    image.src = url;
  });
}

function prepareTexture(
  source: string,
  color: string,
): Promise<PreparedTexture> {
  const key = textureKey(source, color);
  const cached = textures.get(key);
  if (cached) {
    if (cached.ready) retainReady(key);
    return cached.pending;
  }
  const entry: TextureEntry = {
    pending: tinted(source, color)
      .then(async (url): Promise<PreparedTexture> => ({
        status: "ready",
        url,
        image: await decode(url),
      }))
      .catch((): PreparedTexture => ({ status: "error" }))
      .then((result) => {
        if (result.status === "ready") {
          entry.ready = result;
          retainReady(key);
        } else textures.delete(key);
        return result;
      }),
  };
  textures.set(key, entry);
  return entry.pending;
}

function completed(
  request: string,
  appearance: CompanionAppearance,
  texture: PreparedTexture,
): PreparedAppearance {
  return {
    request,
    appearance,
    status: texture.status,
    texture: texture.status === "ready" ? texture.url : undefined,
  };
}

/** 只在纹理可显示后提交整套外观；准备期间保留上一套完整身体和表情。 */
export function usePreparedAppearance(request: string): PreparedAppearance {
  const requested = companionAppearance(request);
  const source = materialTextures[requested.material];
  const color = requested.color;
  const ready = textures.get(textureKey(source, color))?.ready;
  const available = ready ? completed(request, requested, ready) : undefined;
  const [shown, setShown] = useState<PreparedAppearance>(
    () => available ?? { request, appearance: requested, status: "pending" },
  );
  if (
    available &&
    (shown.request !== available.request ||
      shown.status !== available.status ||
      shown.texture !== available.texture)
  )
    setShown(available);
  useEffect(() => {
    let live = true;
    void prepareTexture(source, color).then((texture) => {
      if (!live) return;
      setShown(completed(request, companionAppearance(request), texture));
    });
    return () => {
      live = false;
    };
  }, [request, source, color]);
  return available ?? shown;
}
