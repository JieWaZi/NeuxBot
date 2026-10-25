package wakers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除伙伴请求
type DeleteWakerRequest struct {
	// 确认名称
	ConfirmationName string `json:"confirmationName" validate:"required"`
}

// 删除伙伴
type DeleteWaker struct {
	// MethodDelete 声明伙伴删除接口使用的 HTTP 方法。
	ginx.MethodDelete
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body DeleteWakerRequest `in:"body"`
}

// Path 返回删除伙伴接口路径。
func (d *DeleteWaker) Path() string {
	return "/:wakerId"
}

// Output 级联删除伙伴专属数据。
// @err[WakerHasRunningWork][40900000005][伙伴仍有运行中的工作]
func (d *DeleteWaker) Output(ctx *gin.Context) (interface{}, error) {
	if _, isDefault := builtinrolepreset.DefinitionByWakerID(d.WakerID); isDefault {
		return nil, ginx.WithStack(status_error.DefaultWakerCannotDelete)
	}
	waker, err := global.WakerControllerManager.Get(ctx, d.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if util.NormalizeName(d.Body.ConfirmationName) != waker.Name {
		return nil, ginx.WithStack(status_error.WakerDeleteConfirmationMismatch)
	}
	if global.ScheduleService != nil {
		release := global.ScheduleService.SealWaker(d.WakerID)
		defer release()
	}
	if global.ChannelService != nil {
		if err := global.ChannelService.ValidateWakerDetachment(ctx, d.WakerID); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	if err := global.ChatService.DeleteWakerChats(ctx, d.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.ChannelService != nil {
		if err := global.ChannelService.DetachWaker(ctx, d.WakerID); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	privateVersions, err := global.SkillControllerManager.ListPrivateVersionsByWaker(ctx, d.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 项目标识在库事务删除后即不可查，提前收集用于事务外的记忆级联清理。
	projects := []models.Project{}
	offset := 0
	for {
		page, err := global.ProjectControllerManager.ListByWaker(ctx, d.WakerID, "", 500, offset)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		projects = append(projects, page...)
		if len(page) < 500 {
			break
		}
		offset += len(page)
	}
	run := func(policyContext context.Context) error {
		return global.TransactionControllerManager.Transaction(
			policyContext,
			func(transactionContext context.Context) error {
				if global.AutomaticTaskControllerManager != nil {
					if err := global.AutomaticTaskControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
						return err
					}
				}
				if err := global.ProjectControllerManager.DeleteByWaker(
					transactionContext,
					d.WakerID,
				); err != nil {
					return err
				}
				if err := global.ProfileDocumentControllerManager.DeleteWakerDocuments(
					transactionContext,
					d.WakerID,
				); err != nil {
					return err
				}
				if err := global.SkillControllerManager.DeleteByWaker(
					transactionContext,
					d.WakerID,
				); err != nil {
					return err
				}
				if err := global.MCPConnectorControllerManager.DeleteByWaker(
					transactionContext,
					d.WakerID,
				); err != nil {
					return err
				}
				if global.PluginControllerManager != nil {
					if err := global.PluginControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
						return err
					}
				}
				return global.WakerControllerManager.Delete(transactionContext, d.WakerID)
			},
		)
	}
	change := func(policyContext context.Context) error {
		if global.KnowledgeService != nil {
			return global.KnowledgeService.WithPolicyWrite(func() error { return run(policyContext) })
		}
		return run(policyContext)
	}
	// 发布与聚合删除统一先协调集成授权，再协调知识关系；事务 Context 保留重入身份。
	if global.IntegrationService != nil {
		err = global.IntegrationService.WithAuthorizationWrite(ctx, change)
	} else {
		err = change(ctx)
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 伙伴删除后级联清理其伙伴记忆与全部项目记忆；记忆在独立 memory.db 中，
	// 不共享业务库事务，失败时记录日志并保留剩余数据，由下次启动的孤儿扫描重试。
	if global.MemoryService != nil {
		for _, project := range projects {
			if err := global.MemoryService.DeleteScope(
				ctx,
				constants.MemoryScopeProject,
				project.ID,
			); err != nil {
				logx.Errorf(
					"delete project memories failed: waker_id=%s project_id=%s error=%v",
					d.WakerID,
					project.ID,
					err,
				)
			}
		}
		if err := global.MemoryService.DeleteScope(
			ctx,
			constants.MemoryScopePartner,
			d.WakerID,
		); err != nil {
			logx.Errorf("delete waker memories failed: waker_id=%s error=%v", d.WakerID, err)
		}
	}
	for _, version := range privateVersions {
		if err := managedskills.RemoveVersion(global.Config.DataDirectory, version.ContentPath); err != nil {
			logx.Errorf(
				"remove deleted waker private skill version failed: waker_id=%s version_id=%s error=%v",
				d.WakerID,
				version.ID,
				err,
			)
		}
	}
	return ginx.CommonSuccessResponse(), nil
}
