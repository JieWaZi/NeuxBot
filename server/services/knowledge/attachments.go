package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"io"
	"os"
	"path/filepath"
)

// importSelectedAttachments 只接受当前真实Execution用户消息中的受管附件序号。
func (s *Service) importSelectedAttachments(ctx context.Context, chatID, library, request string, indexes []int, validate func(context.Context) error, guards ...func(context.Context) (func(), error)) ([]string, error) {
	summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ActiveExecutionID == "" {
		return nil, status_error.KnowledgeForbidden
	}
	active, err := s.config.Chats.ReadExecution(ctx, chatID, summary.Chat.ActiveExecutionID)
	if err != nil {
		return nil, err
	}
	messages, err := s.config.Chats.ReadMessageHistory(ctx, chatID, 0, 100)
	if err != nil {
		return nil, err
	}
	for _, message := range messages.Resources {
		if message.ID != active.UserMessageID {
			continue
		}
		root, err := os.OpenRoot(summary.Chat.WorkspaceDirectory)
		if err != nil {
			return nil, status_error.KnowledgeStorageError
		}
		defer root.Close()
		ids := []string{}
		for ordinal, index := range indexes {
			if index < 0 || index >= len(message.Attachments) {
				return nil, status_error.KnowledgeForbidden
			}
			attachment := message.Attachments[index]
			if !attachment.Managed || !filepath.IsLocal(attachment.Path) {
				return nil, status_error.KnowledgeForbidden
			}
			file, err := root.Open(attachment.Path)
			if err != nil {
				return nil, status_error.KnowledgeStorageError
			}
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
				file.Close()
				return nil, status_error.KnowledgeInvalid
			}
			raw, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
			file.Close()
			if err != nil || len(raw) > 32<<20 {
				return nil, status_error.KnowledgeStorageError
			}
			id := request + "/attachment/" + fmt.Sprint(ordinal)
			sum := sha256.Sum256(raw)
			link := models.KnowledgeOperationLink{RequestID: id, Operation: "source.import", Digest: hex.EncodeToString(sum[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: library, ChatID: chatID, ExecutionID: active.ID}
			if err := s.config.References.BeginOperation(ctx, link); err != nil {
				return nil, err
			}
			release := func() {}
			guarded := len(guards) > 0
			if guarded {
				release, err = guards[0](ctx)
				if err != nil {
					return nil, err
				}
			} else {
				s.policy.RLock()
				release = s.policy.RUnlock
			}
			if err := validate(ctx); err != nil {
				release()
				return nil, err
			}
			receipt, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: id, LibraryID: library, Filename: attachment.Name, MIME: attachment.MediaType, Bytes: raw})
			release()
			if err != nil {
				return nil, bridge.MapError(err)
			}
			if err := s.config.References.CompleteOperation(ctx, id, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
				return nil, err
			}
			ids = append(ids, receipt.ResourceID)
		}
		return ids, nil
	}
	return nil, status_error.KnowledgeForbidden
}
