/**
 * 工作区中可以从资源入口打开的会话文件。
 * 消息附件和会话生成文件保存安全的相对路径；项目文件内容由桌面 Provider 按工作范围读取。
 */
export type WorkspaceFile = {
  name: string;
  path: string;
  source: "chat" | "project";
  mediaType?: string;
};
