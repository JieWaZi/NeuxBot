package skills

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/resourcecategory"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新技能
type UpdateWakerSkill struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body UpdateWakerSkillRequest `in:"body"`
}

// 更新技能请求
type UpdateWakerSkillRequest struct {
	// 名称
	Name string `json:"name,omitempty"`
	// 分类
	Categories *[]string `json:"categories,omitempty"`
}

// Path 返回技能展示资料更新接口路径。
func (u *UpdateWakerSkill) Path() string { return "/:wakerId/skills/:skillId" }

// Validate 校验展示名称并规范化用户分类。
func (u *UpdateWakerSkill) Validate(*gin.Context) error {
	if u.Body.Name == "" && u.Body.Categories == nil {
		return ginx.WithStack(status_error.BadRequest)
	}
	if u.Body.Name != "" {
		u.Body.Name = util.NormalizeName(u.Body.Name)
		if u.Body.Name == "" || len(u.Body.Name) > 120 {
			return ginx.WithStack(status_error.BadRequest)
		}
	}
	if u.Body.Categories != nil {
		categories, err := resourcecategory.Normalize(*u.Body.Categories)
		if err != nil {
			return ginx.WithStack(err)
		}
		u.Body.Categories = &categories
	}
	return nil
}

// Output 保存技能展示名称与分类，不修改 SKILL.md 或版本。
func (u *UpdateWakerSkill) Output(ctx *gin.Context) (interface{}, error) {
	return updateSkill(ctx, u)
}

// updateSkill 复用相同资源用例，空伙伴范围表示全局库。
func updateSkill(ctx *gin.Context, u *UpdateWakerSkill) (*ginx.CommonResponse, error) {
	if _, err := resourceSkill(ctx, u.WakerID, u.SkillID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if u.Body.Categories != nil {
			if err := global.ResourceCategoryControllerManager.Ensure(txctx, constants.ResourceCategoryTypeSkill, *u.Body.Categories); err != nil {
				return err
			}
		}
		return global.SkillControllerManager.UpdatePackageMetadata(txctx, u.SkillID, u.Body.Name, u.Body.Categories)
	}); err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, u.SkillID)
	return ginx.CommonSuccessResponse(), nil
}
