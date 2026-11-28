package schedule

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// RuleScope 由聊天宿主构造，模型不能指定管理的伙伴、团队、执行账号或工作目录。
type RuleScope struct {
	// WakerID 是当前伙伴或团队负责人。
	WakerID string
	// ChannelID 固定团队身份；空值表示只能管理当前伙伴的独立规则。
	ChannelID string
	// RuntimeID 是交办会话实际选用的智能体。
	RuntimeID string
	// AccountID 是交办会话实际选用的账号。
	AccountID string
	// Model 是交办会话实际选用的模型。
	Model string
	// Reasoning 是交办会话实际选用的推理强度。
	Reasoning string
	// PermissionLevel 是聊天创建工作流任务允许冻结的执行权限。
	PermissionLevel constants.PermissionLevel
	// WorkspaceReadOnly 是聊天的已选只读约束。
	WorkspaceReadOnly bool
	// ProjectID 是当前交办经宿主授权的项目。
	ProjectID string
	// Timezone 是宿主已经确认的本机 IANA 时区。
	Timezone string
}

// ConversationRulePatch 表示经模型工具或 Router 读取的显式规则变更。
type ConversationRulePatch struct {
	// Name 是规则名称。
	Name *string `json:"name,omitempty"`
	// Instruction 是每次运行的完整要求。
	Instruction *string `json:"instruction,omitempty"`
	// Schedule 是含时区的完整计划。
	Schedule *models.AutomaticTaskSchedule `json:"schedule,omitempty"`
	// Workflow 指定新规则的工作流版本与固定参数。
	Workflow *ConversationRuleWorkflow `json:"workflow,omitempty"`
	// Event 保留旧规则内部使用的事件配置。
	Event *eventsource.Config `json:"event,omitempty"`
	// ReportDestination 是该规则今后的报告位置。
	ReportDestination *constants.AutomaticReportDestination `json:"reportDestination,omitempty"`
	// ReportPolicy 是每次报告或只报变化。
	ReportPolicy *constants.AutomaticReportPolicy `json:"reportPolicy,omitempty"`
	// Enabled 保存显式的启停选择。
	Enabled *bool `json:"enabled,omitempty"`
}

// ConversationRuleWorkflow 是聊天创建定时任务使用的工作流目标。
type ConversationRuleWorkflow struct {
	// ID 是用户选择或伙伴刚创建的工作流。
	ID string `json:"id"`
	// RevisionID 固定已核对输入和权限的工作流版本。
	RevisionID string `json:"revisionId"`
	// Args 是每次检查使用的固定参数。
	Args map[string]any `json:"args,omitempty"`
}

// ConversationRuleReceipt 确认已经提交的规则以及原管理详情入口。
type ConversationRuleReceipt struct {
	// TaskID 是规则的真实标识。
	TaskID string `json:"taskId"`
	// Href 打开同一规则的应用管理详情。
	Href string `json:"href"`
	// Task 是规则的当前配置和运行概览；删除收据省略。
	Task *TaskView `json:"task,omitempty"`
	// Runs 是最近五次真实机会；列表项只呈现最近一次。
	Runs []RunView `json:"runs,omitempty"`
}

func ruleReceipt(id string) ConversationRuleReceipt {
	return ConversationRuleReceipt{TaskID: id, Href: "/automatic-tasks/" + id}
}

func (s *Service) validateRuleScope(ctx context.Context, scope RuleScope) error {
	if strings.TrimSpace(scope.WakerID) == "" {
		return status_error.AgentContextUnauthorized
	}
	if scope.ChannelID == "" {
		return nil
	}
	if s.config.Channels == nil {
		return status_error.AgentContextUnauthorized
	}
	channel, err := s.config.Channels.Get(ctx, scope.ChannelID)
	if err != nil {
		return err
	}
	if channel.LeadWakerID != scope.WakerID {
		return status_error.AgentContextUnauthorized
	}
	return nil
}

func (s *Service) scopedRule(ctx context.Context, scope RuleScope, id string) (models.AutomaticTask, error) {
	if err := s.validateRuleScope(ctx, scope); err != nil {
		return models.AutomaticTask{}, err
	}
	task, err := s.getTask(ctx, "", id)
	if err != nil {
		return task, err
	}
	if !scope.permits(task) {
		return models.AutomaticTask{}, status_error.AutomaticTaskNotFound
	}
	return task, nil
}

func (scope RuleScope) recipient() models.MainConversationRecipient {
	if scope.ChannelID != "" {
		return models.MainConversationRecipient{SubjectType: "team", SubjectID: scope.ChannelID}
	}
	return models.MainConversationRecipient{SubjectType: "waker", SubjectID: scope.WakerID}
}

func (scope RuleScope) permits(task models.AutomaticTask) bool {
	if task.Trigger == constants.AutomaticTaskTriggerAPI || task.Knowledge != nil || task.WorkflowTriggerDetached {
		return false
	}
	if task.WorkflowID != "" {
		return task.WorkflowReportRecipient != nil && *task.WorkflowReportRecipient == scope.recipient() &&
			(scope.ChannelID != "" || task.WakerID == scope.WakerID)
	}
	return task.ChannelID == scope.ChannelID && (scope.ChannelID != "" || task.WakerID == scope.WakerID)
}

// CreateConversationRule 使用同一任务保存服务创建绑定当前主体的规则。
func (s *Service) CreateConversationRule(ctx context.Context, scope RuleScope, patch ConversationRulePatch) (ConversationRuleReceipt, error) {
	if patch.Schedule == nil || patch.Event != nil {
		return ConversationRuleReceipt{}, status_error.BadRequest
	}
	if err := s.validateRuleScope(ctx, scope); err != nil {
		return ConversationRuleReceipt{}, err
	}
	config := models.AutomaticTaskConfiguration{
		ChannelID: scope.ChannelID, RuntimeID: scope.RuntimeID, AccountID: scope.AccountID, Model: scope.Model,
		Reasoning: scope.Reasoning, ProjectID: scope.ProjectID, Enabled: true,
		PermissionLevel: constants.PermissionLevelDefault, Trigger: constants.AutomaticTaskTriggerSchedule,
		ReportDestination: constants.AutomaticReportDestinationMainConversation, ReportPolicy: constants.AutomaticReportPolicyAlways,
	}
	applyRulePatch(&config, patch)
	if config.Trigger == constants.AutomaticTaskTriggerSchedule && config.Schedule.Timezone == "" {
		config.Schedule.Timezone = scope.Timezone
	}
	wakerID := scope.WakerID
	if patch.Workflow != nil {
		if err := s.bindConversationWorkflow(ctx, scope, patch, &config); err != nil {
			return ConversationRuleReceipt{}, err
		}
		// 全局保存从工作流归属解析任务伙伴，收件人已由会话主体固定。
		wakerID = ""
	}
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	result, err := s.saveConfiguration(ctx, wakerID, "", 0, config, true, patch.Workflow != nil)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	return ruleReceipt(result.TaskID), nil
}

func (s *Service) bindConversationWorkflow(ctx context.Context, scope RuleScope, patch ConversationRulePatch, config *models.AutomaticTaskConfiguration) error {
	target := patch.Workflow
	if patch.Schedule == nil || patch.Instruction != nil || target.ID == "" || target.RevisionID == "" ||
		(patch.ReportDestination != nil && *patch.ReportDestination != constants.AutomaticReportDestinationMainConversation) {
		return status_error.AgentInputInvalid
	}
	if s.config.Workflows == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	definition, _, err := s.config.Workflows.ReadTarget(ctx, target.ID, target.RevisionID)
	if err != nil {
		return err
	}
	if definition.CreationWakerID != scope.WakerID {
		return status_error.AgentOperationForbidden
	}
	config.WorkflowID = target.ID
	config.WorkflowRevisionID = target.RevisionID
	config.WorkflowRevisionPolicy = constants.WorkflowRevisionPolicyPinned
	config.WorkflowInputBindings = make(map[string]workflowinput.Binding, len(target.Args))
	for name, value := range target.Args {
		config.WorkflowInputBindings[name] = workflowinput.Fixed(value)
	}
	recipient := scope.recipient()
	config.WorkflowReportRecipient = &recipient
	config.ReportDestination = ""
	config.PermissionLevel = cmp.Or(scope.PermissionLevel, constants.PermissionLevelDefault)
	config.WorkspaceReadOnly = scope.WorkspaceReadOnly
	config.ChannelID = ""
	config.ProjectID = ""
	return nil
}

// UpdateConversationRule 在同一协调锁下读取归属和版本，并只应用显式提供的字段。
func (s *Service) UpdateConversationRule(ctx context.Context, scope RuleScope, id string, patch ConversationRulePatch) (ConversationRuleReceipt, error) {
	if patch.Workflow != nil || (patch.Schedule != nil && patch.Event != nil) {
		return ConversationRuleReceipt{}, status_error.BadRequest
	}
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	task, err := s.scopedRule(ctx, scope, id)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	config := task.AutomaticTaskConfiguration
	if task.WorkflowID != "" && (patch.Event != nil || patch.Instruction != nil || patch.ReportDestination != nil) {
		return ConversationRuleReceipt{}, status_error.AgentInputInvalid
	}
	applyRulePatch(&config, patch)
	if config.Trigger == constants.AutomaticTaskTriggerSchedule && config.Schedule.Timezone == "" {
		config.Schedule.Timezone = task.Schedule.Timezone
		if config.Schedule.Timezone == "" {
			config.Schedule.Timezone = scope.Timezone
		}
	}
	result, err := s.SaveConfiguration(ctx, task.WakerID, id, task.ConfigurationVersion, config, config.Enabled)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	return ruleReceipt(result.TaskID), nil
}

func applyRulePatch(config *models.AutomaticTaskConfiguration, patch ConversationRulePatch) {
	if patch.Name != nil {
		config.Name = *patch.Name
	}
	if patch.Instruction != nil {
		config.Instruction = *patch.Instruction
	}
	if patch.Schedule != nil {
		config.Schedule = *patch.Schedule
		config.Trigger = constants.AutomaticTaskTriggerSchedule
		config.Event = nil
	}
	if patch.Event != nil {
		config.Event = patch.Event
		config.Trigger = constants.AutomaticTaskTriggerEvent
		config.Schedule = models.AutomaticTaskSchedule{}
	}
	if patch.ReportDestination != nil {
		config.ReportDestination = *patch.ReportDestination
	}
	if patch.ReportPolicy != nil {
		config.ReportPolicy = *patch.ReportPolicy
	}
	if patch.Enabled != nil {
		config.Enabled = *patch.Enabled
	}
}

// DeleteConversationRule 删除已核验主体的同一管理规则。
func (s *Service) DeleteConversationRule(ctx context.Context, scope RuleScope, id string) error {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	if _, err := s.scopedRule(ctx, scope, id); err != nil {
		return err
	}
	return s.Delete(ctx, "", id)
}

// ListConversationRules 返回一批当前主体的规则，与管理页读取同一配置和运行概览。
func (s *Service) ListConversationRules(ctx context.Context, scope RuleScope) ([]ConversationRuleReceipt, error) {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	if err := s.validateRuleScope(ctx, scope); err != nil {
		return nil, err
	}
	rows, err := s.listScopedRules(ctx, scope, "")
	if err != nil {
		return nil, err
	}
	receipts := make([]ConversationRuleReceipt, 0, min(len(rows), 50))
	for _, row := range rows {
		view, err := s.taskView(ctx, row)
		if err != nil {
			return nil, err
		}
		receipt := ruleReceipt(row.ID)
		receipt.Task = &view
		receipts = append(receipts, receipt)
		if len(receipts) == 50 {
			break
		}
	}
	return receipts, nil
}

// GetConversationRule 返回指定规则完整配置与最近五次运行。
func (s *Service) GetConversationRule(ctx context.Context, scope RuleScope, id string) (ConversationRuleReceipt, error) {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	task, err := s.scopedRule(ctx, scope, id)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	view, err := s.taskDetail(ctx, task)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	receipt := ruleReceipt(id)
	receipt.Task = &view
	rows, _, err := s.config.Tasks.ListRuns(ctx, id, 5, 0)
	if err != nil {
		return ConversationRuleReceipt{}, err
	}
	for _, row := range rows {
		run, err := s.ReadRun(ctx, row)
		if err != nil {
			return ConversationRuleReceipt{}, err
		}
		receipt.Runs = append(receipt.Runs, run)
	}
	return receipt, nil
}

// FindConversationRuleID 校验当前主体中具有唯一名称的规则。
func (s *Service) FindConversationRuleID(ctx context.Context, scope RuleScope, name string) (string, error) {
	if err := s.validateRuleScope(ctx, scope); err != nil {
		return "", err
	}
	rows, err := s.listScopedRules(ctx, scope, name)
	if err != nil {
		return "", err
	}
	retained := ""
	for _, task := range rows {
		if task.Name != name {
			continue
		}
		if retained != "" {
			return "", status_error.AgentInputInvalid
		}
		retained = task.ID
	}
	if retained == "" {
		return "", status_error.AutomaticTaskNotFound
	}
	return retained, nil
}

func (s *Service) listScopedRules(ctx context.Context, scope RuleScope, query string) ([]models.AutomaticTask, error) {
	filter := controllers.AutomaticTaskFilter{WakerID: scope.WakerID, Query: query}
	if scope.ChannelID != "" {
		filter = controllers.AutomaticTaskFilter{ChannelID: scope.ChannelID, Query: query}
	}
	rows, _, err := s.config.Tasks.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if scope.ChannelID != "" {
		recipient := scope.recipient()
		workflows, _, err := s.config.Tasks.List(ctx, controllers.AutomaticTaskFilter{WorkflowReportRecipient: &recipient, ExecutorKind: "workflow", Query: query})
		if err != nil {
			return nil, err
		}
		rows = append(rows, workflows...)
		slices.SortFunc(rows, func(a, b models.AutomaticTask) int {
			if order := cmp.Compare(b.CreatedAt, a.CreatedAt); order != 0 {
				return order
			}
			return cmp.Compare(a.ID, b.ID)
		})
	}
	return slices.DeleteFunc(rows, func(task models.AutomaticTask) bool { return !scope.permits(task) }), nil
}
