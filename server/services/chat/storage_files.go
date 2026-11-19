package chat

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// InheritedAttachmentPaths 只读取分叉发布时生成的继承前缀，复制后的附件仍用原上传目录。
func (m *service) InheritedAttachmentPaths(ctx context.Context, chatID string) (map[string]string, error) {
	created, err := m.logStore.ReadAfter(chatID, 0, 1)
	if err != nil {
		return nil, err
	}
	var scope eventlog.ChatCreatedData
	if len(created) != 1 || created[0].Type != constants.ChatEventTypeCreated || json.Unmarshal(created[0].Data, &scope) != nil {
		return nil, status_error.ChatLogCorrupted
	}
	if scope.ForkSourceChatID == "" {
		return nil, nil
	}
	paths := map[string]string{}
	for cursor := int64(1); ; {
		if err := ctx.Err(); err != nil {
			return nil, chatContextStatus(err)
		}
		batch, err := m.logStore.ReadAfter(chatID, cursor, 64)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return paths, nil
		}
		for _, record := range batch {
			if record.Type != constants.ChatEventTypeMessageInherited {
				return paths, nil
			}
			var message messageview.InheritedMessage
			if json.Unmarshal(record.Data, &message) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			for _, attachment := range message.Attachments {
				if attachment.Managed {
					paths[attachment.Path] = message.SourceMessageID
				}
			}
			cursor = record.Sequence
		}
	}
}

// CheckOccupiedWorkspace 使用执行进程的既有占用身份和权威冻结目录判断伙伴空间是否可修改。
func (m *service) CheckOccupiedWorkspace(ctx context.Context, wakerID string, occupied []string) error {
	root, err := m.storage.AgentWorkspace(wakerID)
	if err != nil {
		return err
	}
	for _, id := range occupied {
		if err := ctx.Err(); err != nil {
			return chatContextStatus(err)
		}
		created, err := m.logStore.ReadAfter(id, 0, 1)
		if hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
			// 已占用但冻结范围尚不可读时，不能证明该工作不使用伙伴空间。
			return status_error.WakerHasRunningWork
		}
		if err != nil {
			return err
		}
		var scope eventlog.ChatCreatedData
		if len(created) != 1 || created[0].Type != constants.ChatEventTypeCreated || json.Unmarshal(created[0].Data, &scope) != nil || !filepath.IsAbs(scope.WorkspaceDirectory) {
			return status_error.ChatLogCorrupted
		}
		if overlappingWorkspaces(root, scope.WorkspaceDirectory) {
			return status_error.WakerHasRunningWork
		}
		for _, directory := range scope.AdditionalWorkingDirectories {
			if overlappingWorkspaces(root, directory) {
				return status_error.WakerHasRunningWork
			}
		}
	}
	return nil
}

func overlappingWorkspaces(one, other string) bool {
	if !filepath.IsAbs(other) {
		return false
	}
	under, err := filepath.Rel(one, other)
	if err == nil && filepath.IsLocal(under) {
		return true
	}
	under, err = filepath.Rel(other, one)
	return err == nil && filepath.IsLocal(under)
}
