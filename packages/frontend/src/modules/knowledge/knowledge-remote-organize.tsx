import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { hasKnowledgeCapability } from "@/shared/api/knowledge/knowledge-adapter-types";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { Choice, Field } from "./knowledge-controls";
import { knowledgeKeys, organizableSourcesOptions } from "./knowledge-queries";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { createKnowledgeOrganizeIntent } from "./knowledge-organize-start";
import type { KnowledgeOrganizeDialogProps } from "./knowledge-organize-dialog";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import { useCopy } from "./use-extended-copy";
import {
  readOrganizePreferences,
  saveOrganizePreferences,
} from "./knowledge-organize-preferences";

/** 远端任务冻结服务器配置与来源；不读取或传送本机 Runtime、账号和凭据。 */
export function KnowledgeRemoteOrganize(props: KnowledgeOrganizeDialogProps) {
  const t = useCopy();
  const adapter = knowledgeAdapter(props.library.ref);
  const [localIntent] = useState(createKnowledgeOrganizeIntent);
  const intent = props.intent ?? localIntent;
  const retained = intent.get();
  const [preferences] = useState(() =>
    readOrganizePreferences(props.library.ref),
  );
  const [profile, setProfile] = useState(
    retained.remoteCommand?.profileId ?? preferences.profileId ?? "",
  );
  const [requirements, setRequirements] = useState(
    retained.remoteCommand?.instruction ?? preferences.requirements ?? "",
  );
  const [batch, setBatch] = useState<string[] | undefined>(
    retained.remoteCommand?.sourceVersionIds ??
      (props.rebuild ? props.batch : undefined),
  );
  const capabilities = useQuery({
    queryKey: knowledgeKeys.capabilities(props.library.ref),
    queryFn: ({ signal }) => adapter.getCapabilities(signal),
    staleTime: 0,
  });
  const sources = useQuery({
    ...organizableSourcesOptions(props.library.ref),
    enabled: !props.rebuild,
  });
  const profiles = capabilities.data?.profiles ?? [];
  const needsProfile = !capabilities.data?.uploadMode;
  const independent =
    capabilities.data?.uploadMode !== "automatic" &&
    capabilities.data?.uploadMode !== "unknown";
  const allowed = hasKnowledgeCapability(
    capabilities.data,
    props.rebuild ? "rebuild" : "organize",
  );
  const selectedProfile = profile || profiles[0]?.id || "";
  const available = props.rebuild
    ? props.library.sources
    : (sources.data ?? []);
  const selected = batch ?? available.map((source) => source.id);
  const start = useMutation({
    mutationFn: async () => {
      const command = intent.get().remoteCommand ?? {
        ...(needsProfile ? { profileId: selectedProfile } : {}),
        sourceVersionIds: [...selected],
        instruction: requirements.trim(),
        expectedVersion: props.library.revision,
        structureVersion: props.library.structureVersion ?? 0,
        rebuild: Boolean(props.rebuild),
      };
      intent.update({ remoteCommand: command });
      try {
        await props.operation.perform(
          `remote:${intent.id}`,
          async (requestId) => {
            const result = await adapter.organizeRemote({
              path: {
                connectionId: props.library.ref.connectionId,
                libraryId: props.library.id,
              },
              body: { ...command, requestId },
              throwOnError: true,
            });
            const receipt = result.data;
            if (!receipt.jobId && receipt.state !== "no-op")
              throw new ApiError(
                t("organizeTaskNotAccepted"),
                "KnowledgeRemoteOutcomeUnknown",
                504,
              );
          },
        );
      } catch (cause) {
        if (!isKnowledgeWriteOutcomeUnknown(cause))
          intent.update({ remoteCommand: undefined });
        throw cause;
      }
    },
    onSuccess: props.onAccepted,
    retry: false,
  });
  const frozen = Boolean(intent.get().remoteCommand);
  const error = capabilities.error || sources.error;
  return (
    <>
      <div className="grid gap-4 px-6 pb-5">
        {error ? null : capabilities.isPending ||
          (!props.rebuild && sources.isPending) ? (
          <KnowledgeWikiContentLoading />
        ) : (
          <>
            {needsProfile && (
              <Field label={t("remoteProfile")} hint={t("remoteProfileHelp")}>
                {profiles.length ? (
                  <Choice
                    label={t("remoteProfile")}
                    value={selectedProfile}
                    options={profiles.flatMap((entry) =>
                      entry.id
                        ? [{ value: entry.id, label: entry.name || entry.id }]
                        : [],
                    )}
                    onChange={(profileId) => {
                      setProfile(profileId);
                      saveOrganizePreferences(props.library.ref, { profileId });
                    }}
                    disabled={frozen || start.isPending}
                  />
                ) : (
                  <p role="status" className="type-body text-content-secondary">
                    {t("remoteProfileEmpty")}
                  </p>
                )}
              </Field>
            )}
            {!independent && (
              <p role="status" className="type-body text-content-secondary">
                {t(
                  capabilities.data?.uploadMode === "automatic"
                    ? "remoteAutomaticOrganize"
                    : "remoteDetectionUnknown",
                )}
              </p>
            )}
            {!props.rebuild && (
              <Field label={t("organizeFiles")} hint={t("organizeFilesHelp")}>
                <SearchableMultiSelect
                  size="md"
                  options={available.map((source) => ({
                    value: source.id,
                    label: source.name,
                  }))}
                  value={selected}
                  onValueChange={setBatch}
                  triggerLabel={t("organizeSelectedFiles", {
                    count: selected.length,
                  })}
                  searchLabel={t("organizeSearchFiles")}
                  emptyLabel={t("organizeNoFiles")}
                  clearSearchLabel={t("organizeClearSearch")}
                  clearSelectionLabel={t("organizeClearSelection")}
                  disabled={frozen || start.isPending}
                />
              </Field>
            )}
            <Field
              label={t("organizeRequirements")}
              hint={t("organizeRequirementsHelp")}
            >
              <Textarea
                aria-label={t("organizeRequirements")}
                placeholder={t("organizeRequirementsPlaceholder")}
                value={requirements}
                onChange={(event) => {
                  setRequirements(event.target.value);
                  saveOrganizePreferences(props.library.ref, {
                    requirements: event.target.value,
                  });
                }}
                disabled={frozen || start.isPending}
                maxLength={4000}
              />
            </Field>
          </>
        )}
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={props.onClose}>
          {t("cancel")}
        </Button>
        <Button
          disabled={
            start.isPending ||
            props.operation.pending ||
            (!frozen &&
              (Boolean(error) ||
                !independent ||
                !allowed ||
                (needsProfile && !selectedProfile) ||
                !selected.length))
          }
          onClick={() => start.mutate()}
        >
          {t("start")}
        </Button>
      </DialogFooter>
    </>
  );
}
