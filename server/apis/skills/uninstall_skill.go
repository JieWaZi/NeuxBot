package skills

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/skilllearning"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 卸载技能
type UninstallSkill struct {
	ginx.MethodDelete
	// Confirmed 表示已确认全局资源删除。
	Confirmed bool `in:"query" name:"confirmed"`
	// ExpectedAffectedCount 是确认时实际安装伙伴数。
	ExpectedAffectedCount int64 `in:"query" name:"expectedAffectedCount"`
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
}

// Path 返回技能卸载接口路径。
func (u *UninstallSkill) Path() string { return "/:wakerId/skills/:skillId" }

// Output 仅移除伙伴安装；全局入口删除库资源及全部版本。
func (u *UninstallSkill) Output(ctx *gin.Context) (interface{}, error) {
	return removeSkill(ctx, u)
}

// removeSkill 复用相同资源用例，空伙伴范围表示全局库。
func removeSkill(ctx *gin.Context, u *UninstallSkill) (*ginx.CommonResponse, error) {
	config := skilllearning.Config{
		DataDirectory: global.Config.DataDirectory, Skills: global.SkillControllerManager, Wakers: global.WakerControllerManager,
		Transactions: global.TransactionControllerManager,
	}
	if global.EventHub != nil {
		config.Events = global.EventHub
	}
	err := skilllearning.New(config).Remove(ctx, skilllearning.Removal{
		WakerID: u.WakerID, SkillID: u.SkillID, DeletePackage: u.WakerID == "",
		Confirmed: u.Confirmed, ExpectedAffectedCount: u.ExpectedAffectedCount,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
