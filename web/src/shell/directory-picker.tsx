import { useState } from "react";
import { createRoot } from "react-dom/client";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { webText } from "../i18n";

/** 浏览器无法得到本机目录选择器的真实路径，由用户明确指定 Core 目录。 */
export function selectCoreDirectory(): Promise<string | null> {
  return new Promise((resolve) => {
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const finish = (value: string | null) => {
      resolve(value);
      queueMicrotask(() => {
        root.unmount();
        container.remove();
      });
    };
    root.render(<DirectoryPicker onComplete={finish} />);
  });
}
function DirectoryPicker({
  onComplete,
}: {
  onComplete: (value: string | null) => void;
}) {
  const [path, setPath] = useState("");
  const [invalid, setInvalid] = useState(false);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onComplete(null);
      }}
    >
      <DialogContent
        title={webText("directoryTitle")}
        description={webText("directoryDescription")}
        size="sm"
      >
        <form
          className="flex flex-col gap-4 p-6"
          onSubmit={(event) => {
            event.preventDefault();
            const value = path.trim();
            if (!/^(?:\/|[A-Za-z]:[\\/])/.test(value)) {
              setInvalid(true);
              return;
            }
            onComplete(value);
          }}
        >
          <Input
            aria-label={webText("directoryPath")}
            value={path}
            onChange={(event) => {
              setPath(event.target.value);
              setInvalid(false);
            }}
            autoFocus
            autoComplete="off"
            spellCheck={false}
          />
          {invalid ? (
            <p className="text-destructive type-body" role="alert">
              {webText("directoryInvalid")}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => onComplete(null)}
            >
              {webText("cancel")}
            </Button>
            <Button type="submit">{webText("confirm")}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
