import { memo } from "react";
import { cn } from "@/shared/lib/cn";
import { workspaceFileAppearance } from "./workspace-file-icons";

type WorkspaceFileTypeIconProps = {
  name: string;
  isDirectory: boolean;
  isOpen?: boolean;
  size?: number | string;
  className?: string;
};

/**
 * 工作区和消息引用共用精选 Material Icon Theme 原始素材与尺寸。
 */
export const WorkspaceFileTypeIcon = memo(function WorkspaceFileTypeIcon({
  className,
  isDirectory,
  isOpen,
  name,
  size = 16,
}: WorkspaceFileTypeIconProps) {
  const { src } = workspaceFileAppearance(name, isDirectory, isOpen);
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex shrink-0 items-center justify-center",
        className,
      )}
      style={{
        height: size,
        width: size,
      }}
    >
      <img
        src={src}
        alt=""
        draggable={false}
        className="size-full object-contain"
      />
    </span>
  );
});
