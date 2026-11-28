package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// MainConversationEntryPoint 标识经主会话核对主体和来源后接纳的工作。
const MainConversationEntryPoint = "main-conversation"

// MainConversationSourceEntryPoint 标识用户在原工作记录内直接续聊。
const MainConversationSourceEntryPoint = "main-conversation-source"

// MainProjectScope 保存主会话交办实际授权的项目和有序目录配置。
type MainProjectScope struct {
	// Project 是与目录从同一事务读出的所属项目。
	Project models.Project
	// directories 保留目录顺序、默认入口及各目录的指令授权。
	directories []models.ProjectDirectory
}

// ScopeKey 标识有序目录、默认入口和指令策略，项目展示信息的修改不影响工作范围。
func (s *MainProjectScope) ScopeKey() string {
	digest := sha256.New()
	for _, dir := range s.directories {
		// 目录索引标识对应真实路径；带引号的边界与固定布尔值使相邻记录无歧义。
		fmt.Fprintf(digest, "%q:%t:%t\n", dir.Source.ID, dir.IsDefault, dir.LoadInstructions)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// ValidateAvailable 核验已选项目的全部冻结目录仍可访问，且有默认入口。
func (s *MainProjectScope) ValidateAvailable() error {
	defaultDirectory := false
	for _, directory := range s.directories {
		if !projectsource.Available(directory.Source) {
			return status_error.ProjectSourceUnavailable
		}
		defaultDirectory = defaultDirectory || directory.IsDefault
	}
	if !defaultDirectory {
		return status_error.ProjectSourceUnavailable
	}
	return nil
}

// CaptureMainProjectScope 在同一读事务内核对项目归属并冻结目录，供主会话选源和首次建源共用。
func CaptureMainProjectScope(ctx context.Context, projects controllers.ProjectController, tx controllers.TransactionController, wakerID, projectID string) (*MainProjectScope, error) {
	if tx == nil {
		return nil, status_error.ProjectServiceUnavailable
	}
	var scope *MainProjectScope
	err := tx.Transaction(ctx, func(ctx context.Context) error {
		var err error
		scope, err = readProjectScope(ctx, projects, wakerID, projectID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return scope, nil
}

// readProjectScope 按项目所有者读取目录关联；调用者决定是否随本次交办冻结事务快照。
func readProjectScope(ctx context.Context, projects controllers.ProjectController, wakerID, projectID string) (*MainProjectScope, error) {
	if projects == nil {
		return nil, status_error.ProjectServiceUnavailable
	}
	project, err := projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.WakerID != wakerID {
		return nil, status_error.ProjectNotFound.WithParams(map[string]interface{}{"ProjectID": projectID})
	}
	directories, err := projects.ListDirectories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &MainProjectScope{Project: project, directories: directories}, nil
}

// admissionWaker 确定接纳锁对应的伙伴：预留来源尚无工作日志，其余目标沿用已有 Chat 或首发规格。
func (m *service) admissionWaker(ctx context.Context, target ChatTarget) (string, error) {
	if source := target.MainConversationSource; source != nil {
		if target.ChatID == "" || target.Create != nil || source.WakerID == "" {
			return "", status_error.BadRequest
		}
		return source.WakerID, nil
	}
	if target.ChatID != "" {
		chat, err := m.journal.LoadExecutionChat(ctx, target.ChatID)
		return chat.WakerID, err
	}
	if target.Create != nil {
		return strings.TrimSpace(target.Create.WakerID), nil
	}
	return "", nil
}

// ensureMainConversationSource 在伙伴和 Chat 接纳锁内将宿主预留的来源写入原生工作日志。
func (m *service) ensureMainConversationSource(ctx context.Context, intent WorkIntent) error {
	scope := intent.Chat.MainConversationSource
	if scope == nil {
		return nil
	}
	id := intent.Chat.ChatID
	if scope.ProjectID != "" || scope.ProjectScope != nil {
		snapshot := scope.ProjectScope
		if snapshot == nil || snapshot.Project.ID != scope.ProjectID || snapshot.Project.WakerID != scope.WakerID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	}
	chat, err := m.journal.LoadExecutionChat(ctx, id)
	if err == nil {
		if !chat.HiddenFromWakerHistory || chat.WakerID != scope.WakerID || chat.ProjectID != scope.ProjectID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		return nil
	}
	if !util.HasStatusCode(err, status_error.ChatNotFound) {
		return err
	}
	if m.dormantChats == nil {
		return status_error.RuntimeUnavailable
	}
	state, err := m.resolveMessageChat(ctx, pendingMessage{
		createChatID: id,
		create:       &pendingChat{wakerID: scope.WakerID, projectID: scope.ProjectID, projectScope: scope.ProjectScope, workingDirectory: scope.WorkingDirectory, hiddenFromWakerHistory: true},
		runtime:      runtimeSelection{RuntimeID: intent.Options.RuntimeID},
		message:      userMessage{Text: intent.Input.Content},
	}, "")
	if err != nil {
		return err
	}
	_, err = m.dormantChats.CreateDormantChats(ctx, []chatservice.DormantChat{{Chat: state.chat, ProjectSources: state.projectSources}})
	return err
}
