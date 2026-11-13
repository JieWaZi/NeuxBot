package wakers

import (
	"context"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
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
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body DuplicateWakerRequest `in:"body"`
}

// Path 返回伙伴复制接口路径。
func (*DuplicateWaker) Path() string { return "/:wakerId/duplicate" }

// Output 原子复制伙伴资料和档案到独立伙伴。
func (d *DuplicateWaker) Output(ctx *gin.Context) (interface{}, error) {
	identity, err := requestidempotency.New(d.Body.RequestID, d.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	id := identity.ResourceID("waker-copy")
	err = global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		wakers := global.WakerControllerManager
		stored, err := wakers.Get(tx, id)
		if err == nil {
			if stored.CreateRequestHash != identity.Hash {
				return status_error.CreateRequestConflict
			}
			return nil
		}
		if !util.HasStatusCode(err, status_error.WakerNotFound) {
			return err
		}
		source, err := wakers.Get(tx, d.WakerID)
		if err != nil {
			return err
		}
		name, err := duplicateWakerName(tx, source.Name)
		if err != nil {
			return err
		}
		duplicate := models.Waker{
			ID: id, Name: name, RoleName: source.RoleName, Avatar: source.Avatar, Bio: source.Bio,
			DefaultModel: source.DefaultModel, Expertise: source.Expertise, WorkStyle: source.WorkStyle,
			WorkMethods: source.WorkMethods, CreateRequestHash: identity.Hash,
			OnboardingStatus: constants.WakerOnboardingStatusReady,
		}
		if source.OnboardingStatus == constants.WakerOnboardingStatusPending {
			duplicate.OnboardingStatus = constants.WakerOnboardingStatusPending
			duplicate.OnboardingChatID = requestidempotency.WakerOnboardingChatID(id)
		}
		if err := wakers.Create(tx, duplicate); err != nil {
			return err
		}
		profiles := global.ProfileDocumentControllerManager
		rows, err := profiles.ListWakerDocuments(tx, source.ID)
		if err != nil {
			return err
		}
		document := builtinrolepreset.ResolveWakerAgents(source.ID, rows, source.UpdatedAt)
		if document.Content == "" {
			return nil
		}
		return profiles.SaveWakerAgents(tx, id, document.Content, document.UpdatedAt)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return DuplicateWakerResponse{WakerID: id}, nil
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
