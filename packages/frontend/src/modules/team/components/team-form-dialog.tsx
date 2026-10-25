import { zodResolver } from "@hookform/resolvers/zod";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { Check, CircleAlert, Crown, Trash2 } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import type {
  GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail,
  GithubComJieWaZiNeuxBotServerApisChannelsChannelInput as ChannelInput,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWaker as Waker,
} from "@/shared/api/generated/types.gen";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { SearchField } from "@/shared/ui/search-field";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { ExecutionConfigurationFields } from "@/shared/ui/execution-configuration-fields";
import { Textarea } from "@/shared/ui/textarea";

import {
  createChannelMutation,
  getChannelQueryKey,
  teamQueryKeys,
  teamWakersInfiniteOptions,
  updateChannelMutation,
} from "../api/channel-queries";

const permissionLevelSchema = z.enum(["auto", "default", "full-access"]);
const memberSchema = z.object({
  accountId: z.string(),
  model: z.string(),
  permissionLevel: permissionLevelSchema,
  reasoning: z.string(),
  runtimeId: z.string().min(1),
  wakerId: z.string().min(1),
});
const teamFormSchema = z
  .object({
    description: z.string().trim().max(1000),
    leadWakerId: z.string(),
    members: z.array(memberSchema).min(2).max(5),
    name: z.string().trim().min(1),
  })
  .superRefine((values, context) => {
    if (
      !values.leadWakerId ||
      !values.members.some((member) => member.wakerId === values.leadWakerId)
    ) {
      context.addIssue({
        code: "custom",
        message: "lead-required",
        path: ["leadWakerId"],
      });
    }
  });

type TeamFormValues = z.infer<typeof teamFormSchema>;
type TeamMemberValues = TeamFormValues["members"][number];

type TeamFormDialogProps = {
  detail?: ChannelDetail;
  onOpenChange: (open: boolean) => void;
  open: boolean;
};

function permissionLevel(value?: string): TeamMemberValues["permissionLevel"] {
  return value === "default" || value === "full-access" ? value : "auto";
}

function initialValues(detail?: ChannelDetail): TeamFormValues {
  return {
    name: detail?.channel?.name ?? "",
    description: detail?.channel?.description ?? "",
    leadWakerId: detail?.channel?.leadWakerId ?? "",
    members: (detail?.members ?? []).flatMap((member) =>
      member.waker?.id
        ? [
            {
              accountId: member.accountId ?? "",
              wakerId: member.waker.id,
              runtimeId: member.runtimeId ?? "",
              model: member.model ?? "",
              reasoning: member.reasoning ?? "",
              permissionLevel: permissionLevel(member.permissionLevel),
            },
          ]
        : [],
    ),
  };
}

export function TeamFormDialog({
  detail,
  onOpenChange,
  open,
}: TeamFormDialogProps) {
  const { t } = useTranslation("team");
  const queryClient = useQueryClient();
  const channel = detail?.channel;
  const editing = Boolean(channel?.id);
  const form = useForm<TeamFormValues>({
    defaultValues: initialValues(detail),
    mode: "onChange",
    resolver: zodResolver(teamFormSchema),
  });
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const [selectedWakers, setSelectedWakers] = useState<Map<string, Waker>>(
    () =>
      new Map(
        (detail?.members ?? []).flatMap((member) =>
          member.waker?.id ? ([[member.waker.id, member.waker]] as const) : [],
        ),
      ),
  );
  const members = useWatch({ control: form.control, name: "members" });
  const leadWakerId = useWatch({
    control: form.control,
    name: "leadWakerId",
  });
  const name = useWatch({ control: form.control, name: "name" });
  const description = useWatch({
    control: form.control,
    name: "description",
  });
  const wakersQuery = useInfiniteQuery(
    teamWakersInfiniteOptions(deferredSearch),
  );
  const defaultExecution = useExecutionSelection({ value: {} });
  const runtimesQuery = defaultExecution.runtimesQuery;
  const create = useMutation(withLocalErrorMutation(createChannelMutation()));
  const update = useMutation(withLocalErrorMutation(updateChannelMutation()));
  const mutation = editing ? update : create;
  const wakers = useMemo(
    () => wakersQuery.data?.pages.flatMap((page) => page.resources ?? []) ?? [],
    [wakersQuery.data?.pages],
  );
  const defaultRuntime = defaultExecution.runtime;
  const wakerById = new Map([
    ...selectedWakers,
    ...wakers.flatMap((waker) =>
      waker.id ? ([[waker.id, waker]] as const) : [],
    ),
  ]);

  const setMembers = (nextMembers: TeamMemberValues[]) => {
    form.setValue("members", nextMembers, {
      shouldDirty: true,
      shouldValidate: true,
    });
  };
  const setLeadWakerId = (wakerId: string) => {
    form.setValue("leadWakerId", wakerId, {
      shouldDirty: true,
      shouldValidate: true,
    });
  };
  const toggleMember = (wakerId: string) => {
    if (members.some((member) => member.wakerId === wakerId)) {
      setMembers(members.filter((member) => member.wakerId !== wakerId));
      if (leadWakerId === wakerId) setLeadWakerId("");
      setSelectedWakers((selected) => {
        const nextSelected = new Map(selected);
        nextSelected.delete(wakerId);
        return nextSelected;
      });
      return;
    }
    if (members.length >= 5 || !defaultRuntime) return;
    const waker = wakerById.get(wakerId);
    if (waker) {
      setSelectedWakers((selected) => new Map(selected).set(wakerId, waker));
    }
    setMembers([...members, { wakerId, ...defaultExecution.selection }]);
  };
  const updateMember = (wakerId: string, patch: Partial<TeamMemberValues>) => {
    setMembers(
      members.map((member) =>
        member.wakerId === wakerId ? { ...member, ...patch } : member,
      ),
    );
  };
  const close = () => {
    mutation.reset();
    onOpenChange(false);
  };
  const canSubmit = teamFormSchema.safeParse({
    name,
    description,
    leadWakerId,
    members,
  }).success;
  const submit = form.handleSubmit(async (values) => {
    const body: ChannelInput = {
      name: values.name.trim(),
      description: values.description.trim(),
      leadWakerId: values.leadWakerId,
      members: values.members,
    };
    try {
      if (editing && channel?.id) {
        await update.mutateAsync({ path: { channelId: channel.id }, body });
        await queryClient.invalidateQueries({
          queryKey: teamQueryKeys.channels(),
        });
        await queryClient.invalidateQueries({
          queryKey: getChannelQueryKey({ path: { channelId: channel.id } }),
        });
        close();
        return;
      }
      await create.mutateAsync({ body });
      await queryClient.invalidateQueries({
        queryKey: teamQueryKeys.channels(),
      });
      close();
    } catch {
      // React Query 保存错误，表单底部统一展示。
    }
  });

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogContent
        title={editing ? t("edit") : t("create")}
        description={t("form.description")}
        size="xl"
        layout="form"
      >
        <form
          className="flex min-h-0 flex-1 flex-col overflow-hidden"
          onSubmit={submit}
        >
          <div className="flex min-h-0 flex-1 flex-col px-6 pb-5">
            <div className="grid gap-4 md:grid-cols-2">
              <label className="type-label grid gap-2">
                {t("form.name")}
                <Input
                  {...form.register("name")}
                  placeholder={t("form.namePlaceholder")}
                  autoFocus
                />
                {form.formState.errors.name ? (
                  <span role="alert" className="text-destructive type-caption">
                    {t("form.nameRequired")}
                  </span>
                ) : null}
              </label>
              <label className="type-label grid gap-2">
                {t("form.teamDescription")}
                <Textarea
                  {...form.register("description")}
                  size="sm"
                  placeholder={t("form.descriptionPlaceholder")}
                  maxLength={1000}
                />
              </label>
            </div>

            <div className="mt-5 grid min-h-0 flex-1 gap-4 md:grid-cols-2">
              <section className="border-border bg-surface flex min-h-0 flex-col rounded-lg border">
                <div className="border-border border-b p-3">
                  <div className="type-label mb-2 flex justify-between">
                    <span>{t("form.selectMembers")}</span>
                    <span className="text-muted-foreground">
                      {members.length}/5
                    </span>
                  </div>
                  <SearchField
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={t("form.searchMembers")}
                    aria-label={t("form.searchMembers")}
                  />
                </div>
                <div className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto p-2">
                  {wakersQuery.isPending ? (
                    <p
                      role="status"
                      aria-busy="true"
                      className="text-muted-foreground type-caption py-8 text-center"
                    >
                      {t("form.loadingMembers")}
                    </p>
                  ) : wakersQuery.isError ? (
                    <div role="alert" className="py-6 text-center">
                      <p className="text-destructive type-caption">
                        {t("form.membersLoadError")}
                      </p>
                      <Button
                        type="button"
                        size="sm"
                        variant="secondary"
                        className="mt-3"
                        onClick={() => void wakersQuery.refetch()}
                      >
                        {t("retryLoad")}
                      </Button>
                    </div>
                  ) : (
                    wakers.map((waker) => {
                      const id = waker.id ?? "";
                      const checked = members.some(
                        (member) => member.wakerId === id,
                      );
                      return (
                        <label
                          key={id}
                          className="hover:bg-muted flex cursor-pointer items-center gap-3 rounded-sm px-2 py-2"
                        >
                          <Checkbox
                            checked={checked}
                            disabled={
                              !checked &&
                              (members.length >= 5 || !defaultRuntime)
                            }
                            onCheckedChange={() => toggleMember(id)}
                          />
                          <Avatar
                            interactive={false}
                            name={waker.name ?? t("member")}
                            src={waker.avatar}
                            size="sm"
                          />
                          <span className="min-w-0 flex-1">
                            <span className="type-body block truncate">
                              {waker.name}
                            </span>
                            <span className="text-muted-foreground type-caption block truncate">
                              {waker.roleName || t("member")}
                            </span>
                          </span>
                          {checked ? (
                            <Check className="text-primary size-icon-sm" />
                          ) : null}
                        </label>
                      );
                    })
                  )}
                  {wakersQuery.hasNextPage ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="mt-2 w-full"
                      disabled={wakersQuery.isFetchingNextPage}
                      onClick={() => void wakersQuery.fetchNextPage()}
                    >
                      {wakersQuery.isFetchingNextPage
                        ? t("form.loadingMembers")
                        : t("form.loadMoreMembers")}
                    </Button>
                  ) : null}
                </div>
              </section>

              <section className="border-border bg-surface flex min-h-0 flex-col rounded-lg border">
                <div className="border-border border-b p-3">
                  <div className="type-label">{t("form.selectedMembers")}</div>
                  <p className="text-muted-foreground type-caption mt-1">
                    {t("form.leadHelp")}
                  </p>
                  {runtimesQuery.isPending ? (
                    <p
                      role="status"
                      aria-busy="true"
                      className="text-muted-foreground type-caption mt-2"
                    >
                      {t("form.loadingRuntimes")}
                    </p>
                  ) : runtimesQuery.isError ? (
                    <div role="alert" className="mt-2 flex items-center gap-2">
                      <span className="text-destructive type-caption">
                        {t("form.runtimesLoadError")}
                      </span>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => void runtimesQuery.refetch()}
                      >
                        {t("retryLoad")}
                      </Button>
                    </div>
                  ) : null}
                </div>
                <div className="scrollbar-hidden min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
                  {members.map((member) => {
                    const waker = wakerById.get(member.wakerId);
                    const memberName = waker?.name ?? t("member");
                    const isLead = leadWakerId === member.wakerId;
                    return (
                      <div
                        key={member.wakerId}
                        className="bg-muted rounded-md p-3"
                      >
                        <div className="flex items-center gap-2">
                          <Avatar
                            name={memberName}
                            src={waker?.avatar}
                            size="sm"
                          />
                          <span className="type-body min-w-0 flex-1 truncate font-medium">
                            {memberName}
                          </span>
                          <Button
                            type="button"
                            size="sm"
                            variant={isLead ? "primary" : "secondary"}
                            onClick={() => setLeadWakerId(member.wakerId)}
                          >
                            <Crown />
                            {isLead ? t("form.lead") : t("form.setLead")}
                          </Button>
                          <Button
                            type="button"
                            size="icon-sm"
                            variant="ghost"
                            className="hover:text-destructive"
                            aria-label={t("form.removeMember", {
                              name: memberName,
                            })}
                            onClick={() => toggleMember(member.wakerId)}
                          >
                            <Trash2 />
                          </Button>
                        </div>
                        <div className="mt-4">
                          <MemberConfiguration
                            value={member}
                            name={memberName}
                            onChange={(next) =>
                              updateMember(member.wakerId, next)
                            }
                          />
                        </div>
                      </div>
                    );
                  })}
                  {!members.length ? (
                    <p className="text-muted-foreground type-caption py-8 text-center">
                      {t("form.selectEmpty")}
                    </p>
                  ) : null}
                </div>
              </section>
            </div>
          </div>
          <DialogFooter>
            {members.length > 0 && members.length < 2 ? (
              <p role="alert" className="text-destructive type-caption mr-auto">
                {t("form.minMembers")}
              </p>
            ) : members.length >= 2 && !leadWakerId ? (
              <p role="alert" className="text-destructive type-caption mr-auto">
                {t("form.leadRequired")}
              </p>
            ) : null}
            {mutation.isError ? (
              <p
                role="alert"
                className="text-destructive type-caption mr-auto flex items-center gap-2"
              >
                <CircleAlert className="size-icon-sm" />
                {localApiErrorMessage(
                  mutation.error,
                  editing ? t("form.saveFailed") : t("form.createFailed"),
                )}
              </p>
            ) : null}
            <Button type="button" variant="secondary" onClick={close}>
              {t("common:cancel")}
            </Button>
            <Button type="submit" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending
                ? t("form.processing")
                : editing
                  ? t("form.save")
                  : t("create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MemberConfiguration({
  value,
  name,
  onChange,
}: {
  value: TeamMemberValues;
  name: string;
  onChange: (value: Partial<TeamMemberValues>) => void;
}) {
  const state = useExecutionSelection({ value });
  return (
    <ExecutionConfigurationFields
      state={state}
      name={name}
      onChange={onChange}
    />
  );
}
