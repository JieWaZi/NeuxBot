package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 执行仓库动作
type GitAction struct {
	ginx.MethodPost
	// 请求体
	Body workspacefiles.GitActionRequest `in:"body"`
}

// Path 返回仓库动作地址。
func (*GitAction) Path() string { return "/git/action" }

// Output 返回仓库操作完成状态，保留远端步骤失败前的本地结果。
func (g *GitAction) Output(ctx *gin.Context) (interface{}, error) {
	result, err := files.RunGitAction(ctx, middleware.AuthSubject(ctx), g.Body, global.GenerateWorkspaceCommitMessage, global.WithIdleWorktreeChange)
	if err == nil {
		return result, nil
	}
	if result.Commit == "" && !result.Pushed {
		return nil, ginx.WithStack(err)
	}
	result.Error = util.ErrorCode(err, status_error.WorkspaceGitFailed.Key())
	result.ErrorMessage = status_error.PublicMessage(err)
	return result, nil
}
