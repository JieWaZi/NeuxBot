import {
  lazy,
  Suspense,
  useEffect,
  useState,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import { useTheme } from "@/shared/theme";
import bodies from "../../../../../vendor/companion/styles.json";
import plush256 from "../../../../../vendor/companion/assets/bodies/plush-256.png";
import plush512 from "../../../../../vendor/companion/assets/bodies/plush-512.png";

const ElasticLogo = lazy(() => import("./elastic-logo"));
let assetsReady: Promise<void> | undefined;
/** 只预热代码，不提前创建 GPU 或启动字标动画；失败仍由各组件负责静态降级。 */
export function prepareWelcomeAssets(): Promise<void> {
  return (assetsReady ??= Promise.allSettled([
    import("../../../../../vendor/companion/renderer"),
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
  const reducedMotion = useSyncExternalStore(
    watchReducedMotion,
    readReducedMotion,
    () => true,
  );
  const notified = useRef(false);
  const [characterReady, setCharacterReady] = useState(false);
  const [characterArrived, setCharacterArrived] = useState(false);
  const [logoFinished, setLogoFinished] = useState(false);
  const [characterFailed, setCharacterFailed] = useState(false);
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
                {!intro && !characterReady && !characterFailed && (
                  <div className="welcome-character-placeholder absolute inset-0 size-full object-contain">
                    <img
                      src={plush256}
                      srcSet={`${plush256} 256w, ${plush512} 512w`}
                      sizes="clamp(112px, calc(18vw - 17.28px), 192px)"
                      alt=""
                      className="size-full object-contain"
                    />
                    <img
                      src="/welcome-eyes.svg"
                      alt=""
                      className="absolute inset-0 size-full"
                    />
                  </div>
                )}
                {characterFailed ? (
                  <img
                    src={
                      resolvedTheme === "dark"
                        ? "/ally-logo-dark.svg"
                        : "/ally-logo.svg"
                    }
                    alt=""
                    className="size-full object-contain"
                  />
                ) : (
                  <CompanionAvatar
                    avatar={bodies.plush.avatar}
                    onReady={() => {
                      if (intro)
                        performance.mark?.("ally.startup.characterReady");
                      setCharacterReady(true);
                    }}
                    onError={() => {
                      setCharacterFailed(true);
                      setCharacterReady(true);
                      setCharacterArrived(true);
                    }}
                  />
                )}
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
