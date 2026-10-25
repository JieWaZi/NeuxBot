package skills

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
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
	Name string `json:"name" validate:"required"`
}

// Path 返回技能展示名称更新接口路径。
func (u *UpdateWakerSkill) Path() string { return "/:wakerId/skills/:skillId" }

// Validate 校验展示名称格式。
func (u *UpdateWakerSkill) Validate(*gin.Context) error {
	if strings.TrimSpace(u.Body.Name) == "" || len(strings.TrimSpace(u.Body.Name)) > 120 {
		return ginx.WithStack(status_error.BadRequest)
	}
	u.Body.Name = strings.TrimSpace(u.Body.Name)
	return nil
}

// Output 保存技能展示名称，不修改 SKILL.md 元数据。
func (u *UpdateWakerSkill) Output(ctx *gin.Context) (interface{}, error) {
	return updateSkill(ctx, u)
}

// updateSkill 复用相同资源用例，空伙伴范围表示全局库。
func updateSkill(ctx *gin.Context, u *UpdateWakerSkill) (*ginx.CommonResponse, error) {
	if _, err := visibleSkill(ctx, u.WakerID, u.SkillID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.SkillControllerManager.UpdatePackageDisplayName(ctx, u.SkillID, u.Body.Name); err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, u.SkillID)
	return ginx.CommonSuccessResponse(), nil
}
