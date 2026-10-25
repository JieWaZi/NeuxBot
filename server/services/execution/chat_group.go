package execution

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// PrepareChatGroup 为团队首发批量建立共享 thread 工作区和一人一个的成员 Chat。
func (m *service) PrepareChatGroup(
	ctx context.Context,
	intent PrepareChatGroupIntent,
) (PreparedChatGroup, error) {
	intent.ThreadID = strings.TrimSpace(intent.ThreadID)
	intent.ProvisioningID = strings.TrimSpace(intent.ProvisioningID)
	intent.Scope.WakerID = strings.TrimSpace(intent.Scope.WakerID)
	intent.Scope.ProjectID = strings.TrimSpace(intent.Scope.ProjectID)
	intent.Scope.WorkingDirectory = strings.TrimSpace(intent.Scope.WorkingDirectory)
	title := defaultTitle(intent.Title)
	if m.dormantChats == nil || title == "" || len(intent.Members) == 0 ||
		intent.ThreadID == "" || intent.ProvisioningID == "" ||
		intent.Scope.WakerID == "" ||
		(intent.Scope.ProjectID != "" && intent.Scope.WorkingDirectory != "") {
		return PreparedChatGroup{}, status_error.BadRequest
	}
	seen := make(map[string]struct{}, len(intent.Members))
	seenChats := make(map[string]struct{}, len(intent.Members))
	leadRuntimeID := ""
	for index := range intent.Members {
		member := &intent.Members[index]
		member.ChatID = strings.TrimSpace(member.ChatID)
		member.WakerID = strings.TrimSpace(member.WakerID)
		member.RuntimeID = strings.TrimSpace(member.RuntimeID)
		member.AccountID = strings.TrimSpace(member.AccountID)
		member.Model = strings.TrimSpace(member.Model)
		member.Reasoning = strings.TrimSpace(member.Reasoning)
		if member.ChatID == "" || member.WakerID == "" || member.RuntimeID == "" ||
			!constants.SelectablePermissionLevel(member.PermissionLevel) {
			return PreparedChatGroup{}, status_error.BadRequest
		}
		if _, duplicated := seen[member.WakerID]; duplicated {
			return PreparedChatGroup{}, status_error.BadRequest
		}
		seen[member.WakerID] = struct{}{}
		if _, duplicated := seenChats[member.ChatID]; duplicated {
			return PreparedChatGroup{}, status_error.BadRequest
		}
		seenChats[member.ChatID] = struct{}{}
		if _, err := m.wakerController.Get(ctx, member.WakerID); err != nil {
			return PreparedChatGroup{}, err
		}
		if err := m.ValidateConfiguration(ctx, ExecutionConfiguration{
			RuntimeID:                  member.RuntimeID,
			AccountID:                  member.AccountID,
			Model:                      member.Model,
			Reasoning:                  member.Reasoning,
			PermissionLevel:            member.PermissionLevel,
			RequiredConnectorTransport: teamConnectorTransport(member.WakerID == intent.Scope.WakerID),
		}); err != nil {
			return PreparedChatGroup{}, err
		}
		if member.WakerID == intent.Scope.WakerID {
			leadRuntimeID = member.RuntimeID
		}
	}
	if leadRuntimeID == "" {
		return PreparedChatGroup{}, status_error.BadRequest
	}
	resolved, err := m.resolveNewChatWorkspace(ctx, pendingChat{
		wakerID:          intent.Scope.WakerID,
		projectID:        intent.Scope.ProjectID,
		workingDirectory: intent.Scope.WorkingDirectory,
	}, leadRuntimeID)
	if err != nil {
		return PreparedChatGroup{}, err
	}
	stagingLayout, err := m.storage.TeamProvisioningLayout(intent.ProvisioningID)
	if err != nil {
		return PreparedChatGroup{}, err
	}
	threadWorkspaceDirectory, err := stagingLayout.TeamWorkspace(intent.ThreadID)
	if err != nil {
		return PreparedChatGroup{}, err
	}
	threadProjectsDirectory, err := stagingLayout.TeamProjects(intent.ThreadID)
	if err != nil {
		return PreparedChatGroup{}, err
	}
	if err := workspace.EnsureThreadExecutionWorkspace(
		threadWorkspaceDirectory,
		intent.ThreadID,
		intent.Scope.WakerID,
		resolved.projectLinks,
	); err != nil {
		_ = m.storage.RollbackTeamProvisioning(intent.ProvisioningID, intent.ThreadID)
		return PreparedChatGroup{}, err
	}
	if len(resolved.additionalDirectories) == 0 {
		// 未选外部来源时，以本次团队会话的共享项目目录冻结临时工作范围。
		managedDirectory, pathErr := m.storage.TeamProjects(intent.ThreadID)
		if pathErr != nil {
			_ = m.storage.RollbackTeamProvisioning(intent.ProvisioningID, intent.ThreadID)
			return PreparedChatGroup{}, pathErr
		}
		resolved.additionalDirectories = []string{managedDirectory}
	}
	plannedChatIDs := make([]string, 0, len(intent.Members))
	for _, member := range intent.Members {
		plannedChatIDs = append(plannedChatIDs, member.ChatID)
	}
	rollbackThreadWorkspace := func() {
		if err := m.dormantChats.DeleteProvisionalTeam(
			ctx,
			intent.ThreadID,
			intent.ProvisioningID,
			plannedChatIDs,
		); err != nil {
			logx.Errorf(
				"rollback dormant team provisioning failed: provisioning_id=%s thread_id=%s error=%v",
				intent.ProvisioningID,
				intent.ThreadID,
				err,
			)
		}
	}
	dormant := make([]chatservice.DormantChat, 0, len(intent.Members))
	for _, member := range intent.Members {
		wakerID := member.WakerID
		chatID := member.ChatID
		stagedMemberWorkspaceDirectory, pathErr := stagingLayout.TeamMemberWorkspace(
			intent.ThreadID,
			wakerID,
			wakerID == intent.Scope.WakerID,
		)
		if pathErr != nil {
			rollbackThreadWorkspace()
			return PreparedChatGroup{}, pathErr
		}
		if workspaceErr := workspace.EnsureThreadMemberExecutionWorkspace(
			stagedMemberWorkspaceDirectory,
			intent.ThreadID,
			chatID,
			wakerID,
			threadProjectsDirectory,
		); workspaceErr != nil {
			rollbackThreadWorkspace()
			return PreparedChatGroup{}, workspaceErr
		}
		memberWorkspaceDirectory, pathErr := m.storage.TeamMemberWorkspace(
			intent.ThreadID,
			wakerID,
			wakerID == intent.Scope.WakerID,
		)
		if pathErr != nil {
			rollbackThreadWorkspace()
			return PreparedChatGroup{}, pathErr
		}
		chat := models.Chat{
			ID:                           chatID,
			WakerID:                      wakerID,
			ProjectID:                    resolved.projectID,
			ProjectName:                  resolved.projectName,
			WorkspaceDirectory:           memberWorkspaceDirectory,
			AdditionalWorkingDirectories: append([]string(nil), resolved.additionalDirectories...),
			Title:                        title,
			HiddenFromWakerHistory:       true,
		}
		dormant = append(dormant, chatservice.DormantChat{
			Chat:              chat,
			ProjectSources:    append([]eventlog.ProjectSourceSnapshot(nil), resolved.projectSources...),
			ThreadWorkspaceID: intent.ThreadID,
		})
	}
	if err := m.storage.PublishTeamProvisioning(intent.ProvisioningID, intent.ThreadID); err != nil {
		rollbackThreadWorkspace()
		return PreparedChatGroup{}, err
	}
	created, err := m.dormantChats.CreateDormantChats(ctx, dormant)
	if err != nil {
		rollbackThreadWorkspace()
		return PreparedChatGroup{}, err
	}
	return PreparedChatGroup{Chats: created}, nil
}

func teamConnectorTransport(lead bool) constants.RuntimeConnectorTransport {
	if !lead {
		return constants.RuntimeConnectorTransportUnspecified
	}
	return constants.RuntimeConnectorTransportStreamableHTTP
}
