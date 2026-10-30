package workflows

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发送工作流创作消息
type SendWorkflowCreationMessage struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body SendCreationMessageCommand `in:"body"`
}

// 工作流创作消息响应
type SendWorkflowCreationMessageResponse struct {
	// 创作会话
	models.WorkflowCreationSession
	// 接纳事实包含已取得的聊天和执行快照。
	execution.AcceptedExecution
}

func (*SendWorkflowCreationMessage) Path() string { return "/:id/creation/messages" }
func (o *SendWorkflowCreationMessage) Output(ctx *gin.Context) (interface{}, error) {
	o.Body.DefinitionID = o.ID
	accepted, err := sendCreationMessage(ctx, dependencies(), o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return accepted, nil
}

// 工作流创作消息
type SendCreationMessageCommand struct {
	// 工作流标识
	DefinitionID string `json:"-"`
	// 创作会话
	SessionID string `json:"sessionId"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 修改要求
	Content string `json:"content"`
	// 消息附件
	Attachments []workinput.Attachment `json:"attachments,omitempty"`
	// 代码引用
	CodeReferences []workinput.CodeReference `json:"codeReferences,omitempty"`
	// 引用技能
	SkillIDs []string `json:"skillIds,omitempty"`
	// 引用连接器
	PluginIDs []string `json:"pluginIds,omitempty" validate:"max=100,dive,max=32"`
	// 连接器列表
	ConnectorIDs []string `json:"connectorIds,omitempty"`
	// 新建原生会话
	StartNewSession bool `json:"startNewSession"`
	// 历史轮数
	HistoryTurns int `json:"historyTurns"`
	// 历史交接方式
	HistoryMode constants.HistoryMode `json:"historyMode"`
	// 总结模型
	SummaryModel string `json:"summaryModel"`
	// 总结思考
	SummaryReasoning string `json:"summaryReasoning"`
	// 执行配置
	Defaults models.WorkflowExecutionDefaults `json:"defaults"`
}

// sendCreationMessage 先提交输入锚点，再通过既有 Core 接纳创作工作。
func sendCreationMessage(ctx context.Context, d *workflowDependencies, command SendCreationMessageCommand) (SendWorkflowCreationMessageResponse, error) {
	if d.CreationSessions == nil || d.Executions == nil || d.State.AgentBindings == nil {
		return SendWorkflowCreationMessageResponse{}, status_error.WorkflowCapabilityUnsupported
	}
	if strings.TrimSpace(command.Content) == "" || len(command.Content) > constants.WorkflowSourceLimitBytes || command.RequestID == "" || len(command.RequestID) > constants.InputSubmissionIDMaximumLength {
		return SendWorkflowCreationMessageResponse{}, status_error.BadRequest
	}
	if command.Defaults.PermissionLevel == "" {
		command.Defaults.PermissionLevel = constants.PermissionLevelDefault
	}
	d.State.CreationCommands.Lock()
	defer d.State.CreationCommands.Unlock()
	if command.SessionID != "" {
		previous, err := d.CreationSessions.GetCreationSession(ctx, command.SessionID)
		if err != nil {
			return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, err
		}
		if previous.DefinitionID != command.DefinitionID {
			return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, status_error.WorkflowStateConflict
		}
		if previous.RequestID != command.RequestID {
			chatID := previous.ChatID
			if chatID == "" {
				chatID = requestidempotency.Identity{ID: previous.ID}.ResourceID("chat", previous.WakerID)
			}
			if d.Chats == nil {
				return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, status_error.WorkflowCapabilityUnsupported
			}
			accepted, err := d.Chats.ReadExecution(ctx, chatID, previous.ActiveExecutionID)
			if err != nil {
				// 没有提交过 Chat 的输入锚点可以被下一轮要求替换；已接纳记录仍须严格对账。
				key := workflowstate.ErrorKey(err)
				if previous.ChatID != "" || previous.AcceptedExecutionID != "" || (key != status_error.ChatNotFound.Key() && key != status_error.ExecutionNotFound.Key()) {
					return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, err
				}
			} else {
				if accepted.CompletedAt == nil {
					return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, status_error.WorkflowStateConflict.WithField("reason", "creation_execution_active")
				}
				// Core 已提交而确认写入未完成时，先保留已有 Chat，下一轮按续聊接纳。
				if previous.ChatID == "" {
					previous.ChatID = chatID
					previous.AcceptedExecutionID = accepted.ID
					if err := d.CreationSessions.UpdateCreationSession(ctx, previous); err != nil {
						return SendWorkflowCreationMessageResponse{WorkflowCreationSession: previous}, err
					}
				}
			}
		}
	}
	configuration := execution.ExecutionConfiguration{RuntimeID: command.Defaults.RuntimeID, AccountID: command.Defaults.AccountID, Model: command.Defaults.Model, Reasoning: command.Defaults.Reasoning, PermissionLevel: command.Defaults.PermissionLevel}
	if err := d.Executions.ValidateConfiguration(ctx, configuration); err != nil {
		return SendWorkflowCreationMessageResponse{}, err
	}
	digest := workflowstate.Digest(command)
	d.State.Commands.Lock()
	var session models.WorkflowCreationSession
	err := d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, found, err := d.Store.FindReceipt(ctx, command.DefinitionID, command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "creation" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			id, _ := receipt.Result["sessionId"].(string)
			session, err = d.CreationSessions.GetCreationSession(ctx, id)
			return err
		}
		definition, err := d.Store.GetDefinition(ctx, command.DefinitionID)
		if err != nil {
			return err
		}
		if definition.CreationWakerID == "" {
			return status_error.WorkflowResourceInvalid.WithField("field", "creationWakerId")
		}
		if d.Wakers != nil {
			if _, err := d.Wakers.Get(ctx, definition.CreationWakerID); err != nil {
				return err
			}
		}
		if command.SessionID != "" {
			session, err = d.CreationSessions.GetCreationSession(ctx, command.SessionID)
			if err != nil {
				return err
			}
			if session.DefinitionID != definition.ID || session.WakerID != definition.CreationWakerID {
				return status_error.WorkflowStateConflict
			}
		} else {
			session = models.WorkflowCreationSession{ID: util.New(), DefinitionID: definition.ID, WakerID: definition.CreationWakerID}
		}
		session.ActiveExecutionID = util.New()
		session.RequestID = command.RequestID
		session.Content = command.Content
		session.Defaults = command.Defaults
		session.AuthToken = util.New() + util.New()
		if command.SessionID == "" {
			err = d.CreationSessions.CreateCreationSession(ctx, session)
		} else {
			err = d.CreationSessions.UpdateCreationSession(ctx, session)
		}
		if err != nil {
			return err
		}
		return d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: definition.ID, RequestID: command.RequestID, Kind: "creation", PayloadDigest: digest, Result: map[string]any{"sessionId": session.ID, "executionId": session.ActiveExecutionID}})
	})
	d.State.Commands.Unlock()
	if err != nil {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, err
	}
	if session.RequestID != command.RequestID {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, status_error.WorkflowStateConflict
	}
	if d.Chats == nil {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, status_error.WorkflowCapabilityUnsupported
	}
	// 接纳与 Workflow 确认不跨存储原子；只按保留身份对账，不能将未知结果重新派发。
	chatID := session.ChatID
	if chatID == "" {
		chatID = requestidempotency.Identity{ID: session.ID}.ResourceID("chat", session.WakerID)
	}
	if actual, readErr := d.Chats.ReadExecution(ctx, chatID, session.ActiveExecutionID); readErr == nil {
		if session.AcceptedExecutionID != actual.ID {
			session.ChatID, session.AcceptedExecutionID = chatID, actual.ID
			if err := d.Transactions.Transaction(ctx, func(ctx context.Context) error {
				if err := d.CreationSessions.UpdateCreationSession(ctx, session); err != nil {
					return err
				}
				_, err := d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: session.DefinitionID, EventID: util.New(), Type: "workflow.creation.changed", EntityID: session.ID, Payload: map[string]any{"sessionId": session.ID, "chatId": chatID}})
				return err
			}); err != nil {
				return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, err
			}
		}
		summary, err := d.Chats.GetChatSummary(ctx, chatID)
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session, AcceptedExecution: execution.AcceptedExecution{Chat: summary.Chat, Execution: actual, Replayed: true}}, err
	} else if session.AcceptedExecutionID == session.ActiveExecutionID || (workflowstate.ErrorKey(readErr) != status_error.ChatNotFound.Key() && workflowstate.ErrorKey(readErr) != status_error.ExecutionNotFound.Key()) {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, readErr
	}
	definition, err := d.Store.GetDefinition(ctx, command.DefinitionID)
	if err != nil {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, err
	}
	target := execution.ChatTarget{ChatID: session.ChatID}
	clientID := ""
	if session.ChatID == "" {
		scope := definition.ScopeBinding
		if scope.ProjectID == "" && scope.WorkingDirectory == "" {
			scope.WorkingDirectory, err = workflowfiles.PrepareManagedWorkspace(d.DataDirectory, definition.ID)
			if err != nil {
				return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, err
			}
		}
		target.Create = &execution.CreateChatSpec{HiddenFromWakerHistory: true, WakerID: session.WakerID, ProjectID: scope.ProjectID, WorkingDirectory: scope.WorkingDirectory}
		clientID = session.ID
	}

	sessionPolicy := constants.RuntimeSessionPolicyContinue
	if command.StartNewSession {
		sessionPolicy = constants.RuntimeSessionPolicyNew
	}
	accepted, err := d.Executions.Start(ctx, execution.WorkIntent{
		ExecutionID: session.ActiveExecutionID, ClientRequestID: clientID, Chat: target,
		Input: execution.WorkInput{
			SubmissionID: session.RequestID,
			Actor:        string(constants.ChannelMessageActorUser), Content: session.Content,
			Attachments: command.Attachments, CodeReferences: command.CodeReferences,
			SkillIDs: command.SkillIDs, ConnectorIDs: command.ConnectorIDs, PluginIDs: command.PluginIDs,
		},
		Options: execution.WorkOptions{
			RuntimeID: session.Defaults.RuntimeID, AccountID: session.Defaults.AccountID,
			Model: session.Defaults.Model, Reasoning: session.Defaults.Reasoning,
			PermissionLevel: session.Defaults.PermissionLevel, SessionPolicy: sessionPolicy,
			HistoryTurns: command.HistoryTurns, HistoryMode: command.HistoryMode,
			SummaryModel: command.SummaryModel, SummaryReasoning: command.SummaryReasoning,
			AllowedAgentDomains: &[]string{"memory", "interaction"},
			ToolCapabilities: []execution.ToolCapability{{
				ID: "workflow.authoring", Name: "工作流创作", Bindings: d.State.AgentBindings(session.ID, session.AuthToken, constants.WorkflowCapabilityScopeCreation),
			}},
		},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session}, err
	}
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	err = d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		current, err := d.CreationSessions.GetCreationSession(ctx, session.ID)
		if err != nil {
			return err
		}
		if current.ActiveExecutionID != accepted.Execution.ID {
			return status_error.WorkflowStateConflict
		}
		current.ChatID = accepted.Chat.ID
		current.AcceptedExecutionID = accepted.Execution.ID
		session = current
		if err := d.CreationSessions.UpdateCreationSession(ctx, current); err != nil {
			return err
		}
		_, err = d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: current.DefinitionID, EventID: util.New(), Type: "workflow.creation.changed", EntityID: current.ID, Payload: map[string]any{"sessionId": current.ID, "chatId": current.ChatID}})
		return err
	})
	return SendWorkflowCreationMessageResponse{WorkflowCreationSession: session, AcceptedExecution: accepted}, err
}
