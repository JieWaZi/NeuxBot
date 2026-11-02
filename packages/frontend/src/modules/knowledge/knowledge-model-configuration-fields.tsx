import type { ComponentProps } from "react";
import { ExecutionConfigurationFields } from "@/shared/ui/execution-configuration-fields";
import { useCopy } from "./use-extended-copy";

/** 知识模型配置沿用共享控件，能力反馈与文案随知识表单加载。 */
export function KnowledgeModelConfigurationFields(
  props: ComponentProps<typeof ExecutionConfigurationFields>,
) {
  const t = useCopy();
  return (
    <>
      <ExecutionConfigurationFields {...props} reasoningDefaultHidden />
      <p className="text-content-secondary type-caption">
        {t("modelReasoningHelp")}
      </p>
    </>
  );
}
