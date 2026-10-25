import { RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { nextCompanionAvatar } from "@/shared/companion/appearance";
import { Avatar } from "./avatar";
import { Button } from "./button";

export function CompanionPicker({
  label,
  name,
  value,
  onChange,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation("common");
  return (
    <div className="grid content-start gap-2">
      <div className="flex items-center gap-1">
        <span className="type-label font-medium">{label}</span>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t("companion.nextStyle")}
          title={t("companion.nextStyle")}
          onClick={() => onChange(nextCompanionAvatar(value))}
        >
          <RefreshCw />
        </Button>
      </div>
      <Avatar name={name} src={value} size="xl" />
    </div>
  );
}
