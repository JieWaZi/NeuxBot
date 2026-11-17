import { i18n } from "@/shared/i18n";
import { workspaceResources } from "@/shared/i18n/workspace-resources";
import { automaticTaskEn, automaticTaskZh } from "@/shared/i18n/automatic-task";
import { pluginEn, pluginZh } from "@/shared/i18n/plugin";
import { workflowEn, workflowZh } from "@/shared/i18n/workflow";

// 工作台首次使用前同步注册领域文案，欢迎与连接阶段只加载基础文案。
for (const [language, namespaces] of Object.entries(workspaceResources)) {
  for (const [namespace, resources] of Object.entries(namespaces)) {
    i18n.addResourceBundle(language, namespace, resources);
  }
}
i18n.addResourceBundle("zh-CN", "plugin", pluginZh);
i18n.addResourceBundle("en-US", "plugin", pluginEn);
i18n.addResourceBundle("zh-CN", "workflow", workflowZh);
i18n.addResourceBundle("en-US", "workflow", workflowEn);
i18n.addResourceBundle("zh-CN", "automaticTask", automaticTaskZh);
i18n.addResourceBundle("en-US", "automaticTask", automaticTaskEn);
