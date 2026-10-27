package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
)

// BindIntent 将已授权自动任务或普通聊天 CLI 的目标绑定到精确整理操作。
func (s *Service) BindIntent(ctx context.Context, intent execution.WorkIntent, auth models.KnowledgeMaintenanceAuthorization) (execution.WorkIntent, error) {
	if err := bridge.RequireLocal(bridge.ResourceRef{Backend: auth.Backend, ConnectionID: auth.ConnectionID, KnowledgeBaseID: auth.KnowledgeBaseID}); err != nil {
		return intent, err
	}
	waker, project := "", ""
	if intent.Chat.Create != nil {
		waker = intent.Chat.Create.WakerID
		project = intent.Chat.Create.ProjectID
	} else {
		summary, err := s.config.Chats.GetChatSummary(ctx, intent.Chat.ChatID)
		if err != nil {
			return intent, err
		}
		waker = summary.Chat.WakerID
		project = summary.Chat.ProjectID
	}
	lib, err := s.config.Engine.GetLibrary(ctx, auth.KnowledgeBaseID)
	if err != nil {
		return intent, bridge.MapError(err)
	}
	if err := s.validateTarget(ctx, waker, project, lib); err != nil {
		return intent, err
	}
	if auth.Operation == constants.KnowledgeWikiMaintainOperation {
		return s.bindAutomaticMaintenance(ctx, intent, auth, lib, waker, project)
	}
	validateTarget := func(ctx context.Context) error {
		current, err := s.config.Engine.GetLibrary(ctx, lib.ID)
		if err != nil {
			return bridge.MapError(err)
		}
		if current.PolicyVersion != lib.PolicyVersion {
			return status_error.KnowledgeForbidden
		}
		if intent.ExecutionID != "" && intent.Chat.ChatID != "" {
			summary, err := s.config.Chats.GetChatSummary(ctx, intent.Chat.ChatID)
			if err != nil {
				return err
			}
			if summary.Chat.ActiveExecutionID != intent.ExecutionID {
				return status_error.KnowledgeForbidden
			}
		}
		return s.validateTarget(ctx, waker, project, current)
	}
	validate := func(ctx context.Context) error {
		if intent.AutomaticRunID != "" && s.config.InvocationGuard != nil {
			release, err := s.config.InvocationGuard(ctx, intent.AutomaticRunID)
			if err != nil {
				return err
			}
			release()
		}
		return validateTarget(ctx)
	}
	var binding agentcapability.Binding
	switch auth.Operation {
	case "organize":
		// 不得把空来源解释为全库；空时在签发时冻结当前pending集合。
		originalAuthorization := auth
		if len(auth.SourceVersionIDs) == 0 && len(auth.AttachmentIndexes) == 0 {
			auth.SourceVersionIDs, err = s.pendingSourceHeads(ctx, lib.ID)
			if err != nil {
				return intent, err
			}
		}

		// 先核对原冻结授权，再展开 pending，不能把模型指定目标当自动任务授权。
		session, err := s.automaticSession(ctx, intent, originalAuthorization, lib, models.KnowledgeTaskConfiguration{KnowledgeBaseID: lib.ID, RuntimeID: intent.Options.RuntimeID, AccountID: intent.Options.AccountID, Model: intent.Options.Model, Reasoning: intent.Options.Reasoning})
		if err != nil {
			return intent, err
		}
		frozen := append([]string(nil), auth.SourceVersionIDs...)
		selected := append([]int(nil), auth.AttachmentIndexes...)
		seen := map[int]bool{}
		for _, index := range selected {
			if index < 0 || index >= len(intent.Input.Attachments) || seen[index] {
				return intent, status_error.KnowledgeInvalid
			}
			seen[index] = true
		}
		originChat := intent.Chat.ChatID
		if originChat == "" {
			originChat = (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", waker)
		}

		identity := func(id string) string {
			if intent.AutomaticRunID == "" {
				return id
			}
			return (requestidempotency.Identity{ID: id}).ResourceID("automatic-knowledge", intent.AutomaticRunID, lib.ID)
		}
		binding = agentcapability.Binding{Operation: "knowledge.jobs.start", Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			if err := validate(ctx); err != nil {
				return agentcapability.Result{}, err
			}
			chosen := append([]string(nil), frozen...)
			if len(selected) > 0 {
				imported, err := s.importSelectedAttachments(ctx, originChat, lib.ID, identity(r.RequestID), selected, validateTarget, s.publishGuard(session))
				if err != nil {
					return agentcapability.Result{}, err
				}
				chosen = append(chosen, imported...)
			}
			receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: identity(r.RequestID), LibraryID: lib.ID, SourceVersionIDs: chosen, ExplicitSources: true, Configuration: session, Message: intent.Input.Content})
			if err != nil {
				return agentcapability.Result{}, err
			}
			return agentcapability.Result{CommandID: r.RequestID, Data: map[string]any{"receipt": receipt, "noop": receipt == nil || receipt.State == "no-op"}}, nil
		}}
	default:
		return intent, status_error.KnowledgeInvalid
	}
	binding.Receipt = func(ctx context.Context, id string) (agentcapability.Result, error) {
		if err := validate(ctx); err != nil {
			return agentcapability.Result{}, err
		}
		requestID := id
		if intent.AutomaticRunID != "" {
			requestID = (requestidempotency.Identity{ID: id}).ResourceID("automatic-knowledge", intent.AutomaticRunID, lib.ID)
		}
		r, err := s.config.Engine.GetReceipt(ctx, requestID)
		if err != nil {
			return agentcapability.Result{}, bridge.MapError(err)
		}
		if r.LibraryID != lib.ID {
			return agentcapability.Result{}, status_error.KnowledgeForbidden
		}
		return agentcapability.Result{CommandID: id, Replayed: true, Data: map[string]any{"receipt": r, "noop": false}}, nil
	}
	// 普通聊天只获得明确目标的作业触发能力，模型生成完全由知识后台任务拥有。
	intent.Options.ToolCapabilities = append(intent.Options.ToolCapabilities, execution.ToolCapability{ID: "knowledge.organize", Name: "知识整理", Bindings: []agentcapability.Binding{binding}})

	return intent, nil
}

// chatOrganizeInput 只接收整理所需的业务选择，不接收文件路径或执行权限。
type chatOrganizeInput struct {
	// LibraryID 是用户指定且本轮可见的知识库。
	LibraryID string `json:"libraryId"`
	// Backend 保留完整资源身份，远端目标不能回退本机。
	Backend string `json:"backend"`
	// ConnectionID 是目标连接身份。
	ConnectionID string `json:"connectionId"`
	// AttachmentIndexes 只索引触发本轮执行的用户消息附件。
	AttachmentIndexes []int `json:"attachmentIndexes"`
	// SourceVersionIDs 是用户明确限定的已有来源版本。
	SourceVersionIDs []string `json:"sourceVersionIds"`
	// Message 是伙伴补充的发现与整理要求；服务端另行保留用户原始要求。
	Message string `json:"message"`
}

// chatJobBinding 为普通对话提供知识任务入口，复用手动整理、维护及持久回执。
func (s *Service) chatJobBinding(ctx context.Context, chat models.Chat, refs []bridge.ResourceRef, operation string) agentcapability.Binding {
	maintenance := operation == "knowledge.wiki.maintain"
	anchorOperation := constants.KnowledgeChatOrganizeOperation
	namespace := "knowledge-chat-job"
	if maintenance {
		anchorOperation = constants.KnowledgeChatMaintenanceOperation
		namespace = constants.KnowledgeChatMaintenanceOperation
	}
	libraries := map[string]bool{}
	for _, ref := range refs {
		if ref.Backend != "local" || ref.ConnectionID != "local" {
			continue
		}
		libraries[ref.KnowledgeBaseID] = true
	}
	validate := func(ctx context.Context, id string) error {
		if !libraries[id] {
			return status_error.KnowledgeForbidden
		}
		lib, err := s.config.Engine.GetLibrary(ctx, id)
		if err != nil {
			return bridge.MapError(err)
		}
		if err := s.validateTarget(ctx, chat.WakerID, chat.ProjectID, lib); err != nil {
			return err
		}
		current, err := s.config.Chats.GetChatSummary(ctx, chat.ID)
		if err != nil {
			return err
		}
		if current.Chat.ActiveExecutionID != chat.ActiveExecutionID || current.Chat.WakerID != chat.WakerID || current.Chat.ProjectID != chat.ProjectID {
			return status_error.KnowledgeForbidden
		}
		return nil
	}
	identity := func(id string) string {
		return (requestidempotency.Identity{ID: id}).ResourceID(namespace, chat.ID, chat.ActiveExecutionID)
	}
	receipt := func(ctx context.Context, id string) (agentcapability.Result, error) {
		r, err := s.config.Engine.GetReceipt(ctx, identity(id))
		if err != nil {
			return agentcapability.Result{}, bridge.MapError(err)
		}
		if err := validate(ctx, r.LibraryID); err != nil {
			return agentcapability.Result{}, err
		}
		// 核心可能已接纳而宿主补记失败；沿原任务配置恢复启动，不能只回放接纳回执。
		link, err := s.config.References.GetOperation(ctx, r.RequestID)
		if err != nil {
			return agentcapability.Result{}, err
		}
		session, err := s.config.References.GetTaskConfiguration(ctx, link.ConfigurationID)
		if err != nil {
			return agentcapability.Result{}, err
		}
		for _, key := range []string{r.RequestID, r.RequestID + "/input"} {
			if err := s.config.References.CompleteOperation(ctx, key, r.LibraryID, r.ResourceID, r.JobID); err != nil {
				return agentcapability.Result{}, err
			}
		}
		if r.ResourceID != "" {
			s.drive(r.ResourceID, session)
		}
		return agentcapability.Result{CommandID: id, Replayed: true, Data: map[string]any{"receipt": r, "noop": r.State == "no-op"}}, nil
	}
	return agentcapability.Binding{Operation: operation, Receipt: receipt, Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
		raw, err := json.Marshal(r.Params)
		if err != nil {
			return agentcapability.Result{}, status_error.KnowledgeInvalid
		}
		var in chatOrganizeInput
		if json.Unmarshal(raw, &in) != nil || in.LibraryID == "" || r.RequestID == "" {
			return agentcapability.Result{}, status_error.KnowledgeInvalid
		}
		if maintenance && (len(in.AttachmentIndexes) != 0 || len(in.SourceVersionIDs) != 0) {
			return agentcapability.Result{}, status_error.KnowledgeInvalid
		}
		if in.Backend == "" {
			in.Backend = "local"
		}
		if in.ConnectionID == "" && in.Backend == "local" {
			in.ConnectionID = "local"
		}
		if err := bridge.RequireLocal(bridge.ResourceRef{Backend: in.Backend, ConnectionID: in.ConnectionID, KnowledgeBaseID: in.LibraryID}); err != nil {
			return agentcapability.Result{}, err
		}
		if err := validate(ctx, in.LibraryID); err != nil {
			return agentcapability.Result{}, err
		}
		// 输入锚点固定原请求选择；核心回执使用执行范围身份，避免跨聊天碰撞。
		digest := sha256.Sum256(raw)
		key := identity(r.RequestID)
		if err := s.config.References.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: key + "/input", Operation: anchorOperation, Digest: hex.EncodeToString(digest[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: in.LibraryID, ChatID: chat.ID, ExecutionID: chat.ActiveExecutionID}); err != nil {
			return agentcapability.Result{}, err
		}
		if old, err := receipt(ctx, r.RequestID); err == nil {
			return old, nil
		} else if !errors.Is(err, status_error.KnowledgeNotFound) {
			return agentcapability.Result{}, err
		}
		active, err := s.config.Chats.ReadExecution(ctx, chat.ID, chat.ActiveExecutionID)
		if err != nil {
			return agentcapability.Result{}, err
		}
		history, err := s.config.Chats.ReadMessageHistory(ctx, chat.ID, 0, 100)
		if err != nil {
			return agentcapability.Result{}, err
		}
		for _, message := range history.Resources {
			if message.ID != active.UserMessageID {
				continue
			}
			// 原始要求由权威消息读取，不能被模型提交的摘要或补充发现替换。
			requirements := message.Content
			if in.Message != "" && in.Message != message.Content {
				requirements += "\n\nCompanion findings and additional requirements (within the user's requested scope):\n" + in.Message
			}
			configuration := models.KnowledgeTaskConfiguration{RuntimeID: active.RuntimeID, AccountID: active.AccountID, Model: active.Model, Reasoning: active.Reasoning}
			// Chat 的空账号已解析为系统登录；知识入口的空账号却会重选默认值。
			if configuration.AccountID == "" {
				configuration.AccountID = constants.AiAccountSystemID
			}
			if maintenance {
				accepted, _, err := s.Organize(ctx, OrganizeCommand{RequestID: key, LibraryID: in.LibraryID, Maintenance: true, Configuration: configuration, Message: requirements})
				if err != nil {
					return agentcapability.Result{}, err
				}
				if err := s.config.References.CompleteOperation(ctx, key+"/input", accepted.LibraryID, accepted.ResourceID, accepted.JobID); err != nil {
					return agentcapability.Result{}, err
				}
				return agentcapability.Result{CommandID: r.RequestID, Data: map[string]any{"receipt": accepted, "noop": false}}, nil
			}
			attachments := make([]execution.AttachmentInput, len(message.Attachments))
			// 聊天只提交业务选择，本轮执行配置由服务端冻结，不接受模型覆盖。
			intent := execution.WorkIntent{ExecutionID: chat.ActiveExecutionID, Chat: execution.ChatTarget{ChatID: chat.ID}, Input: execution.WorkInput{Content: requirements, Attachments: attachments}, Options: execution.WorkOptions{RuntimeID: configuration.RuntimeID, AccountID: configuration.AccountID, Model: configuration.Model, Reasoning: configuration.Reasoning}}
			bound, err := s.BindIntent(ctx, intent, models.KnowledgeMaintenanceAuthorization{Backend: in.Backend, ConnectionID: in.ConnectionID, KnowledgeBaseID: in.LibraryID, Operation: "organize", AttachmentIndexes: in.AttachmentIndexes, SourceVersionIDs: in.SourceVersionIDs})
			if err != nil {
				return agentcapability.Result{}, err
			}
			command := r
			command.RequestID = key
			result, err := bound.Options.ToolCapabilities[0].Bindings[0].Call(ctx, command)
			if err != nil {
				return agentcapability.Result{}, err
			}
			coreReceipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
			if err := s.config.References.CompleteOperation(ctx, key+"/input", coreReceipt.LibraryID, coreReceipt.ResourceID, coreReceipt.JobID); err != nil {
				return agentcapability.Result{}, err
			}
			return agentcapability.Result{CommandID: r.RequestID, Data: result.Data}, nil
		}
		return agentcapability.Result{}, status_error.KnowledgeForbidden
	}}
}
