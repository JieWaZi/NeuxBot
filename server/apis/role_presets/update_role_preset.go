package role_presets

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新预设
type UpdateRolePreset struct {
	ginx.MethodPut
	// 预设标识
	RolePresetID string `in:"path" name:"rolePresetId" validate:"required"`
	// 请求体
	Body RolePresetWriteRequest `in:"body"`
}

// Path 返回更新角色预设接口路径。
func (u *UpdateRolePreset) Path() string { return "/:rolePresetId" }

// Output 更新用户角色预设和三份初始档案。
func (u *UpdateRolePreset) Output(ctx *gin.Context) (interface{}, error) {
	err := global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			preset, err := global.RolePresetControllerManager.Get(
				transactionContext,
				u.RolePresetID,
			)
			if err != nil {
				return err
			}
			if preset.Source != constants.RolePresetSourceUser {
				return status_error.SystemRolePresetReadOnly
			}
			roleName := util.NormalizeName(u.Body.RoleName)
			occupied, err := global.RolePresetControllerManager.NameExists(
				transactionContext,
				roleName,
				u.RolePresetID,
			)
			if err != nil {
				return err
			}
			if occupied {
				return status_error.RolePresetNameConflict
			}
			documents, err := global.ProfileDocumentControllerManager.
				ListRolePresetDocuments(transactionContext, u.RolePresetID)
			if err != nil {
				return err
			}
			existing, err := rolePresetDocumentContents(documents)
			if err != nil {
				return err
			}
			contents, err := resolveRolePresetContents(existing, u.Body)
			if err != nil {
				return err
			}
			projection := profile.ParseDocuments(contents)
			now := time.Now().UnixMilli()
			avatar := strings.TrimSpace(u.Body.DefaultAvatar)
			if avatar == "" {
				avatar = constants.DefaultAvatar
			}
			if err := global.RolePresetControllerManager.Update(
				transactionContext,
				u.RolePresetID,
				models.RolePreset{
					RoleName:      roleName,
					DefaultAvatar: avatar,
					Description:   strings.TrimSpace(u.Body.Description),
					Expertise:     projection.Expertise,
					WorkStyle:     projection.WorkStyle,
					WorkMethods:   projection.WorkMethods,
					UpdatedAt:     now,
				},
			); err != nil {
				return err
			}
			for _, kind := range profile.EditableKinds() {
				if err := global.ProfileDocumentControllerManager.UpdateRolePresetDocument(
					transactionContext,
					u.RolePresetID,
					kind,
					contents[kind],
					now,
				); err != nil {
					return err
				}
			}
			return nil
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
