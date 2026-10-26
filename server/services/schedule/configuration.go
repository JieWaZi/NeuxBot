package schedule

import (
	"context"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/schedulecalc"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// SavedTask 是创建或切换 API 时只返回一次的密钥收据。
type SavedTask struct {
	// TaskID 是已保存的任务标识。
	TaskID string `json:"taskId"`
	// Token 是新签发的明文密钥，其他读接口永不返回。
	Token string `json:"token,omitempty"`
}

// SaveConfiguration 复用新建和编辑的完整校验、触发代际与计划重算。
func (s *Service) SaveConfiguration(ctx context.Context, wakerID, id string, revision int64, config models.AutomaticTaskConfiguration, validateDependencies bool) (SavedTask, error) {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	return s.saveConfiguration(ctx, wakerID, id, revision, config, validateDependencies)
}

func (s *Service) saveConfiguration(ctx context.Context, wakerID, id string, revision int64, config models.AutomaticTaskConfiguration, validate bool) (SavedTask, error) {
	scopeWakerID := wakerID
	config.ChannelID = strings.TrimSpace(config.ChannelID)
	config.ExecutorWakerID = strings.TrimSpace(config.ExecutorWakerID)
	config.WorkflowID = strings.TrimSpace(config.WorkflowID)
	config.WorkflowRevisionID = strings.TrimSpace(config.WorkflowRevisionID)
	kind := constants.AutomaticTaskTargetKindWaker
	if config.ChannelID != "" {
		kind = constants.AutomaticTaskTargetKindTeam
	}
	if config.WorkflowID != "" {
		if config.ChannelID != "" || config.ExecutorWakerID != "" {
			return SavedTask{}, status_error.BadRequest
		}
		kind = constants.AutomaticTaskTargetKindWorkflow
	}
	if config.TargetKind != "" && config.TargetKind != kind {
		return SavedTask{}, status_error.BadRequest
	}
	if config.Knowledge != nil && kind != constants.AutomaticTaskTargetKindWaker {
		return SavedTask{}, status_error.KnowledgeUnsupported
	}
	if auth := config.Knowledge; auth != nil && (auth.Backend != "local" || auth.ConnectionID != "local" || auth.KnowledgeBaseID == "" || (auth.Operation != "organize" && auth.Operation != constants.KnowledgeWikiMaintainOperation) || auth.Operation == constants.KnowledgeWikiMaintainOperation && (len(auth.SourceVersionIDs) != 0 || len(auth.AttachmentIndexes) != 0)) {
		return SavedTask{}, status_error.KnowledgeInvalid
	}
	config.TargetKind = kind
	if kind != constants.AutomaticTaskTargetKindWorkflow && (config.WorkflowRevisionPolicy != "" || config.WorkflowRevisionID != "" || len(config.WorkflowInputBindings) != 0 || config.WorkflowInputRevisionID != "") {
		return SavedTask{}, status_error.BadRequest
	}
	if config.ChannelID == "" && len(config.MemberConfigs) > 0 {
		return SavedTask{}, status_error.BadRequest
	}
	config.MemberConfigs = append([]models.AutomaticTaskMemberConfiguration(nil), config.MemberConfigs...)
	for index := range config.MemberConfigs {
		member := &config.MemberConfigs[index]
		member.WakerID = strings.TrimSpace(member.WakerID)
		member.RuntimeID = strings.TrimSpace(member.RuntimeID)
		member.AccountID = strings.TrimSpace(member.AccountID)
		member.Model = strings.TrimSpace(member.Model)
		member.Reasoning = strings.TrimSpace(member.Reasoning)
		if member.AccountID == "" {
			member.AccountID = constants.AiAccountSystemID
		}
	}
	if config.ChannelID != "" && validate {
		if config.ExecutorWakerID != "" || s.config.Channels == nil || s.config.Teams == nil {
			return SavedTask{}, status_error.BadRequest
		}
		team, err := s.config.Channels.Get(ctx, config.ChannelID)
		if err != nil {
			return SavedTask{}, err
		}
		wakerID = team.LeadWakerID
	} else if config.ChannelID == "" && config.ExecutorWakerID != "" {
		wakerID = config.ExecutorWakerID
	}
	if kind == constants.AutomaticTaskTargetKindWorkflow && wakerID == "" {
		if s.config.Workflows == nil {
			return SavedTask{}, status_error.WorkflowCapabilityUnsupported
		}
		definition, _, err := s.config.Workflows.ValidateTargetConfiguration(ctx, config.WorkflowID, config.WorkflowRevisionID, workflowDefaults(config))
		if err != nil {
			return SavedTask{}, err
		}
		wakerID = definition.CreationWakerID
	}
	if wakerID == "" {
		return SavedTask{}, status_error.BadRequest
	}
	if s.sealedWakers[wakerID] {
		return SavedTask{}, status_error.AutomaticTaskAdmissionRevoked
	}
	config.Name = strings.TrimSpace(config.Name)
	config.Instruction = strings.TrimSpace(config.Instruction)
	config.RuntimeID = strings.TrimSpace(config.RuntimeID)
	config.AccountID = strings.TrimSpace(config.AccountID)
	if config.AccountID == "" {
		config.AccountID = "system"
	}
	config.Model = strings.TrimSpace(config.Model)
	config.Reasoning = strings.TrimSpace(config.Reasoning)
	config.ProjectID = strings.TrimSpace(config.ProjectID)
	config.WorkingDirectory = strings.TrimSpace(config.WorkingDirectory)
	config.Schedule.Timezone = strings.TrimSpace(config.Schedule.Timezone)
	config.Schedule.Cron = strings.TrimSpace(config.Schedule.Cron)
	if !utf8.ValidString(config.Name) || !utf8.ValidString(config.Instruction) || config.Name == "" || utf8.RuneCountInString(config.Name) > 120 || (kind != constants.AutomaticTaskTargetKindWorkflow && config.Instruction == "") || utf8.RuneCountInString(config.Instruction) > constants.AutomaticTaskInstructionMaximumRunes {
		return SavedTask{}, status_error.BadRequest
	}
	var task models.AutomaticTask
	var err error
	if id != "" {
		task, err = s.getTask(ctx, scopeWakerID, id)
		if err != nil {
			return SavedTask{}, err
		}
		if task.Deleting || task.WorkflowTriggerDetached || task.ConfigurationVersion != revision {
			return SavedTask{}, status_error.AutomaticTaskRevisionConflict
		}
	} else {
		task = models.AutomaticTask{ID: util.New(), WakerID: wakerID}
	}
	if config.WorkingDirectory != "" && (id == "" || config.WorkingDirectory != task.WorkingDirectory) {
		source, err := projectsource.Resolve(config.WorkingDirectory)
		if err != nil {
			return SavedTask{}, status_error.AutomaticTaskWorkspaceInvalid
		}
		config.WorkingDirectory = source.Path
	}
	if validate {
		if err := s.validateConfiguration(ctx, wakerID, &config); err != nil {
			return SavedTask{}, err
		}
	}
	now := s.config.Now()
	var next *int64
	if config.Trigger == constants.AutomaticTaskTriggerSchedule {
		rule, err := schedulecalc.Compile(config.Schedule)
		if err != nil {
			return SavedTask{}, err
		}
		future := rule.Next(now)
		if future.IsZero() && (id == "" || !reflect.DeepEqual(task.Schedule, config.Schedule)) {
			return SavedTask{}, status_error.AutomaticScheduleInvalid
		}
		if config.Enabled {
			next = timePointer(future)
		}
	} else if config.Trigger == constants.AutomaticTaskTriggerAPI {
		config.Schedule = models.AutomaticTaskSchedule{}
	} else {
		return SavedTask{}, status_error.BadRequest
	}
	var token string
	if id == "" || task.Trigger != config.Trigger {
		task.TriggerVersion++
		task.TokenHash = nil
		if config.Trigger == constants.AutomaticTaskTriggerAPI {
			token, task.TokenHash, err = util.NewAutomaticTaskToken()
			if err != nil {
				return SavedTask{}, err
			}
		}
	}
	storedRevision := task.Revision
	task.WakerID = wakerID
	task.AutomaticTaskConfiguration = config
	task.Revision++
	task.ConfigurationVersion++
	task.NextRunAt = next
	task.UpdatedAt = now.UnixMilli()
	if id == "" {
		task.CreatedAt = task.UpdatedAt
		err = s.config.Tasks.Create(ctx, task)
	} else {
		err = s.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return s.config.Tasks.UpdateConfiguration(ctx, task, storedRevision) })
	}
	if err != nil {
		return SavedTask{}, err
	}
	return SavedTask{TaskID: task.ID, Token: token}, nil
}

func (s *Service) validateConfiguration(ctx context.Context, wakerID string, config *models.AutomaticTaskConfiguration) error {
	if config.WorkflowID != "" {
		return s.validateWorkflowConfiguration(ctx, wakerID, config)
	}
	if config.ChannelID != "" {
		if s.config.Channels == nil {
			return status_error.AutomaticTaskServiceInvalid
		}
		team, err := s.config.Channels.Get(ctx, config.ChannelID)
		if err != nil {
			return err
		}
		// 团队任务按当前负责人校验工作范围，不把创建时的负责人当作固定执行者。
		wakerID = team.LeadWakerID
	}
	if _, err := s.config.Wakers.Get(ctx, wakerID); err != nil {
		return err
	}
	if config.ProjectID != "" && config.WorkingDirectory != "" {
		return status_error.AutomaticTaskWorkspaceInvalid
	}
	if config.ProjectID != "" {
		project, err := s.config.Projects.Get(ctx, config.ProjectID)
		if err != nil {
			return err
		}
		if project.WakerID != wakerID {
			return status_error.AutomaticTaskWorkspaceInvalid
		}
		directories, err := s.config.Projects.ListDirectories(ctx, project.ID)
		if err != nil {
			return err
		}
		if len(directories) == 0 {
			return status_error.AutomaticTaskWorkspaceInvalid
		}
		for _, directory := range directories {
			if !projectsource.Available(directory.Source) {
				return status_error.AutomaticTaskWorkspaceInvalid
			}
		}
	} else if config.WorkingDirectory != "" {
		source, err := projectsource.Resolve(config.WorkingDirectory)
		if err != nil {
			return status_error.AutomaticTaskWorkspaceInvalid
		}
		// 对现有配置不能悄悄把已更换的软链接重新绑定为另一目录。
		if source.Path != config.WorkingDirectory {
			return status_error.AutomaticTaskWorkspaceInvalid
		}
	}
	if config.ChannelID != "" {
		return s.validateTeamConfiguration(ctx, wakerID, *config)
	}
	return s.config.Executions.ValidateConfiguration(ctx, execution.ExecutionConfiguration{
		AccountID: config.AccountID, RuntimeID: config.RuntimeID, Model: config.Model, Reasoning: config.Reasoning, PermissionLevel: config.PermissionLevel,
	})
}

func timePointer(value time.Time) *int64 {
	if value.IsZero() {
		return nil
	}
	millis := value.UnixMilli()
	return &millis
}

func (s *Service) getTask(ctx context.Context, wakerID, id string) (models.AutomaticTask, error) {
	task, err := s.config.Tasks.Get(ctx, id)
	if err == nil && wakerID != "" && task.WakerID != wakerID {
		return models.AutomaticTask{}, status_error.AutomaticTaskNotFound
	}
	return task, err
}

func workflowDefaults(config models.AutomaticTaskConfiguration) models.WorkflowExecutionDefaults {
	return models.WorkflowExecutionDefaults{RuntimeID: config.RuntimeID, AccountID: config.AccountID, Model: config.Model, Reasoning: config.Reasoning, PermissionLevel: config.PermissionLevel, WorkspaceReadOnly: config.WorkspaceReadOnly}
}

func (s *Service) validateWorkflowConfiguration(ctx context.Context, wakerID string, config *models.AutomaticTaskConfiguration) error {
	_, err := s.resolveWorkflowConfiguration(ctx, wakerID, config)
	return err
}

func (s *Service) resolveWorkflowConfiguration(ctx context.Context, wakerID string, config *models.AutomaticTaskConfiguration) (models.WorkflowRevision, error) {
	if s.config.Workflows == nil {
		return models.WorkflowRevision{}, status_error.WorkflowCapabilityUnsupported
	}
	if config.ChannelID != "" || config.ExecutorWakerID != "" || len(config.MemberConfigs) != 0 || config.ProjectID != "" || config.WorkingDirectory != "" || config.Instruction != "" {
		return models.WorkflowRevision{}, status_error.BadRequest
	}
	if config.WorkflowRevisionPolicy != constants.WorkflowRevisionPolicyCurrent && config.WorkflowRevisionPolicy != constants.WorkflowRevisionPolicyPinned {
		return models.WorkflowRevision{}, status_error.BadRequest
	}
	if (config.WorkflowRevisionPolicy == constants.WorkflowRevisionPolicyPinned) != (config.WorkflowRevisionID != "") {
		return models.WorkflowRevision{}, status_error.BadRequest
	}
	definition, revision, err := s.config.Workflows.ValidateTargetConfiguration(ctx, config.WorkflowID, config.WorkflowRevisionID, workflowDefaults(*config))
	if err != nil {
		return models.WorkflowRevision{}, err
	}
	if definition.CreationWakerID != wakerID {
		return models.WorkflowRevision{}, status_error.WorkflowResourceInvalid.WithField("field", "creationWakerId")
	}
	if config.WorkflowInputRevisionID == "" {
		config.WorkflowInputRevisionID = revision.ID
	}
	_, base, err := s.config.Workflows.ReadTarget(ctx, config.WorkflowID, config.WorkflowInputRevisionID)
	if err != nil {
		return models.WorkflowRevision{}, err
	}
	allowAPI := config.Trigger == constants.AutomaticTaskTriggerAPI
	if base.ID != revision.ID {
		if err := workflowinput.Compatible(base.Manifest.Meta.InputSchema, revision.Manifest.Meta.InputSchema, config.WorkflowInputBindings, allowAPI); err != nil {
			return models.WorkflowRevision{}, err
		}
	}
	_, err = workflowinput.Analyze(revision.Manifest.Meta.InputSchema, config.WorkflowInputBindings, allowAPI, revision.Manifest.InputPropertyOrder[""])
	return revision, err
}

// validateTeamConfiguration 验证团队负责人和叠加任务专属覆盖后的成员执行配置。
func (s *Service) validateTeamConfiguration(ctx context.Context, wakerID string, config models.AutomaticTaskConfiguration) error {
	if s.config.Channels == nil || s.config.Teams == nil || s.config.Threads == nil || s.config.TeamTasks == nil {
		return status_error.AutomaticTaskServiceInvalid
	}
	team, err := s.config.Channels.Get(ctx, config.ChannelID)
	if err != nil {
		return err
	}
	if team.LeadWakerID != wakerID {
		return status_error.ChannelLeadInvalid
	}
	members, err := s.config.Channels.ListMembers(ctx, team.ID)
	if err != nil {
		return err
	}
	if len(members) < constants.ChannelMinimumMemberCount || len(members) > constants.ChannelMaximumMemberCount {
		return status_error.ChannelMemberCountInvalid
	}
	members, err = channelservice.ApplyMemberConfigOverrides(members, teamMemberOverrides(config))
	if err != nil {
		return err
	}
	lead := false
	for _, member := range members {
		if _, err := s.config.Wakers.Get(ctx, member.WakerID); err != nil {
			return err
		}
		transport := constants.RuntimeConnectorTransportUnspecified
		if member.WakerID == team.LeadWakerID {
			lead = true
			transport = constants.RuntimeConnectorTransportStreamableHTTP
		}
		if err := s.config.Executions.ValidateConfiguration(ctx, execution.ExecutionConfiguration{RuntimeID: member.RuntimeID, AccountID: member.AccountID, Model: member.Model, Reasoning: member.Reasoning, PermissionLevel: member.PermissionLevel, RequiredConnectorTransport: transport}); err != nil {
			return err
		}
	}
	if !lead {
		return status_error.ChannelLeadInvalid
	}
	return nil
}

// teamMemberOverrides 将已冻结的任务覆盖传入既有团队首发配置边界。
func teamMemberOverrides(config models.AutomaticTaskConfiguration) []channelservice.MemberExecutionConfigInput {
	result := make([]channelservice.MemberExecutionConfigInput, 0, len(config.MemberConfigs))
	for _, member := range config.MemberConfigs {
		result = append(result, channelservice.MemberExecutionConfigInput{WakerID: member.WakerID, RuntimeID: member.RuntimeID, AccountID: member.AccountID, Model: member.Model, Reasoning: member.Reasoning, PermissionLevel: member.PermissionLevel})
	}
	return result
}
