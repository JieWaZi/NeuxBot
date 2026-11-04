import {
  ExecutionRuntimeControl,
  ExecutionPermissionControl,
} from "@/shared/ui/execution-selection-controls";
import { usesNativeModelCatalog } from "@/shared/api/runtime-queries";
import { listAiAccountsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Folder } from "lucide-react";
import { useTranslation } from "react-i18next";

import type {
  GithubComJieWaZiNeuxBotServerApisProjectsProjectResource as ProjectResource,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeModelOption as RuntimeModelOption,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as RuntimeReasoningOption,
} from "@/shared/api/generated/types.gen";
import {
  hasPlatformService,
  listWorkspaceGitRepositories,
} from "@/shared/platform";
import {
  modelPermissionLevels,
  runtimeDefaultPermissionLevels,
  safeDefaultPermissionLevel,
  type PermissionLevel,
} from "@/shared/lib/runtime-permissions";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { WorkspaceScopePicker } from "@/shared/ui/workspace-scope-picker";
import { OverflowText } from "@/shared/ui/overflow-text";
import { ChatComposerBranchSelector } from "./chat-composer-branch-selector";
import { ExecutionModelControl } from "@/shared/ui/execution-model-control";
import { ChatEffortEffects } from "./chat-effort-effects";
import { isMaximumReasoning } from "@/shared/lib/runtime-reasoning";
import {
  ChatComposerWorkspaceStrip,
  composerContextControlClassName,
} from "./chat-composer-workspace-strip";

export type ChatExecutionSelectionPatch = Partial<{
  accountId: string;
  model: string;
  permissionLevel: PermissionLevel;
  reasoning: string;
  fast: boolean | null;
  runtimeId: string;
}>;

type ChatComposerControlsProps = {
  workspaceReadOnly?: boolean;
  selectedAccountId?: string;
  activeChatId?: string;
  availableReasoningOptions?: RuntimeReasoningOption[];
  modelOptionsOverride?: RuntimeModelOption[];
  catalogState?: string;
  catalogPending?: boolean;
  refreshCatalog?: () => unknown;
  fastSupported?: boolean;
  selectedFast?: boolean;
  thinkingPending?: boolean;
  historicalRuntime?: RuntimeOption;
  historicalRuntimeDisabled: boolean;
  onPickDirectory: () => void | Promise<void>;
  onSelectProject: (projectId: string) => void;
  projects: ProjectResource[];
  runtimeId: string;
  runtimes: RuntimeOption[];
  selectedDraftProject?: ProjectResource;
  selectedModel: string;
  selectedModelName: string;
  selectedPermissionLevel: PermissionLevel;
  selectedReasoning: string;
  selectedRuntime?: RuntimeOption;
  selectedWakerId?: string;
  supportedPermissions: ReadonlySet<PermissionLevel>;
  updateExecutionSelection: (patch: ChatExecutionSelectionPatch) => void;
  /** Git 分支查询目录；已有对话时优先用源目录而非受管工作区。 */
  gitWorkspaceDirectories?: readonly string[];
  workspaceDirectory: string;
  workspaceLabel: string;
};

const stripControlClassName = composerContextControlClassName;

/** 普通与 Team composer 共用工作区、权限、Runtime、模型和推理控件。 */
export function useChatComposerControls({
  workspaceReadOnly = false,
  selectedAccountId,
  activeChatId,
  availableReasoningOptions,
  modelOptionsOverride,
  catalogState,
  catalogPending,
  refreshCatalog,
  fastSupported,
  selectedFast,
  thinkingPending,
  historicalRuntime,
  historicalRuntimeDisabled,
  onPickDirectory,
  onSelectProject,
  projects,
  runtimeId,
  runtimes,
  selectedDraftProject,
  selectedModel,
  selectedModelName,
  selectedPermissionLevel,
  selectedReasoning,
  selectedRuntime,
  selectedWakerId,
  supportedPermissions,
  updateExecutionSelection,
  gitWorkspaceDirectories,
  workspaceDirectory,
  workspaceLabel,
}: ChatComposerControlsProps) {
  const { t } = useTranslation(["chat", "common"]);
  const accountsQuery = useQuery({
    ...listAiAccountsOptions(),
    enabled: selectedAccountId !== undefined,
  });
  const accounts = accountsQuery.data?.resources ?? [];
  const selectedAccount = accounts.find(
    (account) => account.id === selectedAccountId,
  );
  const selectAccountForRuntime = (
    runtime: RuntimeOption,
    accountId: string,
  ) => {
    const account = accounts.find((item) => item.id === accountId);
    const usesNativeCatalog = usesNativeModelCatalog(
      runtime.baseRuntimeId || runtime.id,
    );
    updateExecutionSelection({
      accountId,
      fast: null,
      model:
        usesNativeCatalog || account?.authMode === "subscription"
          ? ""
          : account?.defaultModel ||
            account?.selectedModels?.[0] ||
            runtime.defaultModel ||
            "",
      permissionLevel: usesNativeCatalog
        ? safeDefaultPermissionLevel(
            selectedPermissionLevel,
            runtimeDefaultPermissionLevels(runtime),
          )
        : selectedPermissionLevel,
      reasoning:
        usesNativeCatalog || account ? "" : (runtime.defaultReasoning ?? ""),
      runtimeId: runtime.id ?? "",
    });
  };
  const modelOptions =
    modelOptionsOverride ??
    (selectedAccountId &&
    selectedAccountId !== "system" &&
    selectedAccount?.authMode !== "subscription"
      ? (selectedAccount?.selectedModels ?? []).map((id) => ({
          id,
          name: id,
          reasoningOptions: [],
        }))
      : (selectedRuntime?.models ?? []));
  const gitDirectories = (
    gitWorkspaceDirectories !== undefined
      ? gitWorkspaceDirectories
      : workspaceDirectory
        ? [workspaceDirectory]
        : []
  ).filter(Boolean);
  const gitRepositoriesQuery = useQuery({
    queryKey: ["workspace-git-repositories", gitDirectories],
    queryFn: () => listWorkspaceGitRepositories(gitDirectories),
    enabled: gitDirectories.length > 0 && hasPlatformService("git"),
    staleTime: 3000,
  });
  const gitRepositories = gitRepositoriesQuery.data?.repositories ?? [];
  const showRepoLabels = gitRepositories.length > 1;

  const workspaceLocked = workspaceReadOnly || Boolean(activeChatId);
  const workspaceControl = workspaceLocked ? (
    <span
      data-testid="composer-workspace-control"
      data-overflow-trigger
      aria-label={workspaceLabel}
      title={t("chat:workspaceLockedHint")}
      className={cn(
        stripControlClassName,
        "hover:text-muted-foreground max-w-45 cursor-default font-medium opacity-55 hover:bg-transparent",
      )}
    >
      <Folder aria-hidden className="size-icon-xs shrink-0" />
      <OverflowText className="min-w-0">{workspaceLabel}</OverflowText>
    </span>
  ) : (
    <WorkspaceScopePicker
      projects={projects}
      projectId={selectedDraftProject?.id}
      ownerId={selectedWakerId}
      onPickDirectory={onPickDirectory}
      onSelectProject={onSelectProject}
      trigger={
        <Button
          type="button"
          data-testid="composer-workspace-control"
          data-overflow-trigger
          title={workspaceDirectory || t("chat:selectWorkspace")}
          variant="ghost"
          size="sm"
          className={cn(stripControlClassName, "max-w-45 font-medium")}
        >
          <Folder aria-hidden className="size-icon-xs shrink-0" />
          <OverflowText className="min-w-0">{workspaceLabel}</OverflowText>
          <ChevronDown className="text-muted-foreground" aria-hidden />
        </Button>
      }
    />
  );

  const permissionControl = (
    <ExecutionPermissionControl
      selectedPermissionLevel={selectedPermissionLevel}
      supportedPermissions={supportedPermissions}
      triggerClassName={stripControlClassName}
      onChange={(permissionLevel) =>
        updateExecutionSelection({ permissionLevel })
      }
    />
  );
  const runtimeControl = (
    <>
      <ExecutionRuntimeControl
        runtimes={runtimes}
        accounts={accounts}
        selectedRuntime={selectedRuntime}
        historicalRuntime={historicalRuntime}
        selectedAccountId={selectedAccountId}
        onSelect={selectAccountForRuntime}
      />
      {historicalRuntimeDisabled && !runtimeId ? (
        <span className="text-destructive type-caption">
          {t("chat:runtimeDisabledSwitch")}
        </span>
      ) : null}
    </>
  );
  const usesNativeCatalog = usesNativeModelCatalog(
    selectedRuntime?.baseRuntimeId || selectedRuntime?.id,
  );
  const modelControl =
    usesNativeCatalog && catalogState && catalogState !== "fresh" ? (
      <Button
        size="sm"
        variant="ghost"
        disabled={catalogPending}
        onClick={() => void refreshCatalog?.()}
      >
        {t(
          catalogState === "stale"
            ? "common:executionSelection.catalogStale"
            : catalogState === "signed_out"
              ? "common:executionSelection.catalogSignedOut"
              : "common:executionSelection.catalogUnavailable",
        )}
      </Button>
    ) : selectedModelName || selectedAccount || usesNativeCatalog ? (
      <ExecutionModelControl
        selectedModelName={selectedModelName}
        selectedModel={selectedModel}
        models={modelOptions}
        selectedReasoning={selectedReasoning}
        reasoningOptions={availableReasoningOptions}
        thinkingPending={thinkingPending || catalogPending}
        disabled={usesNativeCatalog && catalogState !== "fresh"}
        fast={selectedFast}
        onFastChange={
          fastSupported
            ? (fast) => updateExecutionSelection({ fast: fast ?? null })
            : undefined
        }
        allowDefaultReasoning={Boolean(selectedAccount)}
        defaultReasoning={
          selectedAccount?.defaultReasoning || selectedRuntime?.defaultReasoning
        }
        onModelChange={(model) => {
          const nextModelOption = modelOptions.find(
            (item) => item.id === model,
          );
          const nextReasoningOptions =
            usesNativeCatalog || selectedAccount
              ? nextModelOption?.reasoningOptions
              : nextModelOption?.reasoningOptions?.length
                ? nextModelOption.reasoningOptions
                : selectedRuntime?.reasoningOptions;
          const reasoning =
            usesNativeCatalog || selectedAccount
              ? ""
              : (nextReasoningOptions?.find(
                  (item) => item.id === selectedRuntime?.defaultReasoning,
                )?.id ??
                nextReasoningOptions?.[0]?.id ??
                "");
          updateExecutionSelection({
            model,
            permissionLevel: usesNativeCatalog
              ? safeDefaultPermissionLevel(
                  selectedPermissionLevel,
                  modelPermissionLevels(modelOptions, model),
                )
              : selectedPermissionLevel,
            reasoning,
            fast: null,
          });
        }}
        onReasoningChange={(reasoning) =>
          updateExecutionSelection({ reasoning })
        }
      />
    ) : null;

  const branchControl =
    gitRepositories.length > 0 ? (
      <>
        {gitRepositories.map((repository) =>
          repository.branch ? (
            <ChatComposerBranchSelector
              key={repository.gitRoot}
              className={cn(stripControlClassName, "max-w-36 px-1.5")}
              currentBranch={repository.branch}
              gitRoot={repository.directory}
              repoLabel={repository.label}
              showRepoLabel={showRepoLabels}
            />
          ) : null,
        )}
      </>
    ) : null;

  const renderContextStrip = () => (
    <ChatComposerWorkspaceStrip
      branchControl={branchControl}
      permissionControl={permissionControl}
      workspaceControl={workspaceControl}
      workspaceDirectory={workspaceDirectory}
      workspaceLabel={workspaceLabel}
    />
  );

  const composerSettings = (
    <>
      {runtimeControl}
      {modelControl}
    </>
  );

  return {
    composerEffects: (
      <ChatEffortEffects
        active={
          !thinkingPending &&
          isMaximumReasoning(
            availableReasoningOptions,
            selectedReasoning,
            selectedAccount?.defaultReasoning ||
              selectedRuntime?.defaultReasoning,
          )
        }
      />
    ),
    composerSettings,
    renderContextStrip,
  };
}
