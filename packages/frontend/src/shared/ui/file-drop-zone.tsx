import { useId, useState } from "react";
import { Upload } from "lucide-react";
import { cn } from "@/shared/lib/cn";

/** 文件选择由宿主负责；拖入的浏览器 File 交给调用方校验和暂存。 */
export function FileDropZone({
  label,
  description,
  fileName,
  disabled = false,
  onChoose,
  onFiles,
}: {
  label: string;
  description: string;
  fileName?: string;
  disabled?: boolean;
  onChoose: () => void;
  onFiles: (files: File[]) => void;
}) {
  const descriptionId = useId();
  const fileNameId = useId();
  const [dragging, setDragging] = useState(false);
  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={
        fileName ? `${fileNameId} ${descriptionId}` : descriptionId
      }
      disabled={disabled}
      onClick={onChoose}
      onDragOver={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (
          !disabled &&
          Array.from(event.dataTransfer.types).includes("Files")
        ) {
          event.dataTransfer.dropEffect = "copy";
          setDragging(true);
        }
      }}
      onDragLeave={(event) => {
        if (
          !(event.relatedTarget instanceof Node) ||
          !event.currentTarget.contains(event.relatedTarget)
        )
          setDragging(false);
      }}
      onDrop={(event) => {
        event.preventDefault();
        event.stopPropagation();
        setDragging(false);
        if (!disabled) onFiles(Array.from(event.dataTransfer.files));
      }}
      className={cn(
        "border-border-default bg-surface-subtle hover:bg-surface-tertiary focus-visible:ring-ring flex min-h-36 w-full min-w-0 flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-6 py-6 outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-45",
        dragging && !disabled && "border-accent-border bg-surface-tertiary",
      )}
    >
      <Upload
        aria-hidden="true"
        className="text-content-secondary size-icon-xl"
      />
      <span
        id={fileNameId}
        className="type-body max-w-full truncate font-medium"
      >
        {fileName || label}
      </span>
      <span id={descriptionId} className="text-content-secondary type-caption">
        {description}
      </span>
    </button>
  );
}
