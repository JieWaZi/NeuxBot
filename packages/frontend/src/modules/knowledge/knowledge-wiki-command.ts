import type { WikiPage, WikiTheme } from "./knowledge-wiki-queries";

/** 仅保存尚未确认的用户命令；正文权威仍是服务端，关闭弹窗不重置写入身份。 */
export type KnowledgeWikiCommand = {
  kind:
    | "folder"
    | "page"
    | "edit"
    | "rename"
    | "delete-folder"
    | "delete-page"
    | "restore-page";
  revisionVersion?: number;
  requestId: string;
  structureVersion: number;
  draft?: {
    name: string;
    slug: string;
    summary: string;
    content: string;
    parent: string;
    type: string;
  };
  page?: WikiPage;
  theme?: WikiTheme;
};
export type KnowledgeWikiCommands = Map<string, KnowledgeWikiCommand>;
