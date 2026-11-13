import { ExecutionPermissionControl } from "@/shared/ui/execution-selection-controls";
import type { ExecutionSelectionState } from "@/shared/hooks/use-execution-selection";
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
import type { PermissionLevel } from "@/shared/lib/runtime-permissions";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { WorkspaceScopePicker } from "@/shared/ui/workspace-scope-picker";
import { OverflowText } from "@/shared/ui/overflow-text";
import { ChatComposerBranchSelector } from "./chat-composer-branch-selector";
import { ExecutionPicker } from "@/shared/ui/execution-picker";
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
  contextWindow: string;
  runtimeId: string;
}>;

type ChatComposerControlsProps = {
  workspaceReadOnly?: boolean;
  selectedAccountId?: string;
  activeChatId?: string;
  availableReasoningOptions?: RuntimeReasoningOption[];
  modelOptionsOverride?: RuntimeModelOption[];
  defaultModel?: string;
  catalogState?: string;
  catalogIssue?: ExecutionSelectionState["catalogIssue"];
  catalogPending?: boolean;
  catalogRefreshing?: boolean;
  refreshCatalog?: () => unknown;
  fastSupported?: boolean;
  selectedFast?: boolean;
  selectedContextWindow?: string;
  enableContextWindow?: boolean;
  sessionSwitchPending?: boolean;
  thinkingPending?: boolean;
  historicalRuntime?: RuntimeOption;
  historicalRuntimeDisabled: boolean;
  wakerDefaultUnavailable?: boolean;
  onPickDirectory: () => void | Promise<void>;
  onSelectManaged: () => void;
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
  defaultModel,
  catalogState,
  catalogIssue,
  catalogPending,
  catalogRefreshing,
  refreshCatalog,
  fastSupported,
  selectedFast,
  selectedContextWindow,
  enableContextWindow,
  sessionSwitchPending,
  thinkingPending,
  historicalRuntime,
  historicalRuntimeDisabled,
  wakerDefaultUnavailable,
  onPickDirectory,
  onSelectManaged,
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
  const { t } = useTranslation(["chat", "common", "waker"]);
  const accountsQuery = useQuery({
    ...listAiAccountsOptions(),
    enabled: selectedAccountId !== undefined,
  });
  const accounts = accountsQuery.data?.resources ?? [];
  const selectedAccount = accounts.find(
    (account) =>
      account.id === selectedAccountId &&
      account.runtimeId === selectedRuntime?.id,
  );
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
      onSelectManaged={onSelectManaged}
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
  const composerSettings = (
    <>
      <ExecutionPicker
        runtimes={runtimes}
        accounts={accounts}
        runtime={selectedRuntime}
        historicalRuntime={historicalRuntime}
        accountId={selectedAccountId}
        models={modelOptions}
        modelId={selectedModel}
        modelName={selectedModelName}
        defaultModel={defaultModel}
        reasoning={selectedReasoning}
        reasoningOptions={availableReasoningOptions}
        allowDefaultReasoning={Boolean(
          selectedAccount ||
          usesNativeModelCatalog(
            selectedRuntime?.baseRuntimeId || selectedRuntime?.id,
          ),
        )}
        catalogState={catalogState}
        catalogIssue={catalogIssue}
        pending={thinkingPending || catalogPending}
        refreshing={catalogRefreshing}
        sessionSwitchPending={sessionSwitchPending}
        onRefresh={refreshCatalog}
        fast={selectedFast}
        fastSupported={fastSupported}
        onFast={(fast) => updateExecutionSelection({ fast })}
        contextWindow={selectedContextWindow}
        onContextWindow={
          enableContextWindow
            ? (contextWindow) => updateExecutionSelection({ contextWindow })
            : undefined
        }
        onReasoning={(reasoning) => updateExecutionSelection({ reasoning })}
        onModel={(runtime, accountId, model) => {
          const usesNativeCatalog = usesNativeModelCatalog(
            runtime.baseRuntimeId || runtime.id,
          );
          const options = model.reasoningOptions?.length
            ? model.reasoningOptions
            : runtime.reasoningOptions;
          const reasoning =
            usesNativeCatalog || accountId !== "system"
              ? ""
              : (options?.find((item) => item.id === runtime.defaultReasoning)
                  ?.id ??
                options?.[0]?.id ??
                "");
          updateExecutionSelection({
            runtimeId: runtime.id ?? "",
            accountId,
            model: model.id ?? "",
            permissionLevel: selectedPermissionLevel,
            reasoning,
            fast: null,
            ...(enableContextWindow ? { contextWindow: "" } : {}),
          });
        }}
      />
      {wakerDefaultUnavailable ? (
        <span role="alert" className="text-destructive type-caption">
          {t("waker:form.modelUnavailable")}
        </span>
      ) : null}
      {historicalRuntimeDisabled && !runtimeId ? (
        <span className="text-destructive type-caption">
          {t("chat:runtimeDisabledSwitch")}
        </span>
      ) : null}
    </>
  );

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
              showRepositoryPath={gitRepositories.length > 1}
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
