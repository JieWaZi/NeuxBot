package projects

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建项目
type CreateProject struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body ProjectInput `in:"body"`
}

// 创建项目响应
type CreateProjectResponse struct {
	// 项目标识
	ProjectID string `json:"projectId"`
}

// Path 返回项目创建接口路径。
func (c *CreateProject) Path() string { return "" }

// Validate 校验项目输入并规范化文本。
func (c *CreateProject) Validate(*gin.Context) error {
	input, err := normalizeProjectInput(c.Body)
	if err != nil {
		return ginx.WithStack(err)
	}
	c.Body = input
	return nil
}

// Output 原子创建项目与目录关联后返回新项目标识。
func (c *CreateProject) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, c.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	directories, err := resolveProjectDirectories(c.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	now := time.Now().UnixMilli()
	project := models.Project{
		ID:        util.New(),
		WakerID:   c.WakerID,
		Name:      c.Body.Name,
		Brief:     c.Body.Brief,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := func() error {
		return global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
			for index := range directories {
				directories[index].Source.CreatedAt = now
				directories[index].Source.UpdatedAt = now
				if ensureErr := global.ProjectSourceControllerManager.Ensure(
					transactionContext,
					directories[index].Source,
				); ensureErr != nil {
					return ensureErr
				}
			}
			if createErr := global.ProjectControllerManager.Create(transactionContext, project); createErr != nil {
				return createErr
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
	return CreateProjectResponse{ProjectID: project.ID}, nil
}
