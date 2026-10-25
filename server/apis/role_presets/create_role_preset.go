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

// 创建预设
type CreateRolePreset struct {
	ginx.MethodPost
	// 请求体
	Body RolePresetWriteRequest `in:"body"`
}

// Path 返回创建角色预设接口路径。
func (c *CreateRolePreset) Path() string { return "" }

// Output 创建角色预设和三份初始档案。
func (c *CreateRolePreset) Output(ctx *gin.Context) (interface{}, error) {
	var created models.RolePreset
	err := global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
		roleName := util.NormalizeName(c.Body.RoleName)
		occupied, err := global.RolePresetControllerManager.NameExists(
			transactionContext,
			roleName,
			"",
		)
		if err != nil {
			return err
		}
		if occupied {
			return status_error.RolePresetNameConflict
		}
		contents, err := resolveRolePresetContents(
			map[constants.ProfileDocumentKind]string{},
			c.Body,
		)
		if err != nil {
			return err
		}
		projection := profile.ParseDocuments(contents)
		now := time.Now().UnixMilli()
		avatar := strings.TrimSpace(c.Body.DefaultAvatar)
		if avatar == "" {
			avatar = constants.DefaultAvatar
		}
		created = models.RolePreset{
			ID:            util.New(),
			RoleName:      roleName,
			DefaultAvatar: avatar,
			Description:   strings.TrimSpace(c.Body.Description),
			Source:        constants.RolePresetSourceUser,
			Expertise:     projection.Expertise,
			WorkStyle:     projection.WorkStyle,
			WorkMethods:   projection.WorkMethods,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := global.RolePresetControllerManager.Create(transactionContext, created); err != nil {
			return err
		}
		documents := make([]models.RolePresetDocument, 0, 3)
		for _, kind := range profile.EditableKinds() {
			documents = append(documents, models.RolePresetDocument{
				ID:           util.New(),
				RolePresetID: created.ID,
				Kind:         kind,
				Content:      contents[kind],
				CreatedAt:    now,
				UpdatedAt:    now,
			})
		}
		return global.ProfileDocumentControllerManager.CreateRolePresetDocuments(
			transactionContext,
			documents,
		)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 新建伙伴弹窗需要新 ID 自动选中刚创建的角色预设，因此此处保留实体响应。
	return created, nil
}
