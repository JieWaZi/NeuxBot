import { memo } from "react";
import {
  File,
  FileArchive,
  FileCode2,
  FileImage,
  FileJson,
  FileText,
  Folder,
  FolderOpen,
} from "lucide-react";

type WorkspaceFileTypeIconProps = {
  name: string;
  isDirectory: boolean;
  isOpen?: boolean;
  size?: number;
  className?: string;
};

/**
 * 使用 VS Code 风格文件和目录图标，统一所有工作区来源的图标尺寸与布局。
 */
export const WorkspaceFileTypeIcon = memo(function WorkspaceFileTypeIcon({
  className,
  isDirectory,
  isOpen,
  name,
  size = 16,
}: WorkspaceFileTypeIconProps) {
  const Icon = isDirectory ? (isOpen ? FolderOpen : Folder) : iconForFile(name);
  return (
    <span
      aria-hidden="true"
      className={className ?? "text-muted-foreground"}
      style={{
        alignItems: "center",
        display: "inline-flex",
        flexShrink: 0,
        height: size,
        justifyContent: "center",
        width: size,
      }}
    >
      <Icon size={size} strokeWidth={1.5} />
    </span>
  );
});

function iconForFile(name: string) {
  const extension = name.split(".").at(-1)?.toLowerCase();
  if (
    extension &&
    ["js", "jsx", "ts", "tsx", "go", "rs", "py", "css", "html", "vue"].includes(
      extension,
    )
  ) {
    return FileCode2;
  }
  if (
    extension &&
    ["json", "jsonl", "yaml", "yml", "toml"].includes(extension)
  ) {
    return FileJson;
  }
  if (extension && ["md", "markdown", "txt", "log"].includes(extension)) {
    return FileText;
  }
  if (
    extension &&
    ["png", "jpg", "jpeg", "gif", "webp", "svg"].includes(extension)
  ) {
    return FileImage;
  }
  if (extension && ["zip", "tar", "gz", "tgz"].includes(extension)) {
    return FileArchive;
  }
  return File;
}
