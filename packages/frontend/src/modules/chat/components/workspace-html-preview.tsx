import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  isAbsoluteFilePath,
  localFileURL,
} from "@/shared/workspace/file-reference";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { Spinner } from "@/shared/ui/spinner";
import { createBasicHtmlDocument } from "../workbench/html/basic-document";
import { useBrowserSurface } from "./use-browser-surface";

/** HTML 预览：默认互动模式加载磁盘文件；可切换为无脚本的静态隔离文档。 */
export function WorkspaceHTMLPreview({
  active = true,
  interactive = true,
  rootPath,
  path,
  content,
  refreshToken,
  chatId,
}: {
  active?: boolean;
  interactive?: boolean;
  rootPath: string;
  path: string;
  content: string;
  refreshToken: number;
  chatId?: string;
}) {
  const { t } = useTranslation("chat");
  const basicDocument = useMemo(() => {
    try {
      return createBasicHtmlDocument(new TextEncoder().encode(content));
    } catch {
      return undefined;
    }
  }, [content]);

  return (
    <div
      className="absolute inset-0 flex min-h-0 flex-col"
      data-workspace-html-preview
    >
      {interactive ? (
        <InteractiveHTMLPreview
          active={active}
          rootPath={rootPath}
          path={path}
          refreshToken={refreshToken}
          chatId={chatId}
        />
      ) : basicDocument ? (
        <iframe
          title={t("workspace.htmlStaticPreview")}
          className="bg-surface min-h-0 w-full flex-1 border-0"
          sandbox=""
          srcDoc={basicDocument}
        />
      ) : (
        <p className="text-muted-foreground type-body p-4" role="alert">
          {t("workspace.fileLoadError")}
        </p>
      )}
    </div>
  );
}

/** 交互模式沿用隔离的 Electron Guest，只显示磁盘上已保存的内容。 */
function InteractiveHTMLPreview({
  active,
  rootPath,
  path,
  refreshToken,
  chatId,
}: {
  active: boolean;
  rootPath: string;
  path: string;
  refreshToken: number;
  chatId?: string;
}) {
  const { t } = useTranslation("chat");
  const sceneActive = useRouteSceneActive();
  const [url, setURL] = useState(() =>
    localFileURL(isAbsoluteFilePath(path) ? path : `${rootPath}/${path}`),
  );
  const { hostRef, browserReady, error, runBrowserAction } = useBrowserSurface({
    active: active && sceneActive,
    url,
    workspaceRoot: rootPath,
    chatId,
    onURLChange: setURL,
  });
  const previousRefresh = useRef(refreshToken);
  useEffect(() => {
    if (previousRefresh.current === refreshToken) return;
    previousRefresh.current = refreshToken;
    runBrowserAction("reload");
  }, [refreshToken, runBrowserAction]);
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {error ? (
        <p role="alert" className="text-muted-foreground type-body p-4">
          {error}
        </p>
      ) : null}
      {!error && !browserReady ? (
        <div
          role="status"
          aria-label={t("workspace.previewLoading")}
          className="absolute inset-0 grid place-items-center"
        >
          <Spinner />
        </div>
      ) : null}
      <div ref={hostRef} className="min-h-0 flex-1" />
    </div>
  );
}
