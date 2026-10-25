package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 工作文件观察请求
type WatchRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `json:"relativePath"`
	// 授权根目录
	Root string `json:"root"`
	// 观察目录
	Directory bool `json:"directory"`
}

// 创建文件观察
type WatchResource struct {
	// MethodPost 登记观察参数。
	ginx.MethodPost
	// 请求体
	Body WatchRequest `in:"body"`
}

func (*WatchResource) Path() string { return "/watch" }
func (op *WatchResource) Output(ctx *gin.Context) (interface{}, error) {
	subject := middleware.AuthSubject(ctx)
	if err := files.ValidateScope(subject, op.Body.ScopeID); err != nil {
		return nil, err
	}
	id, err := registerWatch(middleware.AuthLifetime(ctx), subject, op.Body)
	if err != nil {
		return nil, err
	}
	return workspacefiles.ChangeEvent{SubscriptionID: id, Sequence: 0, State: "recovering"}, nil
}
func init() { Router.Register(&WatchResource{}); Router.Register(&UnwatchResource{}) }
