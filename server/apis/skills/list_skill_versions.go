package skills

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 版本列表
type ListSkillVersions struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
}

// Path 返回技能版本列表接口路径。
func (l *ListSkillVersions) Path() string { return "/:wakerId/skills/:skillId/versions" }

// Output 返回技能版本列表。
func (l *ListSkillVersions) Output(ctx *gin.Context) (interface{}, error) {
	return listSkillVersions(ctx, l)
}

// listSkillVersions 复用相同资源用例，空伙伴范围表示全局库。
func listSkillVersions(ctx *gin.Context, l *ListSkillVersions) ([]models.SkillVersion, error) {
	if _, err := visibleSkill(ctx, l.WakerID, l.SkillID); err != nil {
		return nil, ginx.WithStack(err)
	}
	versions, err := global.SkillControllerManager.ListVersions(ctx, l.SkillID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return versions, nil
}
