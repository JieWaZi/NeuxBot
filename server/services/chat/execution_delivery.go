package chat

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

// ExecutionDeliveryFile 是来源领域验证权限后交给 Chat 复制的正式产出。
type ExecutionDeliveryFile struct {
	// ID 是来源运行、文件和交付目标共同派生的稳定身份。
	ID string
	// Name 是读取冻结结果时得到的文件名。
	Name string
	// Partial 表示仅交付已确认的节点成果。
	Partial bool
	// Open 从来源领域打开已经验证的明确文件，不接受本机任意路径。
	Open func() (io.ReadCloser, error)
	// HandleSourceError 在原文件无法打开或验证时交还来源错误，返回该错误是否已由来源处理。
	HandleSourceError func(error) bool
}

// ExecutionDelivery 是向既有答复追加的受管文件或依据原结果补写的摘要。
type ExecutionDelivery struct {
	// ChatID 是原执行所属会话。
	ChatID string
	// ExecutionID 也是本轮 Assistant 公开回复标识。
	ExecutionID string
	// SourceID 是来源领域确认的原事实身份。
	SourceID string
	// Files 是允许交付的正式文件。
	Files []ExecutionDeliveryFile
	// Summary 是确定性恢复或独立工具禁用执行得到的正文。
	Summary string
	// SummaryFallback 区分已交付的确切结果与已补写的自然语言总结。
	SummaryFallback bool
}

// stagedExecutionDeliveryFile 保存锁外复制的结果，锁内按最新回执重新确认是否需要提交。
type stagedExecutionDeliveryFile struct {
	// path 是原执行中用于识别已交付附件的稳定路径。
	path string
	// partial 标记原领域确认的部分成果。
	partial bool
	// attachment 是准备发布的附件，复制失败时为空。
	attachment *workspace.StagedResultAttachment
	// err 保存路径无效或复制失败的原因。
	err error
	// handleSourceError 仅处理本次确切尝试中来自原件的错误。
	handleSourceError func(error) bool
}

// PublishExecutionDelivery 将交付追加至已接纳的原答复，重复请求保留同一附件身份。
func (m *service) PublishExecutionDelivery(ctx context.Context, delivery ExecutionDelivery) error {
	// 复制和临时文件清理期间保护受管目录，后续轮次仍可接纳。
	releaseReference, err := chatstorage.AcquireReferences(m.storage.Root(), delivery.ChatID)
	if err != nil {
		return err
	}
	defer releaseReference()
	records, err := m.ReadExecutionRecords(delivery.ChatID, delivery.ExecutionID)
	if err != nil {
		return err
	}
	var created eventlog.ChatCreatedData
	if err := json.Unmarshal(records[0].Data, &created); err != nil {
		logx.Errorf("decode chat delivery owner failed: chat_id=%s execution_id=%s error=%v", delivery.ChatID, delivery.ExecutionID, err)
		return status_error.ChatLogCorrupted
	}
	if records[0].Type != constants.ChatEventTypeCreated || created.WakerID == "" {
		return status_error.ChatLogCorrupted
	}
	directory, err := m.storage.ChatRoot(delivery.ChatID)
	if err != nil {
		return err
	}
	seen, _, err := committedExecutionDelivery(records, delivery)
	if err != nil {
		return err
	}
	staged := make([]stagedExecutionDeliveryFile, 0, len(delivery.Files))
	for _, file := range delivery.Files {
		path, err := workspace.ResultAttachmentPath(file.ID, file.Name)
		// 持久回执已经完成交付，用户可以随后删除对应的副本。
		if seen[path] {
			continue
		}
		candidate := stagedExecutionDeliveryFile{path: path, partial: file.Partial, err: err}
		if err == nil {
			open := func() (io.ReadCloser, error) {
				content, err := file.Open()
				if err != nil {
					candidate.handleSourceError = file.HandleSourceError
				}
				return content, err
			}
			candidate.attachment, candidate.err = workspace.StageResultAttachment(ctx, directory, delivery.ChatID, file.ID, file.Name, open)
			if candidate.attachment != nil {
				defer candidate.attachment.Discard()
			}
		}
		staged = append(staged, candidate)
	}

	// 与接纳和分叉互斥提交，但与保护临时目录的引用共存。
	release, err := chatstorage.AcquireAdmission(m.storage.Root(), created.WakerID, delivery.ChatID)
	if err != nil {
		return err
	}
	defer release()
	records, err = m.ReadExecutionRecords(delivery.ChatID, delivery.ExecutionID)
	if err != nil {
		return err
	}
	seen, summary, err := committedExecutionDelivery(records, delivery)
	if err != nil {
		return err
	}
	data := eventlog.ExecutionDeliveryData{SourceID: delivery.SourceID, Summary: summary, SummaryFallback: delivery.SummaryFallback}
	var copyError error
	for _, file := range staged {
		if seen[file.path] {
			continue
		}
		if file.handleSourceError != nil && file.handleSourceError(file.err) {
			continue
		}
		if file.err == nil {
			attachment, err := file.attachment.Publish()
			if err == nil {
				ref := eventlog.AttachmentReference(attachment)
				ref.Partial = file.partial
				data.Attachments = append(data.Attachments, ref)
				seen[file.path] = true
				continue
			}
			file.err = err
		}
		if copyError == nil {
			copyError = file.err
		}
	}
	if len(data.Attachments) > 0 || summary != "" {
		if _, err := m.AppendExecutionFacts(ctx, delivery.ChatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionDelivery, MessageID: delivery.ExecutionID, ExecutionID: delivery.ExecutionID, Data: marshalData(data)}}); err != nil {
			return err
		}
	}
	return copyError
}

// committedExecutionDelivery 从原答复回执提取已交付的附件及待提交的摘要。
func committedExecutionDelivery(records []eventlog.Record, delivery ExecutionDelivery) (map[string]bool, string, error) {
	seen := map[string]bool{}
	summary := strings.TrimSpace(delivery.Summary)
	for _, record := range records {
		if record.Type != constants.ChatEventTypeExecutionDelivery || record.MessageID != delivery.ExecutionID {
			continue
		}
		var data eventlog.ExecutionDeliveryData
		if err := json.Unmarshal(record.Data, &data); err != nil {
			logx.Errorf("decode committed chat delivery failed: chat_id=%s execution_id=%s sequence=%d error=%v", delivery.ChatID, delivery.ExecutionID, record.Sequence, err)
			return nil, "", status_error.ChatLogCorrupted
		}
		for _, attachment := range data.Attachments {
			seen[attachment.Path] = true
		}
		if data.SourceID == delivery.SourceID && data.Summary != "" {
			if data.Summary == summary && data.SummaryFallback == delivery.SummaryFallback || delivery.SummaryFallback && !data.SummaryFallback {
				summary = ""
			}
		}
	}
	return seen, summary, nil
}
