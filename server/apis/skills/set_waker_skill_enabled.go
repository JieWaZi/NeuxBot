package skills

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
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
// @err[SkillNotFound][40400000007][技能未安装]
func (s *SetWakerSkillEnabled) Output(ctx *gin.Context) (interface{}, error) {
	wakerID, skillID := strings.TrimSpace(s.WakerID), strings.TrimSpace(s.SkillID)
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.SkillControllerManager.SetWakerSkillEnabled(ctx, wakerID, skillID, s.Body.Enabled); err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, skillID)
	return ginx.CommonSuccessResponse(), nil
}
