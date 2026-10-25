package projects

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新项目
type UpdateProject struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 项目标识
	ProjectID string `in:"path" name:"projectId" validate:"required"`
	// 请求体
	Body ProjectInput `in:"body"`
}

// Path 返回项目更新接口路径。
func (u *UpdateProject) Path() string { return "/:projectId" }

// Validate 校验项目输入并规范化文本。
func (u *UpdateProject) Validate(*gin.Context) error {
	input, err := normalizeProjectInput(u.Body)
	if err != nil {
		return ginx.WithStack(err)
	}
	u.Body = input
	return nil
}

// Output 原子替换项目配置。
func (u *UpdateProject) Output(ctx *gin.Context) (interface{}, error) {
	project, err := global.ProjectControllerManager.Get(ctx, u.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if project.WakerID != u.WakerID {
		return nil, ginx.WithStack(status_error.ProjectNotFound.WithParams(
			map[string]interface{}{"ProjectID": u.ProjectID},
		))
	}
	directories, err := resolveProjectDirectories(u.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	now := time.Now().UnixMilli()
	project.Name = u.Body.Name
	project.Brief = u.Body.Brief
	project.UpdatedAt = now
	run := func() error {
		return global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
			for index := range directories {
				directories[index].Source.UpdatedAt = now
				if ensureErr := global.ProjectSourceControllerManager.Ensure(
					transactionContext,
					directories[index].Source,
				); ensureErr != nil {
					return ensureErr
				}
			}
			if updateErr := global.ProjectControllerManager.Update(transactionContext, project); updateErr != nil {
				return updateErr
			}
			if err := global.ProjectControllerManager.ReplaceDirectories(
				transactionContext,
				project.ID,
				directories,
			); err != nil {
				return err
			}
			return nil
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
	return ginx.CommonSuccessResponse(), nil
}
