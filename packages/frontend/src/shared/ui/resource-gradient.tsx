import { useEffect, useRef } from "react";
import type { ShaderMount } from "@paper-design/shaders";
import {
  articleGradient,
  articleGradientCss,
  articleGradientUniforms,
} from "@/shared/lib/resource-gradient";

/** 沿用 SkillGradient 的动态行为，配色读取宿主主题；离屏、隐藏或减少动态效果时释放 shader。 */
export function ResourceGradient({ seed }: { seed: string }) {
  const host = useRef<HTMLDivElement>(null);
  const profile = articleGradient(seed);
  useEffect(() => {
    const element = host.current;
    if (!element || !window.matchMedia || !window.IntersectionObserver) return;
    const gradient = articleGradient(seed);
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)");
    let mount: ShaderMount | undefined,
      visible = false,
      disposed = false,
      generation = 0,
      failed = false;
    const stop = () => {
      generation++;
      mount?.dispose();
      mount = undefined;
    };
    const refresh = async () => {
      if (disposed || !visible || document.hidden || reduced.matches) {
        stop();
        return;
      }
      if (mount || failed) return;
      const attempt = ++generation;
      try {
        const shader = await import("@paper-design/shaders");
        if (disposed || attempt !== generation) return;
        mount = new shader.ShaderMount(
          element,
          shader.meshGradientFragmentShader,
          articleGradientUniforms(gradient, (color) => {
            const token = /^var\((--[a-z-]+)\)$/.exec(color)?.[1];
            return shader.getShaderColorFromString(
              token
                ? getComputedStyle(element).getPropertyValue(token).trim()
                : color,
            );
          }),
          undefined,
          0.6,
          gradient.frame,
        );
      } catch {
        failed = true;
        stop();
      }
    };
    const observer = new IntersectionObserver(([entry]) => {
      visible = entry?.isIntersecting ?? false;
      void refresh();
    });
    const update = () => {
      void refresh();
    };
    const lost = () => {
      failed = true;
      stop();
    };
    // Shader 的颜色只在创建时解析，主题切换后按权威 data-theme 重新物化。
    const themeObserver = new MutationObserver(() => {
      stop();
      void refresh();
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    observer.observe(element);
    reduced.addEventListener("change", update);
    document.addEventListener("visibilitychange", update);
    element.addEventListener("webglcontextlost", lost, true);
    return () => {
      disposed = true;
      stop();
      observer.disconnect();
      themeObserver.disconnect();
      reduced.removeEventListener("change", update);
      document.removeEventListener("visibilitychange", update);
      element.removeEventListener("webglcontextlost", lost, true);
    };
  }, [seed]);
  return (
    <div
      ref={host}
      aria-hidden="true"
      className="catalog-try-gradient"
      style={{ background: articleGradientCss(profile) }}
    />
  );
}
