package wakers

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建伙伴请求
type CreateWakerRequest struct {
	// 预设标识
	RolePresetID string `json:"rolePresetId" validate:"required"`
	// 名称
	Name string `json:"name" validate:"required,max=80"`
	// 头像
	Avatar string `json:"avatar" validate:"omitempty,max=2800000"`
	// 简介
	Bio string `json:"bio" validate:"omitempty,max=1000"`
}

// 创建伙伴
type CreateWaker struct {
	ginx.MethodPost
	// 请求体
	Body CreateWakerRequest `in:"body"`
}

// Path 返回创建伙伴接口路径。
func (c *CreateWaker) Path() string {
	return ""
}

// Output 从角色预设创建独立伙伴。
func (c *CreateWaker) Output(ctx *gin.Context) (interface{}, error) {
	var created models.Waker
	err := global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
		name := util.NormalizeName(c.Body.Name)
		occupied, err := global.WakerControllerManager.NameExists(
			transactionContext,
			name,
			"",
		)
		if err != nil {
			return err
		}
		if occupied {
			return status_error.WakerNameConflict
		}
		rolePresetID := strings.TrimSpace(c.Body.RolePresetID)
		preset, err := global.RolePresetControllerManager.Get(transactionContext, rolePresetID)
		if err != nil {
			return err
		}
		presetDocuments, err := global.ProfileDocumentControllerManager.
			ListRolePresetDocuments(transactionContext, preset.ID)
		if err != nil {
			return err
		}
		contents, err := builtinrolepreset.ResolveRolePresetContents(preset.ID, presetDocuments)
		if err != nil {
			return err
		}
		projection := profile.ParseDocuments(contents)
		now := time.Now().UnixMilli()
		avatar := strings.TrimSpace(c.Body.Avatar)
		if avatar == "" {
			avatar = constants.CompanionAvatarPrefix + util.New()
		}
		bio := strings.TrimSpace(c.Body.Bio)
		if bio == "" {
			bio = strings.TrimSpace(preset.Description)
		}
		created = models.Waker{
			ID:          util.New(),
			Name:        name,
			RoleName:    preset.RoleName,
			Avatar:      avatar,
			Bio:         bio,
			Expertise:   projection.Expertise,
			WorkStyle:   projection.WorkStyle,
			WorkMethods: projection.WorkMethods,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := global.WakerControllerManager.Create(transactionContext, created); err != nil {
			return err
		}
		documents := make([]models.WakerDocument, 0, len(profile.EditableKinds()))
		for _, kind := range profile.EditableKinds() {
			documents = append(documents, models.WakerDocument{
				ID:        util.New(),
				WakerID:   created.ID,
				Kind:      kind,
				Content:   contents[kind],
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
		if err := global.ProfileDocumentControllerManager.CreateWakerDocuments(
			transactionContext,
			documents,
		); err != nil {
			return err
		}
		if definition, builtin := builtinrolepreset.DefinitionByID(preset.ID); builtin {
			return builtinskills.EnableForWaker(
				transactionContext,
				global.SkillControllerManager,
				created.ID,
				definition.DefaultSkills,
			)
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
