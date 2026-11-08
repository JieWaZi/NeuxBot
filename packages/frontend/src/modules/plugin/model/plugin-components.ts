/** 插件组件读取所属安装版本，或目录中明确的源码与归档快照。 */
export type PluginComponentContext = {
  catalogKey?: string;
  sourceCommit?: string;
  archiveSHA256?: string;
  pluginId?: string;
  versionId?: string;
  wakerId?: string;
};
