package skills

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 启停技能
type SetWakerSkillEnabled struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body SetSkillEnabledRequest `in:"body"`
}

// 启停技能请求
type SetSkillEnabledRequest struct {
	// 启用状态
	Enabled bool `json:"enabled"`
}

// Path 返回技能启停接口路径。
func (s *SetWakerSkillEnabled) Path() string { return "/:wakerId/skills/:skillId/enabled" }

// Output 保存技能启停关系。
func (s *SetWakerSkillEnabled) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, strings.TrimSpace(s.WakerID)); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := visibleSkill(ctx, strings.TrimSpace(s.WakerID), strings.TrimSpace(s.SkillID)); err != nil {
		return nil, ginx.WithStack(err)
	}
	err := global.SkillControllerManager.UpsertWakerSkill(ctx, models.WakerSkill{ID: util.New(), WakerID: s.WakerID, SkillPackageID: s.SkillID, Enabled: s.Body.Enabled})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, s.SkillID)
	return ginx.CommonSuccessResponse(), nil
}
