import { ChevronDown, ChevronUp } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";

import type { Expertise, WorkMethod } from "../model/types";
import {
  wakerOverviewExpertiseCollapsedLimit,
  wakerOverviewWorkMethodsCollapsedLimit,
} from "./waker-overview-limits";
import { WakerAccentBlock } from "./waker-accent-block";

type WakerAboutSectionProps = {
  expertise?: Expertise[];
  workStyle?: string[];
  workMethods?: WorkMethod[];
};

/** 核心职能：固定两列网格，收起时最多两行 */
const expertiseGridClassName =
  "grid grid-cols-1 gap-3 @[24rem]/about:grid-cols-2";

/**
 * 工作方式：最多四列、按容器宽度自适应（窄屏自动降为 3/2/1 列）。
 * 使用 auto-fit 而非固定 @container 断点，避免资料左栏不足 40rem 时永远停在两列。
 */
const workMethodsGridClassName =
  "grid list-none grid-cols-[repeat(auto-fit,minmax(min(100%,calc((100%-4.5rem)/4)),1fr))] gap-x-6 gap-y-6";

function AboutSubsection({
  children,
  description,
  title,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0">
      <header className="mb-4">
        <h2 className="type-label font-semibold">{title}</h2>
        <p className="text-content-secondary type-caption mt-1">
          {description}
        </p>
      </header>
      {children}
    </section>
  );
}

function AboutExpandControls({
  expanded,
  hiddenCount,
  onToggle,
}: {
  expanded: boolean;
  hiddenCount: number;
  onToggle: () => void;
}) {
  const { t } = useTranslation("waker");
  if (hiddenCount <= 0) return null;

  return (
    <div className="mt-4">
      <Button size="sm" type="button" variant="ghost" onClick={onToggle}>
        {expanded ? (
          <>
            {t("about.showLess")}
            <ChevronUp />
          </>
        ) : (
          <>
            {t("about.expandMore", { count: hiddenCount })}
            <ChevronDown />
          </>
        )}
      </Button>
    </div>
  );
}

export function WakerAboutSection({
  expertise = [],
  workStyle = [],
  workMethods = [],
}: WakerAboutSectionProps) {
  const { t } = useTranslation("waker");
  const [expertiseExpanded, setExpertiseExpanded] = useState(false);
  const [workMethodsExpanded, setWorkMethodsExpanded] = useState(false);
  const empty = (
    <p className="text-content-secondary type-body">{t("about.empty")}</p>
  );

  const visibleExpertise = expertiseExpanded
    ? expertise
    : expertise.slice(0, wakerOverviewExpertiseCollapsedLimit);
  const hiddenExpertiseCount = Math.max(
    0,
    expertise.length - wakerOverviewExpertiseCollapsedLimit,
  );

  const visibleWorkMethods = workMethodsExpanded
    ? workMethods
    : workMethods.slice(0, wakerOverviewWorkMethodsCollapsedLimit);
  const hiddenWorkMethodsCount = Math.max(
    0,
    workMethods.length - wakerOverviewWorkMethodsCollapsedLimit,
  );

  return (
    <section
      aria-label={t("about.title")}
      className="@container/about grid min-w-0 gap-8"
    >
      <AboutSubsection
        description={t("about.expertiseLead")}
        title={t("about.expertise")}
      >
        {expertise.length ? (
          <>
            <div className={expertiseGridClassName}>
              {visibleExpertise.map((item, index) => (
                <WakerAccentBlock key={`${item.title}-${index}`}>
                  <h3 className="type-label font-medium">{item.title}</h3>
                  {item.description ? (
                    <p className="text-content-secondary type-body mt-2">
                      {item.description}
                    </p>
                  ) : null}
                </WakerAccentBlock>
              ))}
            </div>
            <AboutExpandControls
              expanded={expertiseExpanded}
              hiddenCount={hiddenExpertiseCount}
              onToggle={() => setExpertiseExpanded((value) => !value)}
            />
          </>
        ) : (
          empty
        )}
      </AboutSubsection>

      <AboutSubsection
        description={t("about.workStyleLead")}
        title={t("about.workStyle")}
      >
        {workStyle.length ? (
          <ul className="flex flex-wrap gap-2">
            {workStyle.map((item, index) => (
              <li key={`${item}-${index}`}>
                <span className="bg-card type-ui inline-flex max-w-full rounded-md px-2.5 py-1 font-medium [overflow-wrap:anywhere]">
                  {item}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          empty
        )}
      </AboutSubsection>

      <AboutSubsection
        description={t("about.workMethodsLead")}
        title={t("about.workMethods")}
      >
        {workMethods.length ? (
          <>
            <ul className={workMethodsGridClassName}>
              {visibleWorkMethods.map((method, index) => (
                <li
                  key={`${method.name}-${index}`}
                  className="min-w-0 [overflow-wrap:anywhere]"
                >
                  <h3 className="type-label font-medium">{method.name}</h3>
                  {(method.steps ?? []).length ? (
                    <ul className="text-content-secondary type-body mt-3 list-none space-y-1.5">
                      {(method.steps ?? []).map((step, stepIndex) => (
                        <li key={`${step}-${stepIndex}`}>{step}</li>
                      ))}
                    </ul>
                  ) : null}
                </li>
              ))}
            </ul>
            <AboutExpandControls
              expanded={workMethodsExpanded}
              hiddenCount={hiddenWorkMethodsCount}
              onToggle={() => setWorkMethodsExpanded((value) => !value)}
            />
          </>
        ) : (
          empty
        )}
      </AboutSubsection>
    </section>
  );
}
