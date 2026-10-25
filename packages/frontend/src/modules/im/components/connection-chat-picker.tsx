import { useTranslation } from "react-i18next";
import { SettingsFieldGroup, SettingsFieldRow } from "@/modules/settings";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import type { IMBinding } from "../types";
import { BindingStatus } from "./connection-status";

export function ConnectionChatPicker({
  bindings,
  binding,
  disabled = false,
  onSelect,
}: {
  bindings: IMBinding[];
  binding: IMBinding;
  disabled?: boolean;
  onSelect: (id: string) => void;
}) {
  const { t } = useTranslation("im");
  return (
    <SettingsFieldGroup title={t("chatAccess")} description={t("chatNameHint")}>
      <SettingsFieldRow label={t("name")}>
        {bindings.length > 1 ? (
          <Select
            value={binding.id}
            onValueChange={onSelect}
            disabled={disabled}
          >
            <SelectTrigger aria-label={t("chooseChatAccess")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {bindings.map((item) => (
                <SelectItem key={item.id} value={item.id || ""}>
                  {item.name || item.externalChatId || t("chatAccess")}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <p className="type-ui text-content-secondary [overflow-wrap:anywhere]">
            {binding.name || binding.externalChatId || t("chatAccess")}
          </p>
        )}
      </SettingsFieldRow>
      <SettingsFieldRow label={t("authorization")}>
        <BindingStatus binding={binding} />
      </SettingsFieldRow>
    </SettingsFieldGroup>
  );
}
