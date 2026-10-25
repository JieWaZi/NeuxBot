import { useTranslation } from "react-i18next";
import { WelcomeBrand } from "../welcome/welcome-brand";

export function WelcomeScene() {
  const { t } = useTranslation("common");
  return (
    <section
      aria-label={t("welcomeScene.ariaLabel")}
      className="bg-surface-scene grid h-full min-h-0 min-w-0 overflow-auto"
      role="region"
    >
      <WelcomeBrand intro={false} />
    </section>
  );
}
