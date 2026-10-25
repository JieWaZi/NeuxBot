import { useTranslation } from "react-i18next";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as Team } from "@/shared/api/generated/types.gen";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import {
  permissionLevelOf,
  permissionLevelTranslationKeys,
} from "@/shared/lib/runtime-permissions";
import { Avatar } from "@/shared/ui/avatar";
import type { TaskConfiguration } from "../model/types";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { ReadOnlyField } from "@/shared/ui/read-only-field";

/** 团队任务沿用成员配置，展示值与共享执行字段使用同一目录和产品名称。 */
export function TeamExecutionSummary({
  team,
  overrides = [],
}: {
  team: Team;
  overrides?: TaskConfiguration["memberConfigs"] | null;
}) {
  const { t } = useTranslation("automaticTask");
  const configs = overrides || [];
  return (
    <div className="grid min-w-0 gap-5">
      <p className="text-content-secondary type-body">
        {t("teamConfigurationHint")}
      </p>
      {team.members?.map((member) => (
        <MemberExecutionSummary
          key={member.waker?.id}
          member={{
            ...member,
            ...configs.find((item) => item.wakerId === member.waker?.id),
          }}
          customized={configs.some((item) => item.wakerId === member.waker?.id)}
          leadWakerId={team.channel?.leadWakerId}
        />
      ))}
    </div>
  );
}

function MemberExecutionSummary({
  member,
  leadWakerId,
  customized,
}: {
  member: NonNullable<Team["members"]>[number];
  leadWakerId?: string;
  customized: boolean;
}) {
  const { t } = useTranslation("automaticTask");
  const { runtime, accounts, models, reasoningOptions, selection } =
    useExecutionSelection({ value: member });
  const account = accounts.find((item) => item.id === selection.accountId);
  return (
    <section className="grid min-w-0 gap-4">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Avatar
          size="xs"
          name={member.waker?.name || ""}
          src={member.waker?.avatar}
          interactive={false}
          animated={false}
        />
        <h3 className="type-label min-w-0 font-medium [overflow-wrap:anywhere]">
          {member.waker?.name}
        </h3>
        {customized ? (
          <span className="text-content-secondary type-caption">
            {t("customMemberConfiguration")}
          </span>
        ) : null}
        {member.waker?.id === leadWakerId ? (
          <span className="text-content-secondary type-caption">
            {t("teamLead")}
          </span>
        ) : null}
      </div>
      <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-x-8 gap-y-4">
        {[
          [
            "runtime",
            <span className="inline-flex min-w-0 items-start gap-2">
              <RuntimeCliIcon
                runtimeId={runtime?.baseRuntimeId || member.runtimeId || ""}
              />
              {runtimeDisplayName(runtime || { id: member.runtimeId })}
            </span>,
          ],
          [
            "account",
            selection.accountId === "system"
              ? t("nativeAccount")
              : account?.name || t("configurationInvalid"),
          ],
          [
            "model",
            models.find((item) => item.id === member.model)?.name ||
              member.model ||
              t("common:executionSelection.inheritModel"),
          ],
          [
            "reasoning",
            reasoningOptions.find((item) => item.id === member.reasoning)
              ?.name ||
              member.reasoning ||
              t("common:executionSelection.defaultReasoning"),
          ],
          [
            "permission",
            t(
              `common:${permissionLevelTranslationKeys[permissionLevelOf(member.permissionLevel) || "default"].label}`,
            ),
          ],
        ].map(([label, value]) => (
          <ReadOnlyField key={String(label)} label={t(String(label))}>
            {value}
          </ReadOnlyField>
        ))}
      </dl>
    </section>
  );
}
