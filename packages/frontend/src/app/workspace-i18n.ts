import { i18n } from "@/shared/i18n";
import { pluginEn, pluginZh } from "@/shared/i18n/plugin";

// 插件文案随工作台注册，欢迎与连接阶段无需加载资源管理文案。
i18n.addResourceBundle("zh-CN", "plugin", pluginZh);
i18n.addResourceBundle("en-US", "plugin", pluginEn);
