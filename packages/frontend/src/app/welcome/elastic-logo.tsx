import { useEffect, useRef, useState } from "react";

import { logger } from "@/shared/diagnostics/logger";
import { useTheme } from "@/shared/theme";
import type { mountElasticMesh } from "../../../../../vendor/elastic-mesh/renderer";

/** IP 入场后接续弹性字标；图形不可用时在同一位置保留静态品牌。 */
export default function ElasticLogo({
  intro = true,
  onIntroComplete,
}: {
  intro?: boolean;
  onIntroComplete?: () => void;
}) {
  // 入场意图按挂载固定；服务就绪只交接状态，引导展开通过新 key 重新入场。
  const initialIntro = useRef(intro);
  const complete = useRef(onIntroComplete);
  const completed = useRef(!intro);
  useEffect(() => {
    complete.current = onIntroComplete;
  }, [onIntroComplete]);
  const host = useRef<HTMLDivElement>(null);
  const [display, setDisplay] = useState<{ theme: string; ready: boolean }>();
  const { resolvedTheme } = useTheme();
  const ready = display?.ready === true && display.theme === resolvedTheme;
  const fallback =
    !ready &&
    (!intro || (display?.ready === false && display.theme === resolvedTheme));
  useEffect(() => {
    if (fallback && !completed.current) {
      completed.current = true;
      complete.current?.();
    }
  }, [fallback]);
  useEffect(() => {
    const element = host.current;
    if (!element) return;
    let disposed = false;
    let renderer: ReturnType<typeof mountElasticMesh> | undefined;
    const load = async () => {
      try {
        const { mountElasticMesh } =
          await import("../../../../../vendor/elastic-mesh/renderer");
        if (disposed) return;
        // Canvas 使用浏览器实际计算后的 sRGB 值，不自行维护一套品牌色。
        const styles = getComputedStyle(element);
        const color = styles.color;
        const probe = document.createElement("canvas").getContext("2d");
        const canvasColor = (value: string) => {
          if (!probe) return value;
          probe.fillStyle = value;
          return typeof probe.fillStyle === "string" ? probe.fillStyle : value;
        };
        renderer = mountElasticMesh(element, {
          text: "NeuxBot",
          intro: initialIntro.current && !completed.current,
          onIntroComplete: () => {
            if (!disposed && !completed.current) {
              completed.current = true;
              complete.current?.();
            }
          },
          onError: () => {
            if (!disposed) {
              setDisplay({ theme: resolvedTheme, ready: false });
              queueMicrotask(() => {
                if (!disposed) {
                  renderer?.destroy();
                  renderer = undefined;
                }
              });
            }
          },
          color: canvasColor(color),
          colorEnd: canvasColor(
            styles.getPropertyValue("--welcome-wordmark-end").trim(),
          ),
          theme: resolvedTheme,
          dpr: Math.min(window.devicePixelRatio || 1, 2),
          onReady: () => {
            if (!disposed) {
              if (initialIntro.current && !completed.current)
                performance.mark?.("ally.startup.logoReady");
              setDisplay({ theme: resolvedTheme, ready: true });
            }
          },
        });
        if (!renderer.available)
          setDisplay({ theme: resolvedTheme, ready: false });
      } catch (error) {
        if (!disposed) {
          logger.warn("welcome elastic logo failed", { error });
          setDisplay({ theme: resolvedTheme, ready: false });
        }
      }
    };
    const lost = (event: Event) => {
      event.preventDefault();
      renderer?.destroy();
      renderer = undefined;
      setDisplay({ theme: resolvedTheme, ready: false });
    };
    const canvas = element.querySelector("canvas");
    canvas?.addEventListener("webglcontextlost", lost);
    void load();
    return () => {
      disposed = true;
      canvas?.removeEventListener("webglcontextlost", lost);
      renderer?.destroy();
    };
  }, [resolvedTheme]);
  return (
    <div
      ref={host}
      className="welcome-logo relative touch-pan-y select-none"
      role="img"
      aria-label="NeuxBot"
    >
      <span
        className="welcome-logo-fallback"
        hidden={!fallback}
        aria-hidden="true"
      >
        NeuxBot
      </span>
      <canvas
        aria-hidden="true"
        className="absolute inset-0 size-full"
        hidden={!ready}
      />
    </div>
  );
}
