package wakers

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
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
	layout, err := chatstorage.NewLayout(global.Config.ManagedFilesDirectory)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	deletions, err := chatstorage.NewDeletionCoordinator(layout)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	agentRoot, err := layout.AgentWorkspace(d.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	var staged *chatstorage.StagedDeletion
	var prepared bool
	// 文件与记忆结算仍持有伙伴操作门，防止旧删除清理同一身份的新副本。
	settle := func() error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, err := global.WakerControllerManager.Get(cleanup, d.WakerID)
		if util.HasStatusCode(err, status_error.WakerNotFound) {
			if global.ReviewWorker != nil {
				global.ReviewWorker.ObservePartnerDeletion(d.WakerID)
			}
			if global.MemoryService != nil {
				if err := global.MemoryService.DeletePartner(cleanup, d.WakerID); err != nil {
					// 独立记忆库清理失败时，由下次启动的孤儿扫描重试。
					logx.Errorf("delete waker memories failed: waker_id=%s error=%v", d.WakerID, err)
				}
			}
			if staged != nil {
				return staged.Commit()
			}
			return nil
		}
		if err != nil || staged == nil {
			return err
		}
		return staged.Rollback()
	}
	prepare := func(chatIDs []string) (func() error, error) {
		for _, chatID := range chatIDs {
			if global.AgentThreads != nil {
				if err := global.AgentThreads.CloseThread(ctx, chatID); err != nil {
					return nil, err
				}
			}
		}
		if global.ChannelService != nil {
			if err := global.ChannelService.DetachWaker(ctx, d.WakerID); err != nil {
				return nil, err
			}
		}
		if err := workspace.ValidateAgentWorkspaceDeletion(agentRoot, d.WakerID); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(agentRoot); os.IsNotExist(err) {
			prepared = true
			return nil, nil
		} else if err != nil {
			return nil, status_error.ManagedWorkspaceError
		}
		var err error
		staged, err = deletions.StageWaker(util.New(), d.WakerID)
		prepared = true
		return nil, err
	}
	run := func(transactionContext context.Context) error {
		if global.AutomaticTaskControllerManager != nil {
			if err := global.AutomaticTaskControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
				return err
			}
		}
		if err := global.ProjectControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
			return err
		}
		if err := global.ProfileDocumentControllerManager.DeleteWakerDocuments(transactionContext, d.WakerID); err != nil {
			return err
		}
		if err := global.SkillControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
			return err
		}
		if err := global.MCPConnectorControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
			return err
		}
		if global.PluginControllerManager != nil {
			if err := global.PluginControllerManager.DeleteByWaker(transactionContext, d.WakerID); err != nil {
				return err
			}
		}
		return global.WakerControllerManager.Delete(transactionContext, d.WakerID)
	}
	var releaseWaker func()
	defer func() {
		if releaseWaker != nil {
			releaseWaker()
		}
	}()
	deleteOwned := func(ctx context.Context) error {
		claimed, release, err := chatstorage.AcquireWakerDeletion(ctx, layout.Root(), d.WakerID)
		if err != nil {
			return err
		}
		releaseWaker = release
		return global.ChatService.DeleteWakerChats(claimed, d.WakerID, prepare, run)
	}
	change := func(policyContext context.Context) error {
		if global.KnowledgeService != nil {
			return global.KnowledgeService.WithPolicyWrite(func() error { return deleteOwned(policyContext) })
		}
		return deleteOwned(policyContext)
	}
	// 发布与聚合删除统一先协调集成授权，再协调知识关系；事务 Context 保留重入身份。
	if global.IntegrationService != nil {
		err = global.IntegrationService.WithAuthorizationWrite(ctx, change)
	} else {
		err = change(ctx)
	}
	if prepared {
		err = errors.Join(err, settle())
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.StorageService != nil {
		global.StorageService.Invalidate()
	}
	return ginx.CommonSuccessResponse(), nil
}
