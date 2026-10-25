import { lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";

import { WelcomeBrand } from "./welcome/welcome-brand";

const StartupErrorPanel = lazy(() => import("./startup-error-panel"));

export type StartupStage = "interface" | "connection" | "workspace";

export function StartupScreen({
  stage,
  error,
  onRetry,
  intro = true,
  onIntroComplete,
}: {
  stage: StartupStage;
  error?: string;
  onRetry?: () => void;
  intro?: boolean;
  onIntroComplete?: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <>
      <WelcomeBrand intro={intro} onIntroComplete={onIntroComplete}>
        {error ? (
          <Suspense
            fallback={
              <p
                role="alert"
                className="text-destructive type-body break-words"
              >
                {error}
              </p>
            }
          >
            <StartupErrorPanel error={error} onRetry={onRetry} />
          </Suspense>
        ) : undefined}
      </WelcomeBrand>
      {!error && (
        <span className="sr-only" role="status" aria-live="polite">
          {t(`startup.status.${stage}`)}
        </span>
      )}
    </>
  );
}
