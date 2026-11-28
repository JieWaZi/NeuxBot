package mainconversation

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (q *InputQueue) stageAttachments(input *models.MainConversationInput, claim func(*workinput.Input) (func(bool), error)) (func(bool, bool), error) {
	if len(input.Input.Attachments) == 0 {
		return func(bool, bool) {}, nil
	}
	if q.options.Layout == nil {
		return nil, status_error.ManagedWorkspaceError
	}
	input.Input.Attachments = slices.Clone(input.Input.Attachments)
	unclaim := func(bool) {}
	if claim != nil {
		var err error
		unclaim, err = claim(&input.Input)
		if err != nil {
			return nil, err
		}
	}
	committed := false
	defer func() {
		if !committed {
			unclaim(false)
		}
	}()
	root, err := q.attachmentRoot(input.WorkChatID)
	if err != nil {
		return nil, err
	}
	if err := workspace.RemoveQueuedAttachments(root, input.WorkChatID, input.PublicMessageID); err != nil {
		return nil, err
	}
	files := make([]workspace.AttachmentInput, 0, len(input.Input.Attachments))
	for _, item := range input.Input.Attachments {
		files = append(files, workspace.AttachmentInput{SourcePath: item.SourcePath})
	}
	prepared, err := workspace.StageQueuedAttachments(root, input.WorkChatID, input.PublicMessageID, files)
	if err != nil {
		return nil, err
	}
	for i, path := range prepared.RuntimePaths {
		input.Input.Attachments[i].SourcePath = path
	}
	committed = true
	return func(committed, keepStaged bool) {
		if !keepStaged {
			if err := prepared.Rollback(); err != nil {
				logx.Warnf("rollback main input attachments: chat_id=%s input_id=%s error=%v", input.WorkChatID, input.PublicMessageID, err)
			}
		}
		unclaim(committed)
	}, nil
}

func (q *InputQueue) releaseAttachments(input models.MainConversationInput) {
	if q.options.Layout == nil || len(input.Input.Attachments) == 0 {
		return
	}
	root, err := q.options.Layout.ChatRoot(input.WorkChatID)
	if err == nil {
		err = workspace.RemoveQueuedAttachments(root, input.WorkChatID, input.PublicMessageID)
	}
	if err != nil {
		logx.Warnf("release main input attachments: chat_id=%s input_id=%s error=%v", input.WorkChatID, input.PublicMessageID, err)
	}
}

// recoverAttachments removes interrupted staging and copies whose durable receipt was already settled.
func (q *InputQueue) recoverAttachments(ctx context.Context) error {
	if q.options.Layout == nil {
		return nil
	}
	chats, err := q.options.Layout.ChatsWithQueuedAttachments()
	if err != nil {
		return err
	}
	for _, chatID := range chats {
		root, err := q.attachmentRoot(chatID)
		if err != nil {
			return err
		}
		ids, err := workspace.RecoverQueuedAttachmentIDs(root)
		if err != nil {
			return err
		}
		for _, id := range ids {
			active, err := q.store.HoldsAttachments(ctx, chatID, id)
			if err != nil {
				return err
			}
			if !active {
				if err := workspace.RemoveQueuedAttachments(root, chatID, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (q *InputQueue) attachmentRoot(chatID string) (string, error) {
	root, err := q.options.Layout.ChatRoot(chatID)
	if err != nil {
		return "", err
	}
	stat, err := os.Lstat(root)
	if err != nil || !stat.IsDir() {
		return "", status_error.ChatDirectoryOwnershipInvalid
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return "", status_error.ChatDirectoryOwnershipInvalid
	}
	return root, nil
}
