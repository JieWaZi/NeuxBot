import { useTranslation } from "react-i18next";
import { lazy, Suspense } from "react";
import { WorkflowGenerationDiagram } from "./workflow-generation-diagram";
const GenerationAnimation = lazy(() =>
  import("./workflow-generation-animation").then((module) => ({
    default: module.WorkflowGenerationAnimation,
  })),
);

/** 无数据画布始终展示流水线轮廓，辅助语义独立表达真实加载与生成状态。 */
export function WorkflowCanvasPlaceholder({
  state,
  active = true,
}: {
  state: "loading" | "generating" | "empty";
  active?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const diagram = (
    <div className="pointer-events-none h-full min-h-0 w-full max-w-6xl">
      <WorkflowGenerationDiagram />
    </div>
  );
  return (
    <div
      role={state === "empty" ? "img" : "status"}
      aria-label={t(state === "empty" ? "canvasPlaceholder" : state)}
      aria-busy={state !== "empty"}
      data-motion="view"
      data-state="open"
      className="surface-pattern-dots surface-pattern-dots-canvas flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden p-6"
    >
      <Suspense fallback={diagram}>
        <GenerationAnimation active={active} />
      </Suspense>
    </div>
  );
}
