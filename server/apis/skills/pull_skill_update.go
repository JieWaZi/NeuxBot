package skills

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	internalSkills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

var importSkillSource = internalSkills.Import

// 拉取更新
type PullSkillUpdate struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body PullSkillUpdateRequest `in:"body"`
}

// 拉取更新请求
type PullSkillUpdateRequest struct {
	// 版本号
	Version string `json:"version" validate:"required"`
	// 版本备注
	Notes string `json:"notes"`
}

// Path 返回 Git 拉取更新接口路径。
func (p *PullSkillUpdate) Path() string { return "/:wakerId/skills/:skillId/pull" }

// Validate 校验新版本号格式。
func (p *PullSkillUpdate) Validate(*gin.Context) error {
	if !isSemVer(p.Body.Version) {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 拉取 Git 来源并写入新的当前版本。
func (p *PullSkillUpdate) Output(ctx *gin.Context) (interface{}, error) {
	return pullSkillUpdate(ctx, p)
}

// pullSkillUpdate 复用相同资源用例，空伙伴范围表示全局库。
func pullSkillUpdate(ctx *gin.Context, p *PullSkillUpdate) (models.SkillVersion, error) {
	skill, err := visibleSkill(ctx, p.WakerID, p.SkillID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	if skill.Source != constants.SkillSourceGit || strings.TrimSpace(skill.GitURL) == "" {
		return models.SkillVersion{}, ginx.WithStack(status_error.BadRequest)
	}
	current, err := global.SkillControllerManager.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	if compareSemVer(p.Body.Version, current.Version) <= 0 {
		return models.SkillVersion{}, ginx.WithStack(status_error.BadRequest)
	}
	packages, root, err := importSkillSource(
		ctx,
		filepath.Join(global.Config.DataDirectory, "skills", "tmp"),
		internalSkills.Source{Kind: constants.SkillSourceGit, Location: skill.GitURL, Ref: skill.GitRef, Subpath: skill.GitSubpath},
	)
	if err != nil {
		logx.Errorf(
			"pull skill source failed: waker_id=%s skill_id=%s git_subpath=%s error=%v",
			p.WakerID,
			skill.ID,
			skill.GitSubpath,
			err,
		)
		return models.SkillVersion{}, ginx.WithStack(status_error.SkillImportFailed)
	}
	defer os.RemoveAll(root)
	expectedSubpath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(skill.GitSubpath)))
	var selected *internalSkills.Package
	for index := range packages {
		candidateSubpath := filepath.ToSlash(filepath.Clean(
			filepath.FromSlash(packages[index].RelativePath),
		))
		if candidateSubpath == expectedSubpath {
			selected = &packages[index]
			break
		}
	}
	if selected == nil {
		return models.SkillVersion{}, ginx.WithStack(status_error.BadRequest)
	}
	versionID := util.New()
	contentPath, digest, err := internalSkills.CopyVersion(global.Config.DataDirectory, skill.ID, p.Body.Version, selected.Directory)
	if err != nil {
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	if digest == current.ContentDigest {
		_ = internalSkills.RemoveVersion(global.Config.DataDirectory, contentPath)
		return current, nil
	}
	version := models.SkillVersion{ID: versionID, SkillPackageID: skill.ID, Version: strings.TrimSpace(p.Body.Version), Name: selected.Name, Description: selected.Description, Operation: "pull", Notes: p.Body.Notes, ContentPath: contentPath, ContentDigest: digest}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, current.ID); err != nil {
		_ = internalSkills.RemoveVersion(global.Config.DataDirectory, contentPath)
		return models.SkillVersion{}, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, p.SkillID)
	return version, nil
}
