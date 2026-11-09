import {
  lazy,
  Suspense,
  useEffect,
  useState,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { useTheme } from "@/shared/theme";

const CompanionAvatar = lazy(() =>
  import("@/shared/companion/companion-avatar").then((module) => ({
    default: module.CompanionAvatar,
  })),
);

const ElasticLogo = lazy(() => import("./elastic-logo"));
let assetsReady: Promise<void> | undefined;
/** 只预热代码，不提前创建 GPU 或启动字标动画；失败仍由各组件负责静态降级。 */
export function prepareWelcomeAssets(): Promise<void> {
  return (assetsReady ??= Promise.allSettled([
    import("@/shared/companion/companion-avatar"),
    import("../../../../../vendor/elastic-mesh/renderer"),
    import("./elastic-logo"),
  ]).then(() => undefined));
}
const motionQuery = "(prefers-reduced-motion: reduce)";
const readReducedMotion = () =>
  window.matchMedia?.(motionQuery).matches ?? false;
const watchReducedMotion = (listener: () => void) => {
  const query = window.matchMedia?.(motionQuery);
  query?.addEventListener("change", listener);
  return () => query?.removeEventListener("change", listener);
};

/** 品牌入场的完成信号来自实际绘制和动画结束，不由服务响应或固定停留时间推断。 */
export function WelcomeBrand({
  children,
  intro = true,
  onIntroComplete,
}: {
  children?: ReactNode;
  intro?: boolean;
  onIntroComplete?: () => void;
}) {
  const { resolvedTheme } = useTheme();
  const [avatarColor, setAvatarColor] = useState<string>();
  const reducedMotion = useSyncExternalStore(
    watchReducedMotion,
    readReducedMotion,
    () => true,
  );
  const notified = useRef(false);
  const [characterReady, setCharacterReady] = useState(false);
  const [characterArrived, setCharacterArrived] = useState(false);
  const [logoFinished, setLogoFinished] = useState(false);
  useEffect(() => {
    // 等待主题提供者将新 Token 应用到根节点后再读取。
    const frame = requestAnimationFrame(() => {
      // 头像标识需要 sRGB 十六进制；通过浏览器转换主题 Token，避免维护第二套品牌色。
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = 1;
      const context = canvas.getContext("2d", { willReadFrequently: true });
      if (!context) {
        setCharacterReady(true);
        setCharacterArrived(true);
        return;
      }
      context.fillStyle = getComputedStyle(document.documentElement)
        .getPropertyValue("--accent-default")
        .trim();
      context.fillRect(0, 0, 1, 1);
      setAvatarColor(
        Array.from(context.getImageData(0, 0, 1, 1).data.slice(0, 3))
          .map((channel) => channel.toString(16).padStart(2, "0"))
          .join(""),
      );
    });
    return () => cancelAnimationFrame(frame);
  }, [resolvedTheme]);
  if (characterReady && reducedMotion && !characterArrived)
    setCharacterArrived(true);
  useEffect(() => {
    void prepareWelcomeAssets();
  }, []);
  const showLogo =
    !intro ||
    (characterReady && (reducedMotion || characterArrived || logoFinished));
  const finished = !intro || logoFinished;
  const guideShown = Boolean(children) && finished;
  const [guideSettled, setGuideSettled] = useState(false);
  if (!guideShown && guideSettled) setGuideSettled(false);
  if (guideShown && reducedMotion && !guideSettled) setGuideSettled(true);
  // 收缩期间卸载旧画布，等引导实际入场结束后按最终尺寸重新弹出字标。
  const logoVisible =
    showLogo && (!guideShown || reducedMotion || guideSettled);
  useEffect(() => {
    if (finished && !notified.current && onIntroComplete) {
      notified.current = true;
      onIntroComplete();
    }
  }, [finished, onIntroComplete]);
  return (
    <main className="welcome-screen bg-background text-foreground">
      <div className="welcome-layout" data-guide={guideShown}>
        <div className="welcome-brand" role="img" aria-label="NeuxBot">
          <div className="welcome-brand-content">
            <div
              className="welcome-character shrink-0"
              data-ready={characterReady}
              data-intro={intro && !logoFinished}
              onAnimationEnd={(event) => {
                if (event.target === event.currentTarget)
                  setCharacterArrived(true);
              }}
            >
              <div className="welcome-character-appearance size-full">
                <Suspense
                  fallback={
                    <span aria-hidden="true" className="block size-full" />
                  }
                >
                  {avatarColor ? (
                    <CompanionAvatar
                      avatar={`bloub:v1:particles:pentagone:neutre:${avatarColor}`}
                      onReady={() => {
                        if (intro)
                          performance.mark?.("ally.startup.characterReady");
                        setCharacterReady(true);
                      }}
                      onError={() => {
                        // 纹理失败仍有完整的同色形象，保留实例并接续字标。
                        setCharacterReady(true);
                        setCharacterArrived(true);
                      }}
                    />
                  ) : (
                    <span aria-hidden="true" className="block size-full" />
                  )}
                </Suspense>
              </div>
            </div>
            <div className="welcome-wordmark" data-intro={intro}>
              {logoVisible ? (
                <Suspense fallback={<span className="welcome-logo" />}>
                  <ElasticLogo
                    key={guideShown ? "guide" : "welcome"}
                    intro={intro || guideShown}
                    onIntroComplete={() => setLogoFinished(true)}
                  />
                </Suspense>
              ) : (
                <span className="welcome-logo" aria-hidden="true" />
              )}
            </div>
          </div>
        </div>
        {guideShown && (
          <aside
            className="welcome-guide"
            onAnimationEnd={(event) => {
              if (event.target === event.currentTarget) setGuideSettled(true);
            }}
          >
            {children}
          </aside>
        )}
      </div>
    </main>
  );
}
