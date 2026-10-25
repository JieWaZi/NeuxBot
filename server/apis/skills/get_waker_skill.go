package skills

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取技能
type GetWakerSkill struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
}

// Path 返回技能详情接口路径。
func (g *GetWakerSkill) Path() string { return "/:wakerId/skills/:skillId" }

// Output 返回技能详情。
func (g *GetWakerSkill) Output(ctx *gin.Context) (interface{}, error) {
	return getSkill(ctx, g)
}

// getSkill 复用相同资源用例，空伙伴范围表示全局库。
func getSkill(ctx *gin.Context, g *GetWakerSkill) (WakerSkillResponse, error) {
	skill, visibleErr := visibleSkill(ctx, g.WakerID, g.SkillID)
	if visibleErr != nil {
		return WakerSkillResponse{}, ginx.WithStack(visibleErr)
	}
	if g.WakerID == "" {
		item := models.SkillWithState{SkillPackage: skill}
		if skill.CurrentVersionID != "" {
			version, err := global.SkillControllerManager.GetVersion(ctx, skill.CurrentVersionID)
			if err != nil {
				return WakerSkillResponse{}, ginx.WithStack(err)
			}
			item.CurrentVersion = version
		}
		responses, err := wakerSkillResponses(ctx, []models.SkillWithState{item})
		if err != nil {
			return WakerSkillResponse{}, ginx.WithStack(err)
		}
		return responses[0], nil
	}
	if _, err := visibleSkill(ctx, g.WakerID, g.SkillID); err != nil {
		return WakerSkillResponse{}, ginx.WithStack(err)
	}
	items, err := global.SkillControllerManager.ListVisible(ctx, g.WakerID, "")
	if err != nil {
		return WakerSkillResponse{}, ginx.WithStack(err)
	}
	for _, item := range items {
		if item.SkillPackage.ID == g.SkillID {
			responses, responseErr := wakerSkillResponses(ctx, []models.SkillWithState{item})
			if responseErr != nil {
				return WakerSkillResponse{}, ginx.WithStack(responseErr)
			}
			return responses[0], nil
		}
	}
	return WakerSkillResponse{}, ginx.WithStack(status_error.SkillNotFound)
}
