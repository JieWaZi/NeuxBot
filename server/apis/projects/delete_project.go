package projects

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除项目
type DeleteProject struct {
	ginx.MethodDelete
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 项目标识
	ProjectID string `in:"path" name:"projectId" validate:"required"`
}

// Path 返回项目删除接口路径。
func (d *DeleteProject) Path() string { return "/:projectId" }

// Output 删除项目主体和目录关联配置。
func (d *DeleteProject) Output(ctx *gin.Context) (interface{}, error) {
	project, err := global.ProjectControllerManager.Get(ctx, d.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if project.WakerID != d.WakerID {
		return nil, ginx.WithStack(status_error.ProjectNotFound.WithParams(
			map[string]interface{}{"ProjectID": d.ProjectID},
		))
	}
	run := func() error {
		return global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
			return global.ProjectControllerManager.Delete(transactionContext, d.ProjectID)
		})
	}
	if global.KnowledgeService != nil {
		err = global.KnowledgeService.WithPolicyWrite(run)
	} else {
		err = run()
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 项目删除后级联清理项目记忆；跨库失败时保留数据并由下次启动的孤儿扫描重试。
	if global.MemoryService != nil {
		if err := global.MemoryService.DeleteScope(
			ctx,
			constants.MemoryScopeProject,
			d.ProjectID,
		); err != nil {
			logx.Errorf("delete project memories failed: project_id=%s error=%v", d.ProjectID, err)
		}
	}
	return ginx.CommonSuccessResponse(), nil
}
