package main_conversations

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

type mainSubmissionKey struct {
	// kind 是主会话主体类型。
	kind string
	// subject 是主会话主体标识。
	subject string
	// submission 是调用方稳定重投的输入标识。
	submission string
}

var mainSubmissions = struct {
	// Mutex 保护当前同步接纳操作索引。
	sync.Mutex
	// active 仅保存正在处理的提交，关闭通道唤醒同键重投。
	active map[mainSubmissionKey]chan struct{}
}{active: make(map[mainSubmissionKey]chan struct{})}

// acquireMainSubmission 串行处理同一发送的重投，重投读取原路由接纳回执。
func acquireMainSubmission(ctx context.Context, key mainSubmissionKey) (func(), error) {
	for ctx.Err() == nil {
		mainSubmissions.Lock()
		pending := mainSubmissions.active[key]
		if pending == nil {
			pending = make(chan struct{})
			mainSubmissions.active[key] = pending
			mainSubmissions.Unlock()
			return func() {
				mainSubmissions.Lock()
				defer mainSubmissions.Unlock()
				delete(mainSubmissions.active, key)
				close(pending)
			}, nil
		}
		mainSubmissions.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
		}
	}
	return nil, status_error.OperationCanceled
}

// scope 在预留来源前规范化目录；实际项目归属及目录授权由执行入口核验。
func (body *MainConversationInput) scope() (string, error) {
	body.ProjectID = strings.TrimSpace(body.ProjectID)
	body.WorkingDirectory = strings.TrimSpace(body.WorkingDirectory)
	if body.ReplyToMessageID != "" && (body.ProjectID != "" || body.WorkingDirectory != "") {
		return "", status_error.BadRequest
	}
	return body.scopeForAssignment()
}

// scopeForAssignment 规范化已确定的范围，包含对此前无来源澄清的首次派工。
func (body *MainConversationInput) scopeForAssignment() (string, error) {
	if body.WorkingDirectory != "" {
		if body.ProjectID != "" || !filepath.IsAbs(body.WorkingDirectory) {
			return "", status_error.BadRequest
		}
		body.WorkingDirectory = filepath.Clean(body.WorkingDirectory)
		return "directory:" + body.WorkingDirectory, nil
	}
	if body.ProjectID != "" {
		return "project:" + body.ProjectID, nil
	}
	return "managed", nil
}

// actorOptions 从本机已保存的伙伴或团队配置确定本次实际使用的 CLI。
func (op *SendMainConversationMessage) actorOptions(ctx context.Context, sourceWakerID string, selected *execution.WorkOptions) (string, execution.WorkOptions, error) {
	var options execution.WorkOptions
	if op.SubjectType == "team" {
		team, err := global.ChannelControllerManager.Get(ctx, op.SubjectID)
		if err != nil {
			return "", options, err
		}
		members, err := global.ChannelControllerManager.ListMembers(ctx, team.ID)
		if err != nil {
			return "", options, err
		}
		if sourceWakerID == "" {
			sourceWakerID = team.LeadWakerID
		}
		for _, m := range members {
			if m.WakerID == sourceWakerID {
				return m.WakerID, execution.WorkOptions{RuntimeID: m.RuntimeID, AccountID: m.AccountID, Model: m.Model, Reasoning: m.Reasoning, Fast: m.Fast, PermissionLevel: m.PermissionLevel}, nil
			}
		}
		return "", options, status_error.ChatReplyTargetUnavailable
	}
	if selected != nil {
		return op.SubjectID, *selected, nil
	}
	waker, err := global.WakerControllerManager.Get(ctx, op.SubjectID)
	if err != nil {
		return "", options, err
	}
	if m := waker.DefaultModel; m != nil {
		options.RuntimeID, options.AccountID, options.Model, options.Reasoning = m.RuntimeID, m.AccountID, m.Model, m.Reasoning
	}
	environments, err := global.RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return "", options, err
	}
	for _, env := range environments {
		if env.Enabled && (options.RuntimeID == env.RuntimeID || options.RuntimeID == "" && env.IsDefault) {
			options.RuntimeID, options.PermissionLevel = env.RuntimeID, env.DefaultPermissionLevel
			return waker.ID, options, nil
		}
	}
	return "", options, status_error.RuntimeUnavailable
}

// lockMainConversationTeam 保留团队引用、成员和待答请求的短同步边界。
func lockMainConversationTeam(ctx context.Context, kind, id string) (context.Context, func(), error) {
	if kind == "team" {
		return teamwork.Acquire(ctx, id)
	}
	return ctx, func() {}, nil
}

// mainConversationAdmission 在耐久接纳前复核原执行者资格，并由团队操作门排除并发成员调整。
func mainConversationAdmission(kind, id string, work models.MainConversationWork, newAssignment bool, admit execution.AdmissionBoundary) execution.AdmissionBoundary {
	return global.MainConversationService.WorkAdmission(kind, id, work, newAssignment, admit.Commit)
}
