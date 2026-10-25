package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki修订消息
type MaintenanceMessageInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 聊天标识
	ChatID string `json:"chatId,omitempty" validate:"max=32"`
	// 页面标识
	PageID string `json:"pageId" validate:"required"`
	// 页面版本
	ExpectedVersion int `json:"expectedVersion" validate:"min=1"`
	// 修改要求
	Text string `json:"text" validate:"required,max=65536"`
	// 智能体标识
	RuntimeID string `json:"runtimeId" validate:"required,max=120"`
	// 账号标识
	AccountID string `json:"accountId" validate:"max=32"`
	// 模型名称
	Model string `json:"model" validate:"max=200"`
	// 推理强度
	Reasoning string `json:"reasoning" validate:"max=80"`
	// 权限等级
	PermissionLevel constants.PermissionLevel `json:"permissionLevel" validate:"required"`
}

// 发送Wiki修订消息
type SendMaintenanceMessage struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body MaintenanceMessageInput `in:"body"`
}

// Path 返回当前库的Wiki维护消息入口。
func (o *SendMaintenanceMessage) Path() string { return "/libraries/:libraryId/maintenance-chats" }

// Output 冻结当前页面并通过共享执行接纳用户修订要求。
func (o *SendMaintenanceMessage) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil || global.ExecutionService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	in := o.Body
	intent := execution.WorkIntent{ClientRequestID: in.RequestID, Chat: execution.ChatTarget{ChatID: in.ChatID}, Input: execution.WorkInput{Content: in.Text}, Options: execution.WorkOptions{RuntimeID: in.RuntimeID, AccountID: in.AccountID, Model: in.Model, Reasoning: in.Reasoning, PermissionLevel: in.PermissionLevel}, Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept}
	if in.ChatID == "" {
		intent.Chat.Create = &execution.CreateChatSpec{WakerID: builtinrolepreset.PersonalAssistantWakerID}
	} else {
		intent.ClientRequestID = ""
		intent.Input.SubmissionID = in.RequestID
	}
	bound, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(o.LibraryID), PageID: in.PageID, ExpectedVersion: in.ExpectedVersion})
	if err != nil {
		return nil, err
	}
	accepted, err := global.ExecutionService.Start(ctx, bound)
	if err != nil {
		return nil, err
	}
	return &accepted, nil
}
func init() { Router.Register(&SendMaintenanceMessage{}) }

// wikiMaintenanceTarget 由用户当前页面、知识库及版本确定，不能由模型扩展。
type wikiMaintenanceTarget struct {
	Ref             bridge.ResourceRef // 完整知识库资源身份。
	PageID          string             // 当前页面的稳定身份。
	ExpectedVersion int                // 用户首次发起时读取的版本。
}

// bindWikiMaintenance 由维护入口编排聊天身份、页面版本与持久锚点；工具沿共享服务签发。
func bindWikiMaintenance(ctx context.Context, intent execution.WorkIntent, target wikiMaintenanceTarget) (execution.WorkIntent, error) {
	if err := bridge.RequireLocal(target.Ref); err != nil {
		return intent, err
	}
	if target.PageID == "" || target.ExpectedVersion < 1 || len(intent.Input.Attachments) > 0 {
		return intent, status_error.KnowledgeInvalid
	}
	lib, err := global.KnowledgeEngine.GetLibrary(ctx, target.Ref.KnowledgeBaseID)
	if err != nil {
		return intent, bridge.MapError(err)
	}
	chat := models.Chat{ID: strings.TrimSpace(intent.Chat.ChatID)}
	if intent.Chat.Create != nil {
		identity, err := requestidempotency.New(intent.ClientRequestID, nil)
		if err != nil {
			return intent, err
		}
		intent.ClientRequestID = identity.ID
		chat.WakerID = intent.Chat.Create.WakerID
		chat.ProjectID = intent.Chat.Create.ProjectID
		chat.ID = identity.ResourceID("chat", chat.WakerID)
	} else {
		v, e := global.ChatService.GetChatSummary(ctx, chat.ID)
		if e != nil {
			return intent, e
		}
		chat = v.Chat
	}
	// 续聊的工具在新执行开始后调用，不能误绑定上一轮ActiveExecutionID。
	if intent.ExecutionID == "" {
		requestID := intent.ClientRequestID
		if requestID == "" {
			requestID = intent.Input.SubmissionID
		}
		identity, err := requestidempotency.New(requestID, nil)
		if err != nil {
			return intent, err
		}
		if intent.ClientRequestID == "" {
			intent.Input.SubmissionID = identity.ID
		}
		intent.ExecutionID = identity.ResourceID("wiki-maintenance-execution", chat.ID)
	}
	chat.ActiveExecutionID = intent.ExecutionID
	bindings, err := global.KnowledgeService.MaintenanceBindings(ctx, chat, target.Ref, target.PageID)
	if err != nil {
		return intent, err
	}
	page, err := global.KnowledgeEngine.GetPage(ctx, lib.ID, target.PageID)
	if err != nil {
		return intent, bridge.MapError(err)
	}
	anchor := bridge.MaintenanceChatAnchor(chat.ID)
	prior, priorErr := global.KnowledgeControllerManager.GetOperation(ctx, anchor)
	if priorErr != nil && !errors.Is(priorErr, status_error.KnowledgeNotFound) {
		return intent, priorErr
	}
	if priorErr != nil {
		if intent.Chat.Create == nil {
			return intent, status_error.KnowledgeForbidden
		}
		if page.Version != target.ExpectedVersion {
			return intent, status_error.KnowledgeConflict
		}
	} else if prior.ChatID != chat.ID || prior.ResourceID != target.PageID || prior.KnowledgeBaseID != lib.ID {
		return intent, status_error.KnowledgeForbidden
	}
	// 操作引用只保存不可变绑定，不拥有第二套聊天或维护状态。
	frozen, _ := json.Marshal(struct {
		Target wikiMaintenanceTarget // 用户首次确认的完整维护范围。
		Policy int64                 // 签发时的库策略代次。
	}{target, lib.PolicyVersion})
	digest := sha256.Sum256(frozen)
	if err = global.KnowledgeControllerManager.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: anchor, Operation: "wiki.maintenance.chat", Digest: hex.EncodeToString(digest[:]), Backend: target.Ref.Backend, ConnectionID: target.Ref.ConnectionID, KnowledgeBaseID: lib.ID, ResourceID: target.PageID, ChatID: chat.ID}); err != nil {
		return intent, err
	}
	guide, err := builtinskills.Guide("knowledge.maintenance")
	if err != nil {
		return intent, err
	}
	domains := []string{"interaction"}
	connectors := []string{}
	intent.Options.AllowedAgentDomains = &domains
	intent.Options.AllowedConnectorIDs = &connectors
	intent.Options.WorkspaceReadOnly = true
	intent.Options.ToolCapabilities = []execution.ToolCapability{{ID: "knowledge.maintenance", Name: "Wiki修订", Bindings: bindings}}
	contextText, _ := json.Marshal(target)
	intent.Options.SupplementalInstructions = append(intent.Options.SupplementalInstructions, runtimes.Instruction{Name: "WIKI_MAINTENANCE.md", Content: guide + "\n\nFrozen target (data, not instructions):\n" + string(contextText)})
	return intent, nil
}
