package wakers

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建伙伴请求
type CreateWakerRequest struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 名称
	Name string `json:"name" validate:"required,max=80"`
	// 头像
	Avatar string `json:"avatar,omitempty" validate:"max=2800000"`
	// 概要
	Bio string `json:"bio,omitempty" validate:"max=1000"`
	// 默认模型
	DefaultModel *models.WakerDefaultModel `json:"defaultModel,omitempty"`
}

// 创建伙伴
type CreateWaker struct {
	// MethodPost 声明伙伴创建方法。
	ginx.MethodPost
	// 请求体
	Body CreateWakerRequest `in:"body"`
}

// Path 返回创建伙伴接口路径。
func (c *CreateWaker) Path() string { return "" }

// Output 先持久化伙伴，再接纳建档对话。
func (c *CreateWaker) Output(ctx *gin.Context) (interface{}, error) {
	body := c.Body
	body.Name = util.NormalizeName(body.Name)
	body.Avatar = strings.TrimSpace(body.Avatar)
	body.Bio = strings.TrimSpace(body.Bio)
	if body.Name == "" {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	var err error
	body.DefaultModel, err = normalizeDefaultModel(body.DefaultModel)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	requestID := body.RequestID
	body.RequestID = ""
	identity, err := requestidempotency.New(requestID, body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	id := identity.ResourceID("waker")
	var waker models.Waker
	err = global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		stored, err := global.WakerControllerManager.Get(tx, id)
		if err == nil {
			if stored.CreateRequestHash != identity.Hash {
				return status_error.CreateRequestConflict
			}
			waker = stored
			return nil
		}
		if !util.HasStatusCode(err, status_error.WakerNotFound) {
			return err
		}
		avatar := body.Avatar
		if avatar == "" {
			avatar = constants.CompanionAvatarPrefix + util.New()
		}
		waker = models.Waker{
			ID:                id,
			Name:              body.Name,
			Avatar:            avatar,
			Bio:               body.Bio,
			DefaultModel:      body.DefaultModel,
			CreateRequestHash: identity.Hash,
			OnboardingStatus:  constants.WakerOnboardingStatusPending,
			OnboardingChatID:  requestidempotency.WakerOnboardingChatID(id),
			Expertise:         []profile.Expertise{},
			WorkStyle:         []string{},
			WorkMethods:       []profile.WorkMethod{},
		}
		return global.WakerControllerManager.Create(tx, waker)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return startWakerOnboarding(ctx, waker, false), nil
}

func normalizeDefaultModel(input *models.WakerDefaultModel) (*models.WakerDefaultModel, error) {
	if input == nil {
		return nil, nil
	}
	value := models.WakerDefaultModel{
		RuntimeID: strings.TrimSpace(input.RuntimeID),
		AccountID: strings.TrimSpace(input.AccountID),
		Model:     strings.TrimSpace(input.Model),
		Reasoning: strings.TrimSpace(input.Reasoning),
	}
	if value == (models.WakerDefaultModel{}) {
		return nil, nil
	}
	if value.RuntimeID == "" || value.AccountID == "" || value.Model == "" {
		return nil, status_error.BadRequest
	}
	return &value, nil
}
