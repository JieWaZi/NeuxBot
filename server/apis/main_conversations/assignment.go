package main_conversations

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// scope 在预留来源前规范化目录；实际项目归属及目录授权由执行入口核验。
func (body *MainConversationInput) scope() (string, error) {
	body.ProjectID = strings.TrimSpace(body.ProjectID)
	body.WorkingDirectory = strings.TrimSpace(body.WorkingDirectory)
	if body.ReplyToMessageID != "" && (body.ProjectID != "" || body.WorkingDirectory != "") {
		return "", status_error.BadRequest
	}
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

// freezeSource 对同一交办使用一次授权快照，同时确定工作 Chat 的规格与复用范围。
func (body MainConversationInput) freezeSource(ctx context.Context, actor, scope string) (*execution.CreateChatSpec, string, error) {
	source := &execution.CreateChatSpec{WakerID: actor, ProjectID: body.ProjectID}
	if body.ProjectID != "" {
		project, err := execution.CaptureMainProjectScope(ctx, global.ProjectControllerManager, global.TransactionControllerManager, actor, body.ProjectID)
		if err != nil {
			return nil, "", err
		}
		source.ProjectScope = project
		scope += "@" + project.ScopeKey()
	}
	if body.WorkingDirectory != "" {
		directory, err := projectsource.Resolve(body.WorkingDirectory)
		if err != nil {
			return nil, "", status_error.ProjectSourceUnavailable
		}
		source.WorkingDirectory = directory.Path
		scope = "directory:" + directory.Path
	}
	return source, scope, nil
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
	if kind != "team" {
		return admit
	}
	return func(ctx context.Context, commit func(context.Context) error) error {
		locked, release, err := lockMainConversationTeam(ctx, kind, id)
		if err != nil {
			return err
		}
		defer release()
		if newAssignment {
			team, err := global.ChannelControllerManager.Get(locked, id)
			if err != nil {
				return err
			}
			if team.LeadWakerID != work.WakerID {
				return status_error.ChatReplyTargetUnavailable
			}
		}
		chat := models.Chat{ID: work.WorkChatID, WakerID: work.WakerID, HiddenFromWakerHistory: true}
		if err := global.MainConversationService.AuthorizeWorkActor(locked, chat); err != nil {
			if util.HasStatusCode(err, status_error.AgentOperationForbidden) {
				return status_error.ChatReplyTargetUnavailable
			}
			return err
		}
		return admit.Commit(locked, commit)
	}
}
