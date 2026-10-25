package wakers

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新伙伴请求
type UpdateWakerRequest struct {
	// 名称
	Name string `json:"name" validate:"required,max=80"`
	// 角色名称
	RoleName string `json:"roleName" validate:"required,max=80"`
	// 头像
	Avatar string `json:"avatar" validate:"omitempty,max=2800000"`
	// 简介
	Bio string `json:"bio" validate:"omitempty,max=1000"`
	// 关于我
	Manual profile.ManualInput `json:"manual"`
}

// 更新伙伴
type UpdateWaker struct {
	// MethodPut 声明伙伴更新接口使用的 HTTP 方法。
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body UpdateWakerRequest `in:"body"`
}

// Path 返回更新伙伴接口路径。
func (u *UpdateWaker) Path() string {
	return "/:wakerId"
}

// Output 原子更新伙伴基础资料和关于我展示投影；关于我编辑不再改写档案 MD。
func (u *UpdateWaker) Output(ctx *gin.Context) (interface{}, error) {
	now := time.Now().UnixMilli()
	err := global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			waker, err := global.WakerControllerManager.Get(
				transactionContext,
				u.WakerID,
			)
			if err != nil {
				return err
			}
			name := util.NormalizeName(u.Body.Name)
			occupied, err := global.WakerControllerManager.NameExists(
				transactionContext,
				name,
				waker.ID,
			)
			if err != nil {
				return err
			}
			if occupied {
				return status_error.WakerNameConflict
			}
			avatar := strings.TrimSpace(u.Body.Avatar)
			if avatar == "" {
				avatar = waker.Avatar
			}
			if err := global.WakerControllerManager.Update(
				transactionContext,
				waker.ID,
				models.Waker{
					Name:      name,
					RoleName:  util.NormalizeName(u.Body.RoleName),
					Avatar:    avatar,
					Bio:       strings.TrimSpace(u.Body.Bio),
					UpdatedAt: now,
				},
			); err != nil {
				return err
			}
			return global.WakerControllerManager.UpdateProfileProjection(
				transactionContext,
				waker.ID,
				profile.ProjectionFromManual(u.Body.Manual),
				now,
			)
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
