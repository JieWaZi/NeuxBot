import { Copy } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { toast } from "@/shared/ui/sonner";
export function CopyValue({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation("automaticTask");
  const [copied, setCopied] = useState(false);
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="type-ui font-medium">{label}</span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-label={`${t("copy")} ${label}`}
          onClick={() => {
            void navigator.clipboard
              .writeText(value)
              .then(() => setCopied(true))
              .catch(() => toast.error(t("actionFailed")));
          }}
        >
          <Copy />
          {t(copied ? "copied" : "copy")}
        </Button>
      </div>
      <pre className="bg-surface-tertiary type-code overflow-x-auto rounded-md p-3 break-all whitespace-pre-wrap">
        {value}
      </pre>
    </div>
  );
}
