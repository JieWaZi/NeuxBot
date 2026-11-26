package wakers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 复制伙伴请求
type DuplicateWakerRequest struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
}

// 复制伙伴响应
type DuplicateWakerResponse struct {
	// 伙伴标识
	WakerID string `json:"wakerId"`
}

// 复制伙伴
type DuplicateWaker struct {
	// MethodPost 声明伙伴复制接口使用的 HTTP 方法。
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body DuplicateWakerRequest `in:"body"`
}

// Path 返回伙伴复制接口路径。
func (*DuplicateWaker) Path() string { return "/:wakerId/duplicate" }

// Output 复制伙伴资料、记忆、空间和已安装技能，返回独立伙伴。
func (d *DuplicateWaker) Output(ctx *gin.Context) (interface{}, error) {
	identity, err := requestidempotency.New(d.Body.RequestID, d.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	id := identity.ResourceID("waker-copy")
	if err := copyWaker(ctx.Request.Context(), d.WakerID, id, identity.Hash); err != nil {
		return nil, ginx.WithStack(err)
	}
	return DuplicateWakerResponse{WakerID: id}, nil
}

func copyWaker(ctx context.Context, sourceID, id, requestHash string) error {
	if found, err := existingWakerCopy(ctx, id, requestHash); found || err != nil {
		return err
	}
	layout, err := chatstorage.NewLayout(global.Config.ManagedFilesDirectory)
	if err != nil {
		return err
	}
	sourceRoot, err := layout.AgentWorkspace(sourceID)
	if err != nil {
		return err
	}
	targetRoot, err := layout.AgentWorkspace(id)
	if err != nil {
		return err
	}
	// 与伙伴删除和新接纳共用操作门，同时独占副本身份与其可恢复的空间。
	release, err := chatstorage.AcquireOperation(layout.Root(), "waker:"+sourceID, "waker:"+id)
	if err != nil {
		return status_error.WakerHasRunningWork
	}
	defer release()
	if found, err := existingWakerCopy(ctx, id, requestHash); found || err != nil {
		return err
	}
	if _, err := global.WakerControllerManager.Get(ctx, sourceID); err != nil {
		return err
	}
	busy, err := global.ChatControllerManager.HasActiveWakerWork(ctx, sourceID)
	if err != nil {
		return err
	}
	if busy {
		return status_error.WakerHasRunningWork
	}
	if global.StorageService != nil {
		if err := global.StorageService.CheckFileIdle(ctx, sourceID, ""); err != nil {
			return err
		}
	}
	memories := global.MemoryService
	if memories == nil {
		return status_error.MemoryServiceUnavailable
	}
	if err := workspace.SettleAgentWorkspaceCopy(targetRoot, id, false); err != nil {
		return err
	}
	if global.StorageService != nil {
		defer global.StorageService.Invalidate()
	}
	if err := workspace.PrepareAgentWorkspaceCopy(ctx, sourceRoot, targetRoot, sourceID, id); err != nil {
		return err
	}
	rollback := func(ctx context.Context) error {
		return errors.Join(memories.DeletePartner(ctx, id), workspace.SettleAgentWorkspaceCopy(targetRoot, id, false))
	}
	if err := memories.DuplicatePartner(ctx, sourceID, id); err != nil {
		settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		return errors.Join(err, rollback(settle))
	}

	commitErr := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		return writeWakerCopy(tx, sourceID, id, requestHash)
	})
	if commitErr != nil {
		// 独立存储仅在业务库确认未提交时清理。查不到提交结果时由重试和启动恢复处理。
		settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		found, err := existingWakerCopy(settle, id, requestHash)
		if err != nil {
			return errors.Join(commitErr, err)
		}
		if !found {
			return errors.Join(commitErr, rollback(settle))
		}
	}
	if err := workspace.SettleAgentWorkspaceCopy(targetRoot, id, true); err != nil {
		logx.Warnf("finish committed partner workspace copy: waker_id=%s error=%v", id, err)
	}
	return nil
}

func existingWakerCopy(ctx context.Context, id, requestHash string) (bool, error) {
	stored, err := global.WakerControllerManager.Get(ctx, id)
	if util.HasStatusCode(err, status_error.WakerNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if stored.CreateRequestHash != requestHash {
		return false, status_error.CreateRequestConflict
	}
	return true, nil
}

func writeWakerCopy(ctx context.Context, sourceID, id, requestHash string) error {
	wakers := global.WakerControllerManager
	source, err := wakers.Get(ctx, sourceID)
	if err != nil {
		return err
	}
	name, err := duplicateWakerName(ctx, source.Name)
	if err != nil {
		return err
	}
	duplicate := models.Waker{
		ID: id, Name: name, RoleName: source.RoleName, Avatar: source.Avatar, Bio: source.Bio,
		DefaultModel: source.DefaultModel, Expertise: source.Expertise, WorkStyle: source.WorkStyle,
		WorkMethods: source.WorkMethods, CreateRequestHash: requestHash, NavigationGroupID: source.NavigationGroupID,
		OnboardingStatus: constants.WakerOnboardingStatusReady,
	}
	if source.OnboardingStatus == constants.WakerOnboardingStatusPending {
		duplicate.OnboardingStatus = constants.WakerOnboardingStatusPending
		duplicate.OnboardingChatID = requestidempotency.WakerOnboardingChatID(id)
	}
	if err := wakers.Create(ctx, duplicate); err != nil {
		return err
	}
	if err := wakers.PlaceWakerInNavigation(ctx, id, source.ID, constants.WakerNavigationPlacementAfter); err != nil {
		return err
	}
	profiles := global.ProfileDocumentControllerManager
	rows, err := profiles.ListWakerDocuments(ctx, source.ID)
	if err != nil {
		return err
	}
	document := builtinrolepreset.ResolveWakerAgents(source.ID, rows, source.UpdatedAt)
	if document.Content != "" {
		if err := profiles.SaveWakerAgents(ctx, id, document.Content, document.UpdatedAt); err != nil {
			return err
		}
	}
	return global.SkillControllerManager.DuplicateWakerSkills(ctx, sourceID, id)
}

func duplicateWakerName(ctx context.Context, name string) (string, error) {
	runes := []rune(name)
	for index := 2; ; index++ {
		suffix := fmt.Sprintf(" (%d)", index)
		candidate := strings.TrimRight(string(runes[:min(len(runes), 80-len(suffix))]), " ") + suffix
		occupied, err := global.WakerControllerManager.NameExists(ctx, candidate, "")
		if err != nil || !occupied {
			return candidate, err
		}
	}
}
