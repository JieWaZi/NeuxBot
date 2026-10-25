import { TextMessagePartProvider } from "@assistant-ui/react";
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ComponentPropsWithoutRef,
} from "react";

import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
  statWorkspaceResource,
} from "@/shared/platform";
import {
  fileDirectory,
  resolveFileReference,
} from "@/shared/workspace/file-reference";
import { AssistantText } from "../../components/chat-message-content";
import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
import { markdownPreviewSource } from "./markdown-frontmatter";
import { localImagePath, rewriteLocalImageUrls } from "./markdown-local-images";

const maxInlineImageBytes = workspaceSingleFileBytesLimit;
const safeImageTypes = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
]);

type MarkdownImageProps = ComponentPropsWithoutRef<"img"> & {
  chatId?: string;
  rootPath: string;
  filePath: string;
  node?: unknown;
};

function MarkdownImage({
  chatId,
  rootPath,
  filePath,
  src,
  alt,
  node: _node,
  ...props
}: MarkdownImageProps) {
  void _node;
  const localPath = src ? localImagePath(src) : null;
  const [loaded, setLoaded] = useState<{ path: string; url: string }>();

  useEffect(() => {
    if (!chatId || !localPath) return;
    const controller = new AbortController();
    let objectUrl: string | undefined;
    void (async () => {
      try {
        let destination = localPath;
        try {
          destination = decodeURIComponent(localPath);
        } catch {
          // A literal percent sign in a filename remains literal.
        }
        const resolved = await resolveFileReference(
          { kind: "file", path: destination, raw: localPath },
          {
            chatId,
            baseDirectory: fileDirectory(filePath),
            workspaceRoot: rootPath,
            sourceRoots: [],
          },
        );
        if (resolved.status !== "resolved") return;
        const scope = await openWorkspaceResourceScope(chatId);
        const info = await statWorkspaceResource(
          scope.id,
          resolved.file.path,
          resolved.file.rootPath,
        );
        if (
          !info ||
          info.size > maxInlineImageBytes ||
          !safeImageTypes.has(info.mediaType)
        )
          return;
        const file = await readWorkspaceResource(
          scope.id,
          resolved.file.path,
          controller.signal,
          resolved.file.rootPath,
        );
        if (controller.signal.aborted) return;
        objectUrl = URL.createObjectURL(
          new Blob([Uint8Array.from(file.bytes)], { type: info.mediaType }),
        );
        setLoaded({ path: localPath, url: objectUrl });
      } catch {
        // Keep the alt text visible when an image is missing or unreadable.
      }
    })();
    return () => {
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [chatId, filePath, localPath, rootPath]);

  if (!localPath) return <img {...props} src={src} alt={alt ?? ""} />;
  const imageUrl = loaded?.path === localPath ? loaded.url : undefined;
  if (!imageUrl)
    return (
      <span
        role="img"
        aria-label={alt || localPath}
        className="text-muted-foreground"
      >
        {alt || localPath}
      </span>
    );
  return <img {...props} src={imageUrl} alt={alt ?? ""} />;
}

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
      <MarkdownImage
        {...props}
        chatId={chatId}
        rootPath={rootPath}
        filePath={filePath}
      />
    ),
    [chatId, filePath, rootPath],
  );

  return (
    <div data-workspace-markdown className="mx-auto max-w-4xl px-6 py-4">
      <TextMessagePartProvider text={content} isRunning={false}>
        <AssistantText renderImage={renderImage} />
      </TextMessagePartProvider>
    </div>
  );
}
