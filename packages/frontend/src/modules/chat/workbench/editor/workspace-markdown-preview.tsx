import { useCallback, useMemo, type ComponentPropsWithoutRef } from "react";

import { fileDirectory } from "@/shared/workspace/file-reference";
import { WorkspaceMarkdownImage } from "@/shared/workspace/workspace-markdown-image";
import { AssistantMarkdown } from "../../components/chat-message-content";
import { markdownPreviewSource } from "./markdown-frontmatter";
import { rewriteLocalImageUrls } from "./markdown-local-images";

export function WorkspaceMarkdownPreview({
  source,
  chatId,
  rootPath,
  path,
}: {
  source: string;
  chatId?: string;
  rootPath: string;
  path: string;
}) {
  const content = useMemo(
    () => rewriteLocalImageUrls(markdownPreviewSource(source)),
    [source],
  );
  const filePath = `${rootPath.replace(/[\\/]$/, "")}/${path}`;
  const renderImage = useCallback(
    (props: ComponentPropsWithoutRef<"img">) => (
      <WorkspaceMarkdownImage
        {...props}
        allowRemote
        context={{
          chatId,
          baseDirectory: fileDirectory(filePath),
          workspaceRoot: rootPath,
          sourceRoots: [],
        }}
      />
    ),
    [chatId, filePath, rootPath],
  );

  return (
    <div data-workspace-markdown className="mx-auto max-w-4xl px-6 py-4">
      <AssistantMarkdown text={content} renderImage={renderImage} />
    </div>
  );
}
