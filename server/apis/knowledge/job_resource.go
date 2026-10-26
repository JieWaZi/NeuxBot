package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

// 知识任务
type Job struct {
	// Wiki维护
	Maintenance bool `json:"maintenance"`
	// 本次接纳时间
	AttemptStartedAt int64 `json:"attemptStartedAt,omitempty"`

	// 任务说明
	Diagnostic string `json:"diagnostic"`
	// 宿主状态
	HostState string `json:"hostState"`
	// 宿主错误码
	HostErrorCode string `json:"hostErrorCode"`
	// 暂停说明
	HostDiagnostic string `json:"hostDiagnostic"`

	// 任务标识
	ID string `json:"id"`
	// 知识库标识
	LibraryID string `json:"libraryId"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 任务状态
	State string `json:"state"`
	// 尝试序号
	Attempt int `json:"attempt"`
	// 状态版本
	Version int64 `json:"version"`
	// 来源版本
	SourceVersionIDs []string `json:"sourceVersionIds"`
	// 取消屏障
	CancelRequested bool `json:"cancelRequested"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

func jobDTO(v *core.Job) Job {
	return Job{Maintenance: core.IsMaintenanceJob(v), AttemptStartedAt: v.AttemptStartedAt, Diagnostic: v.Diagnostic, ID: v.ID, LibraryID: v.LibraryID, RequestID: v.RequestID, State: v.State, Attempt: v.Attempt, Version: v.Version, SourceVersionIDs: v.SourceVersionIDs, CancelRequested: v.CancelRequested, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func jobHostDTO(ctx context.Context, v *core.Job) (Job, error) {
	out := jobDTO(v)
	if v.State != "queued" && v.State != "processing" && v.State != "cancel_requested" {
		out.HostState = "idle"
		return out, nil
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, v.RequestID)
	if errors.Is(err, status_error.KnowledgeNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.HostState = link.HostState
	out.HostErrorCode = link.HostErrorCode
	out.HostDiagnostic = link.HostDiagnostic
	return out, nil
}

// 知识任务模型选择
type TaskConfiguration struct {
	// 配置标识
	ID string `json:"id"`
	// 智能体
	RuntimeID string `json:"runtimeId"`
	// 账号标识
	AccountID string `json:"accountId"`
	// 模型
	Model string `json:"model"`
	// 推理强度
	Reasoning string `json:"reasoning"`
}

func taskConfigurationDTO(value models.KnowledgeTaskConfiguration) TaskConfiguration {
	return TaskConfiguration{ID: value.ID, RuntimeID: value.RuntimeID, AccountID: value.AccountID, Model: value.Model, Reasoning: value.Reasoning}
}
