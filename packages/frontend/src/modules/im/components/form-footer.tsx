import type { ReactNode } from "react";
import { SettingsPageFooter } from "@/modules/settings";
import { DialogFooter } from "@/shared/ui/dialog";

export function FormFooter({
  host,
  children,
}: {
  host: "page" | "dialog";
  children: ReactNode;
}) {
  return host === "dialog" ? (
    <DialogFooter>{children}</DialogFooter>
  ) : (
    <SettingsPageFooter>
      <div className="flex w-full flex-wrap items-center justify-end gap-2">
        {children}
      </div>
    </SettingsPageFooter>
  );
}
