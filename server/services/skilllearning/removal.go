package skilllearning

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Removal 定义卸载或删除技能包时需要核对的作用域。
type Removal struct {
	// WakerID 是来源伙伴，资源库入口留空。
	WakerID string
	// SkillID 是技能包标识。
	SkillID string
	// DeletePackage 删除整个技能包及版本，未设置时仅解除安装。
	DeletePackage bool
	// Confirmed 是全局资源页面提交的删除确认。
	Confirmed bool
	// ExpectedAffectedCount 是全局资源页面确认时的安装关系数。
	ExpectedAffectedCount int64
	// ExpectedVersionID 是聊天确认时技能的当前版本。
	ExpectedVersionID string
	// ConfirmedPartnerIDs 是聊天确认时的完整安装伙伴集合。
	ConfirmedPartnerIDs []string
}

// Remove 解除安装或删除库资源，供对话和资源管理页使用。
func (s *Service) Remove(ctx context.Context, input Removal) error {
	var versions []models.SkillVersion
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		if !input.DeletePackage {
			return s.uninstall(ctx, input)
		}
		version, err := s.confirmPackageDeletion(ctx, input)
		if err != nil {
			return err
		}
		versions, err = s.skills.ListVersions(ctx, input.SkillID)
		if err != nil {
			return err
		}
		if err := s.skills.DeletePackage(ctx, input.SkillID); err != nil {
			return err
		}
		return capability.Commit(ctx, skillVersionResult(input.SkillID, version))
	})
	if err != nil {
		return err
	}
	for _, version := range versions {
		if err := skills.RemoveVersion(s.dataDirectory, version.ContentPath); err != nil {
			logx.Errorf("remove uninstalled skill version failed: skill_id=%s version_id=%s error=%v", input.SkillID, version.ID, err)
		}
	}
	s.emitResourceChange(ctx, input.SkillID)
	return nil
}

func (s *Service) uninstall(ctx context.Context, input Removal) error {
	if _, err := s.wakers.Get(ctx, input.WakerID); err != nil {
		return err
	}
	item, err := s.skills.GetInstalled(ctx, input.WakerID, input.SkillID)
	if err != nil {
		return err
	}
	if capability.Active(ctx) && (item.SkillPackage.BuiltIn || !item.SkillPackage.Source.RevisableByLearning()) {
		return status_error.BadRequest
	}
	if input.ExpectedVersionID != "" && item.CurrentVersion.ID != input.ExpectedVersionID {
		return status_error.Conflict
	}
	if err := s.skills.DeleteWakerSkill(ctx, input.WakerID, input.SkillID); err != nil {
		return err
	}
	return capability.Commit(ctx, skillVersionResult(input.SkillID, item.CurrentVersion))
}

func (s *Service) confirmPackageDeletion(ctx context.Context, input Removal) (models.SkillVersion, error) {
	if input.WakerID != "" {
		_, version, partners, err := s.revisionState(ctx, input.WakerID, input.SkillID, input.ExpectedVersionID)
		if err != nil {
			return models.SkillVersion{}, err
		}
		_, err = confirmedScope(partners, input.ConfirmedPartnerIDs)
		return version, err
	}
	skill, err := s.skills.GetPackage(ctx, input.SkillID)
	if err != nil {
		return models.SkillVersion{}, err
	}
	if skill.BuiltIn {
		return models.SkillVersion{}, status_error.BuiltInSkillCannotUninstall
	}
	counts, err := s.skills.InstalledCounts(ctx, []string{skill.ID})
	if err != nil {
		return models.SkillVersion{}, err
	}
	if !input.Confirmed || counts[skill.ID] != input.ExpectedAffectedCount {
		return models.SkillVersion{}, status_error.PluginConfigurationConflict
	}
	return models.SkillVersion{}, nil
}
