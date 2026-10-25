import { useEffect, useState } from "react";
import { ChevronDown } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as Team } from "@/shared/api/generated/types.gen";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import type { TaskConfiguration } from "../model/types";

type MemberConfiguration = NonNullable<
  TaskConfiguration["memberConfigs"]
>[number];

/** 输入框内按成员配置任务覆盖；未编辑成员在每次运行时继承团队设置。 */
export function TeamExecutionControls({
  team,
  overrides,
  onChange,
  onValidityChange,
  disabled,
}: {
  team: Team;
  overrides: MemberConfiguration[];
  onChange: (value: MemberConfiguration[]) => void;
  onValidityChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation("automaticTask");
  const [memberId, setMemberId] = useState(team.channel?.leadWakerId || "");
  const member =
    team.members?.find((item) => item.waker?.id === memberId) ||
    team.members?.[0];
  const selectedId = member?.waker?.id || "";
  const override = overrides.find((item) => item.wakerId === selectedId);
  const state = useExecutionSelection({
    value: override || member || {},
    enabled: Boolean(member),
  });
  const missingMember = overrides.some(
    (item) =>
      !team.members?.some((candidate) => candidate.waker?.id === item.wakerId),
  );
  const valid = Boolean(member && state.valid && !missingMember);
  useEffect(() => onValidityChange(valid), [onValidityChange, valid]);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={disabled || !member}
            aria-label={t("configurationMember")}
            className="max-w-[220px]"
          >
            <Avatar
              name={member?.waker?.name || ""}
              src={member?.waker?.avatar}
              size="xs"
              interactive={false}
              animated={false}
            />
            <span className="truncate">{member?.waker?.name}</span>
            <ChevronDown className="size-icon-xs" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup
            value={selectedId}
            onValueChange={setMemberId}
          >
            {team.members?.map((item) =>
              item.waker?.id ? (
                <DropdownMenuRadioItem
                  key={item.waker.id}
                  value={item.waker.id}
                >
                  <Avatar
                    name={item.waker.name || ""}
                    src={item.waker.avatar}
                    size="xs"
                    interactive={false}
                    animated={false}
                  />
                  {item.waker.name}
                  {item.waker.id === team.channel?.leadWakerId ? (
                    <span className="text-content-secondary type-caption">
                      {t("teamLead")}
                    </span>
                  ) : null}
                  {overrides.some(
                    (config) => config.wakerId === item.waker?.id,
                  ) ? (
                    <span className="text-content-secondary type-caption">
                      {t("customMemberConfiguration")}
                    </span>
                  ) : null}
                </DropdownMenuRadioItem>
              ) : null,
            )}
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={!override}
            onSelect={() =>
              onChange(overrides.filter((item) => item.wakerId !== selectedId))
            }
          >
            {t("inheritTeamConfiguration")}
          </DropdownMenuItem>
          {missingMember ? (
            <DropdownMenuItem onSelect={() => onChange([])}>
              {t("inheritAllTeamConfiguration")}
            </DropdownMenuItem>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
      {missingMember ? (
        <span
          role="alert"
          className="text-destructive type-caption w-full text-right"
        >
          {t("memberConfigurationInvalid")}
        </span>
      ) : null}
      <ExecutionConfigurationControls
        key={selectedId}
        state={state}
        disabled={disabled || !member}
        onChange={(selection) =>
          onChange([
            ...overrides.filter((item) => item.wakerId !== selectedId),
            { wakerId: selectedId, ...selection },
          ])
        }
      />
    </>
  );
}
