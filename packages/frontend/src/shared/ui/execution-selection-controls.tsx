import { useState } from "react";
import {
  ArrowLeft,
  ChevronDown,
  ShieldAlert,
  ShieldKeyhole,
  Sparkles,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as AiAccount,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption,
} from "@/shared/api/generated/types.gen";
import { accountSupportsRuntime } from "@/shared/lib/account-runtime-compatibility";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { cn } from "@/shared/lib/cn";
import {
  permissionLevelOf,
  permissionLevelToneClasses,
  permissionLevelTranslationKeys,
  permissionLevels,
  type PermissionLevel,
} from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { OverflowText } from "@/shared/ui/overflow-text";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";

const permissionLevelIcons = {
  default: ShieldKeyhole,
  auto: Sparkles,
  "full-access": ShieldAlert,
} satisfies Record<PermissionLevel, typeof ShieldKeyhole>;

/** 聊天与指令输入共用智能体菜单；账号选择只展示兼容账号。 */
export function ExecutionRuntimeControl({
  runtimes,
  accounts,
  selectedRuntime,
  historicalRuntime,
  selectedAccountId,
  onSelect,
  disabled = false,
}: {
  runtimes: RuntimeOption[];
  accounts: AiAccount[];
  selectedRuntime?: RuntimeOption;
  historicalRuntime?: RuntimeOption;
  selectedAccountId?: string;
  onSelect: (runtime: RuntimeOption, accountId: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation(["chat", "common"]);
  const [accountRuntimeId, setAccountRuntimeId] = useState<string | null>(null);
  const accountRuntime = runtimes.find(
    (runtime) => runtime.id === accountRuntimeId,
  );
  const compatibleAccountsFor = (runtime: RuntimeOption) =>
    selectedAccountId === undefined
      ? []
      : accounts.filter((account) =>
          accountSupportsRuntime(
            account,
            runtime.baseRuntimeId || runtime.id || "",
          ),
        );
  const selectedAccount = accounts.find(
    (account) => account.id === selectedAccountId,
  );
  const runtimeControl = (
    <>
      <DropdownMenu
        onOpenChange={(open) => {
          if (!open) setAccountRuntimeId(null);
        }}
      >
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            disabled={disabled}
            data-testid="composer-runtime-control"
            size="sm"
            variant="ghost"
            className="max-w-[220px]"
          >
            <RuntimeCliIcon
              runtimeId={(selectedRuntime ?? historicalRuntime)?.id}
            />
            <span className="truncate">
              {runtimeDisplayName(selectedRuntime ?? historicalRuntime) ||
                t("chat:runtimeUnavailable")}
              {selectedAccountId !== undefined && selectedAccount ? (
                <> · {selectedAccount.name}</>
              ) : null}
            </span>
            <ChevronDown className="text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="w-[320px]"
          onCloseAutoFocus={(event) => event.preventDefault()}
        >
          {accountRuntime ? (
            <>
              <DropdownMenuItem
                aria-label={t("chat:backToRuntimeList")}
                onSelect={(event) => {
                  event.preventDefault();
                  setAccountRuntimeId(null);
                }}
                className="border-border mb-1 border-b"
              >
                <ArrowLeft aria-hidden />
                <span className="font-medium">
                  {runtimeDisplayName(accountRuntime)}
                </span>
              </DropdownMenuItem>
              <DropdownMenuRadioGroup
                value={
                  selectedRuntime?.id === accountRuntime.id
                    ? selectedAccountId
                    : ""
                }
              >
                <DropdownMenuRadioItem
                  value="system"
                  onSelect={() => onSelect(accountRuntime, "system")}
                >
                  {t("aiAccounts:system")}
                </DropdownMenuRadioItem>
                {compatibleAccountsFor(accountRuntime).map((account) => (
                  <DropdownMenuRadioItem
                    key={account.id}
                    value={account.id ?? ""}
                    onSelect={() => onSelect(accountRuntime, account.id ?? "")}
                  >
                    {account.name}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </>
          ) : (
            <>
              <DropdownMenuLabel>{t("chat:runtimeSection")}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={selectedRuntime?.id ?? ""}>
                {runtimes
                  .filter((item) => item.state === "ready")
                  .map((item) => {
                    const hasAccountChoices =
                      compatibleAccountsFor(item).length > 0;
                    return (
                      <DropdownMenuRadioItem
                        key={item.id}
                        value={item.id ?? ""}
                        disabled={!item.enabled}
                        onSelect={(event) => {
                          if (!item.enabled) return;
                          if (hasAccountChoices) {
                            event.preventDefault();
                            setAccountRuntimeId(item.id ?? null);
                          } else if (
                            selectedRuntime?.id !== item.id ||
                            selectedAccountId !== "system"
                          ) {
                            onSelect(item, "system");
                          }
                        }}
                        className="min-h-row-rich grid grid-cols-[32px_minmax(0,1fr)_auto] items-center px-2 py-1"
                      >
                        <span className="bg-muted grid size-8 place-items-center rounded-sm">
                          <RuntimeCliIcon runtimeId={item.id} />
                        </span>
                        <span className="min-w-0">
                          <strong className="type-body block truncate font-medium">
                            {runtimeDisplayName(item)}
                          </strong>
                          <small className="text-muted-foreground type-caption block truncate">
                            {!item.enabled
                              ? t("chat:runtimeDisabled")
                              : item.version || t("chat:ready")}
                          </small>
                        </span>
                      </DropdownMenuRadioItem>
                    );
                  })}
              </DropdownMenuRadioGroup>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
  return runtimeControl;
}

/** 权限菜单共用语义色、图标和能力校验；紧凑上下文条可传入已有触发器样式。 */
export function ExecutionPermissionControl({
  selectedPermissionLevel,
  supportedPermissions,
  onChange,
  disabled = false,
  triggerClassName,
}: {
  selectedPermissionLevel: PermissionLevel;
  supportedPermissions: ReadonlySet<PermissionLevel>;
  onChange: (level: PermissionLevel) => void;
  disabled?: boolean;
  triggerClassName?: string;
}) {
  const { t } = useTranslation("common");
  const permissionDetail =
    permissionLevelTranslationKeys[selectedPermissionLevel];
  const permissionTone = permissionLevelToneClasses[selectedPermissionLevel];
  const PermissionIcon = permissionLevelIcons[selectedPermissionLevel];
  const permissionLabel = t(`common:${permissionDetail.label}`);
  const permissionControl = (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          disabled={disabled}
          data-testid="composer-permission-control"
          data-overflow-trigger
          aria-label={t("common:permission.pickerAria", {
            name: permissionLabel,
          })}
          variant="ghost"
          size="sm"
          className={cn(
            triggerClassName,
            "max-w-28 font-medium",
            permissionTone.trigger,
          )}
        >
          <PermissionIcon className={permissionTone.icon} aria-hidden />
          <OverflowText className="min-w-0">{permissionLabel}</OverflowText>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuRadioGroup
          value={selectedPermissionLevel}
          onValueChange={(permissionLevel) => {
            const nextLevel = permissionLevelOf(permissionLevel);
            if (!nextLevel) return;
            onChange(nextLevel);
          }}
        >
          {permissionLevels.map((level) => {
            const detail = permissionLevelTranslationKeys[level];
            const tone = permissionLevelToneClasses[level];
            const available = supportedPermissions.has(level);
            const Icon = permissionLevelIcons[level];
            return (
              <DropdownMenuRadioItem
                key={level}
                value={level}
                disabled={!available}
                className="min-h-row-rich items-start py-2"
              >
                <Icon className={cn("size-icon-sm mt-0.5", tone.icon)} />
                <span className="min-w-0 flex-1">
                  <span className={cn("type-ui block font-medium", tone.title)}>
                    {t(`common:${detail.label}`)}
                  </span>
                  <span className="text-muted-foreground type-caption mt-0.5 block whitespace-normal">
                    {available
                      ? t(`common:${detail.description}`)
                      : t("common:permission.unsupported")}
                  </span>
                </span>
              </DropdownMenuRadioItem>
            );
          })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return permissionControl;
}
