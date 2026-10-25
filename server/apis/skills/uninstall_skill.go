package skills

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
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

// Output 删除技能数据库事实和全部权威版本文件。
func (u *UninstallSkill) Output(ctx *gin.Context) (interface{}, error) {
	return removeSkill(ctx, u)
}

// removeSkill 复用相同资源用例，空伙伴范围表示全局库。
func removeSkill(ctx *gin.Context, u *UninstallSkill) (*ginx.CommonResponse, error) {
	skill, err := visibleSkill(ctx, u.WakerID, u.SkillID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if skill.BuiltIn {
		return nil, ginx.WithStack(status_error.BuiltInSkillCannotUninstall)
	}
	if skill.Scope == constants.SkillScopePublic && u.WakerID != "" {
		if err := global.SkillControllerManager.DeleteWakerSkill(ctx, u.WakerID, skill.ID); err != nil {
			return nil, ginx.WithStack(err)
		}
		notifyResourceChanged(ctx, u.SkillID)
		return ginx.CommonSuccessResponse(), nil
	}
	versions, err := global.SkillControllerManager.ListVersions(ctx, skill.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	err = global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			if u.WakerID == "" {
				counts, err := global.SkillControllerManager.InstalledCounts(transactionContext, []string{skill.ID})
				if err != nil {
					return err
				}
				if !u.Confirmed || counts[skill.ID] != u.ExpectedAffectedCount {
					return status_error.PluginConfigurationConflict
				}
			}
			return global.SkillControllerManager.DeletePackage(transactionContext, skill.ID)
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// Runtime 使用工作区软链接时，删除权威目录会让正在启动的调用明确失败；
	// 已经读取技能内容的 Agent 调用由 Runtime 自身完成收尾。
	for _, version := range versions {
		if err := managedskills.RemoveVersion(global.Config.DataDirectory, version.ContentPath); err != nil {
			logx.Errorf(
				"remove uninstalled skill version failed: skill_id=%s version_id=%s error=%v",
				skill.ID,
				version.ID,
				err,
			)
		}
	}
	notifyResourceChanged(ctx, u.SkillID)
	return ginx.CommonSuccessResponse(), nil
}
