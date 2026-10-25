import { lazy, Suspense, type ReactNode } from "react";
import type { WorkspaceFileContent } from "@/shared/platform";
import type { WorkspaceFileView } from "@/shared/workspace/workspace-tab";
import { WorkspaceHTMLPreview } from "./workspace-html-preview";
import { Spinner } from "@/shared/ui/spinner";
import { WorkspaceMarkdownPreview } from "../workbench/editor/workspace-markdown-preview";

const PdfPreview = lazy(() =>
  import("../workbench/pdfjs/pdfjs-preview").then((module) => ({
    default: module.PdfJsPreview,
  })),
);
const DocxPreview = lazy(() =>
  import("../workbench/office/docx-preview").then((module) => ({
    default: module.DocxPreview,
  })),
);
const PptxPreview = lazy(() =>
  import("../workbench/office/pptx-preview").then((module) => ({
    default: module.PptxPreview,
  })),
);
const ExcelPreview = lazy(() =>
  import("../workbench/excel/excel-preview").then((module) => ({
    default: module.ExcelPreview,
  })),
);

type FilePreviewProps = {
  active?: boolean;
  file: WorkspaceFileContent;
  path: string;
  rootPath: string;
  refreshToken: number;
  imageScale: number;
  scopeChatId?: string;
  htmlInteractive?: boolean;
};

type FileViewAdapter = {
  matches: (path: string, file?: WorkspaceFileContent) => boolean;
  views: readonly WorkspaceFileView[];
  defaultView: WorkspaceFileView;
  canEdit?: boolean;
  hasOutline?: boolean;
  canZoom?: boolean;
  renderPreview?: (props: FilePreviewProps) => ReactNode;
};

const fileViewAdapters: readonly FileViewAdapter[] = [
  {
    matches: (path) => /\.pdf$/i.test(path),
    views: ["preview"],
    defaultView: "preview",
    renderPreview: ({ file, rootPath, path, refreshToken, scopeChatId }) => (
      <Suspense fallback={<Spinner />}>
        <PdfPreview
          key={`${rootPath}/${path}:${file.version ?? refreshToken}`}
          chatId={scopeChatId}
          rootPath={rootPath}
          path={path}
          title={file.name}
        />
      </Suspense>
    ),
  },
  {
    matches: (path) => /\.docx$/i.test(path),
    views: ["preview"],
    defaultView: "preview",
    renderPreview: ({ file, rootPath, path, refreshToken, scopeChatId }) => (
      <Suspense fallback={<Spinner />}>
        <DocxPreview
          key={`${rootPath}/${path}:${file.version ?? refreshToken}`}
          chatId={scopeChatId}
          rootPath={rootPath}
          path={path}
          title={file.name}
        />
      </Suspense>
    ),
  },
  {
    matches: (path) => /\.pptx$/i.test(path),
    views: ["preview"],
    defaultView: "preview",
    renderPreview: ({ file, rootPath, path, refreshToken, scopeChatId }) => (
      <Suspense fallback={<Spinner />}>
        <PptxPreview
          key={`${rootPath}/${path}:${file.version ?? refreshToken}`}
          chatId={scopeChatId}
          rootPath={rootPath}
          path={path}
          title={file.name}
        />
      </Suspense>
    ),
  },
  {
    matches: (path) => /\.(?:xlsx|xls|csv|tsv)$/i.test(path),
    views: ["preview"],
    defaultView: "preview",
    renderPreview: ({ file, rootPath, path, refreshToken, scopeChatId }) => (
      <Suspense fallback={<Spinner />}>
        <ExcelPreview
          key={`${rootPath}/${path}:${file.version ?? refreshToken}`}
          chatId={scopeChatId}
          rootPath={rootPath}
          path={path}
          title={file.name}
        />
      </Suspense>
    ),
  },
  {
    matches: (path) => /\.(?:md|markdown)$/i.test(path),
    views: ["code", "preview"],
    defaultView: "preview",
    canEdit: true,
    hasOutline: true,
    renderPreview: ({ file, path, rootPath, scopeChatId }) => (
      <WorkspaceMarkdownPreview
        source={file.content ?? ""}
        path={path}
        rootPath={rootPath}
        chatId={scopeChatId}
      />
    ),
  },
  {
    matches: (path) => /\.(?:html?|xhtml)$/i.test(path),
    views: ["code", "preview"],
    defaultView: "code",
    canEdit: true,
    renderPreview: ({
      active,
      file,
      rootPath,
      path,
      refreshToken,
      scopeChatId,
      htmlInteractive,
    }) => (
      <WorkspaceHTMLPreview
        active={active}
        interactive={htmlInteractive}
        key={`${rootPath}/${path}`}
        rootPath={rootPath}
        path={path}
        content={file.content ?? ""}
        refreshToken={refreshToken}
        chatId={scopeChatId}
      />
    ),
  },
  {
    matches: (_path, file) => file?.kind === "image",
    views: ["preview"],
    defaultView: "preview",
    canZoom: true,
    renderPreview: ({ file, imageScale }) => (
      <div className="grid min-h-full min-w-full place-items-center p-4">
        <img
          alt={file.name}
          src={file.dataUrl}
          className="max-w-none select-none"
          draggable={false}
          style={{
            transform: `scale(${imageScale})`,
            transformOrigin: "center",
          }}
        />
      </div>
    ),
  },
];

const textAdapter: FileViewAdapter = {
  matches: () => true,
  views: ["code"],
  defaultView: "code",
  canEdit: true,
};

/** 类型差异集中在适配器；查看器只消费模式、工具能力与渲染入口。 */
export function resolveFileViewAdapter(
  path: string,
  file?: WorkspaceFileContent,
): FileViewAdapter {
  return (
    fileViewAdapters.find((adapter) => adapter.matches(path, file)) ??
    textAdapter
  );
}
