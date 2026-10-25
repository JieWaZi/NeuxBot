package skills

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 技能列表
type ListWakerSkills struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
}

// Path 返回伙伴技能列表接口路径。
func (l *ListWakerSkills) Path() string { return "/:wakerId/skills" }

// Output 返回技能列表。
func (l *ListWakerSkills) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, l.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	result, err := global.SkillControllerManager.ListVisible(ctx, l.WakerID, strings.TrimSpace(l.Query))
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	responses, err := wakerSkillResponses(ctx, result)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return responses, nil
}
