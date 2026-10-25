package im

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// DeliveryRequest 由 IM 回复、普通伙伴和工作流共用，来源范围由调用入口签发。
type DeliveryRequest struct {
	// VerifyFileContent 要求 CLI 重入时对本次文件字节重新冻结并比较，不由模型设置。
	VerifyFileContent bool
	// RequestID 是入口范围内稳定复用的发送请求身份。
	RequestID string
	// TargetID 是管理员批准的真实投递目标。
	TargetID string
	// Content 是待冻结发送的正文。
	Content string
	// Files 是所属工作范围内的原始文件路径。
	Files []string
	// AllowedRoots 是当前真实执行或运行冻结的授权目录，不接受模型提供。
	AllowedRoots []string
	// LinkID 是原始 IM 对话关系，主动发送时为空。
	LinkID string
	// ChatID 是产生结果的标准聊天。
	ChatID string
	// ExecutionID 是产生结果的实际回合。
	ExecutionID string
	// WorkflowRunID 是产生结果的工作流运行。
	WorkflowRunID string
	// WorkflowDefinitionID 是工作流运行所属的实际定义。
	WorkflowDefinitionID string
}

// CreateDelivery 冻结已批准目标、正文和文件，以独立 Outbox 记录交付。
func (s *Service) CreateDelivery(ctx context.Context, request DeliveryRequest) (models.IMDelivery, error) {
	s.deliveryGate.Lock()
	defer s.deliveryGate.Unlock()
	request.Content = strings.TrimSpace(request.Content)
	if request.RequestID == "" || request.TargetID == "" || (request.Content == "" && len(request.Files) == 0) || len(request.Content) > 200000 || len(request.Files) > 10 {
		return models.IMDelivery{}, status_error.IMConfigurationInvalid
	}
	var frozen []models.IMFile
	snapshotID := ""
	keepSnapshot := false
	defer func() {
		if snapshotID != "" && !keepSnapshot {
			_ = os.RemoveAll(filepath.Join(s.config.DataDirectory, "im", "deliveries", snapshotID))
		}
	}()
	digests := []string{}
	if request.VerifyFileContent {
		snapshotID = util.New()
		for index, path := range request.Files {
			file, err := protocol.FreezeFile(s.config.DataDirectory, snapshotID, index, path, request.AllowedRoots)
			if err != nil {
				return models.IMDelivery{}, err
			}
			frozen = append(frozen, file)
			digests = append(digests, file.Digest)
		}
	}
	payload := []any{request.TargetID, request.Content, request.Files, request.LinkID, request.ChatID, request.ExecutionID, request.WorkflowRunID, request.WorkflowDefinitionID}
	if request.VerifyFileContent {
		payload = append(payload, digests)
	}
	identity, err := requestidempotency.New(protocol.StableID(request.RequestID), payload)
	if err != nil {
		return models.IMDelivery{}, err
	}
	previous, exists, err := s.config.Store.FindDelivery(ctx, request.RequestID)
	if err != nil {
		return previous, err
	}
	if exists {
		if previous.RequestHash != identity.Hash {
			return previous, status_error.IMConfigurationConflict
		}
		return previous, nil
	}
	binding, err := s.config.Store.Binding(ctx, request.TargetID)
	if err != nil {
		return models.IMDelivery{}, err
	}
	if binding.State != constants.IMBindingStateApproved {
		return models.IMDelivery{}, status_error.IMChatNotAuthorized
	}
	if request.WorkflowRunID != "" && !binding.AllowWorkflowDelivery {
		return models.IMDelivery{}, status_error.IMWorkflowDeliveryNotAllowed
	}
	connection, err := s.config.Store.Connection(ctx, binding.ConnectionID)
	if err != nil {
		return models.IMDelivery{}, err
	}
	if !connection.Enabled {
		return models.IMDelivery{}, status_error.IMConfigurationInvalid
	}
	delivery := models.IMDelivery{ID: protocol.StableID("delivery", request.RequestID), RequestID: request.RequestID, RequestHash: identity.Hash, BindingID: binding.ID, ConnectionID: binding.ConnectionID, ExternalChatID: binding.ExternalChatID, LinkID: request.LinkID, ChatID: request.ChatID, ExecutionID: request.ExecutionID, WorkflowRunID: request.WorkflowRunID, WorkflowDefinitionID: request.WorkflowDefinitionID, Content: request.Content, State: constants.IMDeliveryStatePending, Files: []models.IMFile{}, Receipts: []string{}}
	if request.VerifyFileContent {
		delivery.Files = frozen
	} else {
		for index, path := range request.Files {
			file, err := protocol.FreezeFile(s.config.DataDirectory, delivery.ID, index, path, request.AllowedRoots)
			if err != nil {
				return models.IMDelivery{}, err
			}
			delivery.Files = append(delivery.Files, file)
		}
	}

	if err := s.config.Store.CreateDelivery(ctx, delivery); err != nil {
		if hasCode(err, status_error.IMConfigurationConflict) {
			previous, exists, lookupErr := s.config.Store.FindDelivery(ctx, request.RequestID)
			if lookupErr != nil {
				return delivery, lookupErr
			}
			if exists && previous.RequestHash == identity.Hash {
				return previous, nil
			}
		}
		return delivery, err
	}
	keepSnapshot = true
	return delivery, nil
}

// RetryDelivery 只重新投递已有冻结内容；不确定结果必须由用户明确核对。
func (s *Service) RetryDelivery(ctx context.Context, id string, confirmedUnknown bool) error {
	s.life.Lock()
	defer s.life.Unlock()
	if s.delivering[id] {
		return status_error.IMConfigurationConflict
	}
	delivery, err := s.config.Store.Delivery(ctx, id)
	if err != nil {
		return err
	}
	if delivery.State == constants.IMDeliveryStateUnknown && !confirmedUnknown {
		return status_error.IMDeliveryRetryUnsafe
	}
	if delivery.State != constants.IMDeliveryStateFailed && delivery.State != constants.IMDeliveryStateUnknown {
		return status_error.IMConfigurationConflict
	}
	delivery.State = constants.IMDeliveryStatePending
	delivery.ErrorCode = ""
	return s.config.Store.UpdateDelivery(ctx, delivery)
}

func (s *Service) startDelivery(delivery models.IMDelivery) {
	s.life.Lock()
	if s.ctx.Err() != nil || s.delivering[delivery.ID] || s.delivering["target:"+delivery.BindingID] {
		s.life.Unlock()
		return
	}
	s.delivering[delivery.ID] = true
	s.delivering["target:"+delivery.BindingID] = true
	s.workers.Add(1)
	s.life.Unlock()
	go func() {
		defer s.workers.Done()
		defer func() {
			s.life.Lock()
			delete(s.delivering, delivery.ID)
			delete(s.delivering, "target:"+delivery.BindingID)
			s.life.Unlock()
		}()
		if err := s.sendDelivery(s.ctx, delivery); err != nil {
			logx.Errorf("im delivery processing failed: delivery_id=%s error=%v", delivery.ID, err)
		}
	}()
}

func (s *Service) sendDelivery(ctx context.Context, delivery models.IMDelivery) error {
	binding, err := s.config.Store.Binding(ctx, delivery.BindingID)
	if err != nil {
		return err
	}
	connection, err := s.config.Store.Connection(ctx, delivery.ConnectionID)
	if err != nil {
		return err
	}
	if binding.State != constants.IMBindingStateApproved {
		delivery.State = constants.IMDeliveryStateFailed
		delivery.ErrorCode = status_error.IMChatNotAuthorized.Key()
		return s.config.Store.UpdateDelivery(ctx, delivery)
	}
	if !connection.Enabled {
		if delivery.WorkflowRunID == "" {
			return nil
		}
		delivery.State = constants.IMDeliveryStateFailed
		delivery.ErrorCode = status_error.IMConnectionDisabled.Key()
		return s.config.Store.UpdateDelivery(ctx, delivery)
	}
	if delivery.WorkflowRunID != "" && !binding.AllowWorkflowDelivery {
		delivery.State = constants.IMDeliveryStateFailed
		delivery.ErrorCode = status_error.IMWorkflowDeliveryNotAllowed.Key()
		return s.config.Store.UpdateDelivery(ctx, delivery)
	}
	platform := s.config.Platforms[connection.Platform]
	if readiness, ok := platform.(protocol.ConnectionReadiness); ok && !readiness.ConnectionReady(connection.ID) {
		// 沿用已有维护节拍等待认证或重连，不领取、不计尝试，也不增加等待协程。
		return nil
	}
	claimed, err := s.config.Store.ClaimDelivery(ctx, delivery.ID)
	if err != nil || !claimed {
		return err
	}
	delivery.State = constants.IMDeliveryStateSending
	delivery.Attempts++
	if err := s.config.Store.UpdateDelivery(ctx, delivery); err != nil {
		return err
	}
	// 接收方由登记时冻结，之后修改绑定不能改变既有投递目的地。
	binding.ExternalChatID = delivery.ExternalChatID
	if platform == nil {
		delivery.State = constants.IMDeliveryStateFailed
		delivery.ErrorCode = status_error.IMConfigurationInvalid.Key()
		return s.commitDeliveryProgress(ctx, delivery)
	}
	textLimit := 1800
	if connection.Platform == constants.IMPlatformWecom {
		// 主动 Markdown 上限按 UTF-8 字节计，最多四字节字符也不超过 4096。
		textLimit = 1000
	}
	texts := protocol.SplitText(delivery.Content, textLimit)
	total := len(texts) + len(delivery.Files)
	for delivery.NextPart < total {
		if delivery.WorkflowRunID != "" {
			if err := s.ValidateWorkflowTarget(ctx, delivery.BindingID); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				delivery.State = constants.IMDeliveryStateFailed
				delivery.ErrorCode = errorCode(err)
				return s.commitDeliveryProgress(ctx, delivery)
			}
		}
		index := delivery.NextPart
		text := ""
		var file *models.IMFile
		if index < len(texts) {
			text = texts[index]
		} else {
			file = &delivery.Files[index-len(texts)]
		}
		receipt, definitive, sendErr := platform.Send(ctx, connection, binding, partIdentity(delivery.ID, index), text, file)
		if sendErr != nil {
			delivery.ErrorCode = errorCode(sendErr)
			if connection.Platform == constants.IMPlatformWecom && definitive && hasCode(sendErr, status_error.IMServiceUnavailable) {
				// 就绪检查后连接也可能退出；仅尚未写出消息的失活会话允许继续排队。
				delivery.State = constants.IMDeliveryStatePending
				return s.commitDeliveryProgress(ctx, delivery)
			}
			delivery.State = constants.IMDeliveryStateUnknown
			if definitive {
				delivery.State = constants.IMDeliveryStateFailed
			}
			return s.commitDeliveryProgress(ctx, delivery)
		}
		delivery.Receipts = append(delivery.Receipts, receipt)
		delivery.NextPart++
		if err := s.commitDeliveryProgress(ctx, delivery); err != nil {
			return err
		}
	}
	delivery.State = constants.IMDeliveryStateSent
	delivery.ErrorCode = ""
	return s.commitDeliveryProgress(ctx, delivery)
}

// DeliverySummary 只把发送状态交给模型，不回传平台身份、认证或本机暂存路径。
func DeliverySummary(delivery models.IMDelivery) map[string]any {
	return map[string]any{"deliveryId": delivery.ID, "state": delivery.State, "errorCode": delivery.ErrorCode, "partsSent": delivery.NextPart, "createdAt": delivery.CreatedAt}
}

// commitDeliveryProgress 保存已确认片段边界，取消等待不能丢失发送收据。
func (s *Service) commitDeliveryProgress(ctx context.Context, delivery models.IMDelivery) error {
	return s.config.Store.UpdateDelivery(context.WithoutCancel(ctx), delivery)
}
