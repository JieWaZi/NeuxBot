package channel

import (
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// prepareTeamInputAttachments 把临时上传移交给将持久接纳的团队输入，保留已有本地路径。
func (s *service) prepareTeamInputAttachments(thread models.ChannelThread, inputID string, input *execution.WorkInput, claim func(*execution.WorkInput) (func(bool), error)) (func(bool), error) {
	if claim == nil || input == nil || len(input.Attachments) == 0 {
		return func(bool) {}, nil
	}
	original := input.Attachments
	input.Attachments = slices.Clone(original)
	release, err := claim(input)
	if err != nil {
		return nil, err
	}
	prepared, err := s.stageTeamInputAttachments(thread, inputID, original, input.Attachments)
	if err != nil {
		release(false)
		return nil, err
	}
	return func(committed bool) {
		if !committed && prepared.Rollback != nil {
			if err := prepared.Rollback(); err != nil {
				logx.Warnf("rollback team input attachments: thread_id=%s input_id=%s error=%v", thread.ID, inputID, err)
			}
		}
		release(committed)
	}, nil
}

func (s *service) stageTeamInputAttachments(thread models.ChannelThread, inputID string, original, resolved []execution.AttachmentInput) (workspace.PreparedAttachments, error) {
	var files []workspace.AttachmentInput
	var positions []int
	for index, source := range original {
		if uploads.IsReference(source.SourcePath) {
			positions = append(positions, index)
			files = append(files, workspace.AttachmentInput{SourcePath: resolved[index].SourcePath})
		}
	}
	if len(files) == 0 {
		return workspace.PreparedAttachments{}, nil
	}
	layout, err := chatstorage.NewLayout(s.managedFilesDirectory)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	directory, err := layout.TeamRoot(thread.ID)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	owner, err := workspace.ReadExecutionWorkspaceOwner(directory)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != thread.ID {
		return workspace.PreparedAttachments{}, status_error.ChatDirectoryOwnershipInvalid
	}
	if err := workspace.RemoveQueuedAttachments(directory, thread.LeadChatID, inputID); err != nil {
		return workspace.PreparedAttachments{}, err
	}
	prepared, err := workspace.StageQueuedAttachments(directory, thread.LeadChatID, inputID, files)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	for index, position := range positions {
		resolved[position].SourcePath = prepared.RuntimePaths[index]
	}
	return prepared, nil
}

// releaseTeamInputAttachments 回收来源输入的副本；Chat 已接纳的附件由该 Chat 保存。
func (s *service) releaseTeamInputAttachments(thread models.ChannelThread, inputID string) {
	if s.managedFilesDirectory == "" {
		return
	}
	layout, err := chatstorage.NewLayout(s.managedFilesDirectory)
	if err == nil {
		var directory string
		directory, err = layout.TeamRoot(thread.ID)
		if err == nil {
			err = workspace.RemoveQueuedAttachments(directory, thread.LeadChatID, inputID)
		}
	}
	if err != nil {
		logx.Warnf("release team input attachments: thread_id=%s input_id=%s error=%v", thread.ID, inputID, err)
	}
}
