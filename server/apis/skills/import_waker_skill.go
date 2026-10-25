package skills

import (
	"context"
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

// 导入技能
type ImportWakerSkill struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body ImportSkillRequest `in:"body"`
}

// 导入技能请求
type ImportSkillRequest struct {
	// 批次令牌
	Token string `json:"token" validate:"required"`
	// 候选列表
	Candidates []InstallSkillCandidateRequest `json:"candidates" validate:"required,min=1,max=100,dive"`
}

// 安装候选请求
type InstallSkillCandidateRequest struct {
	// 标识
	ID string `json:"id" validate:"required"`
	// 版本号
	Version string `json:"version" validate:"required"`
	// 描述
	Description string `json:"description"`
}

// 预检导入
type preparedSkillImport struct {
	// 技能元数据
	skill models.SkillPackage
	// 版本记录
	version models.SkillVersion
	// 伙伴关系
	relation models.WakerSkill
}

// 选定导入
type selectedSkillImport struct {
	// 安装配置
	request InstallSkillCandidateRequest
	// 导入快照
	imported internalSkills.Package
}

// Path 返回技能导入接口路径。
func (i *ImportWakerSkill) Path() string { return "/:wakerId/skills/import" }

// Validate 校验候选数量、范围和版本格式。
func (i *ImportWakerSkill) Validate(*gin.Context) error {
	if len(i.Body.Candidates) == 0 || len(i.Body.Candidates) > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	seen := make(map[string]struct{}, len(i.Body.Candidates))
	for _, candidate := range i.Body.Candidates {

		if !isSemVer(candidate.Version) || strings.TrimSpace(candidate.ID) == "" {
			return ginx.WithStack(status_error.BadRequest)
		}
		if _, exists := seen[candidate.ID]; exists {
			return ginx.WithStack(status_error.BadRequest)
		}
		seen[candidate.ID] = struct{}{}
	}
	return nil
}

// Output 导入技能并为当前伙伴创建启用关系。
func (i *ImportWakerSkill) Output(ctx *gin.Context) (interface{}, error) {
	return importSkills(ctx, i)
}

// importSkills 复用相同资源用例，空伙伴范围表示全局库。
func importSkills(ctx *gin.Context, i *ImportWakerSkill) ([]models.SkillPackage, error) {
	if i.WakerID != "" {
		if _, err := global.WakerControllerManager.Get(ctx, i.WakerID); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	temporaryParent := filepath.Join(global.Config.DataDirectory, "skills", "tmp")
	batch, err := internalSkills.OpenStagedImport(temporaryParent, i.Body.Token)
	if err != nil {
		logx.Errorf(
			"open staged skill import failed: token=%s waker_id=%s error=%v",
			i.Body.Token,
			i.WakerID,
			err,
		)
		return nil, ginx.WithStack(status_error.SkillImportFailed)
	}
	packagesByID := make(map[string]internalSkills.Package, len(batch.Packages))
	for _, candidate := range batch.Packages {
		packagesByID[candidate.RelativePath] = candidate
	}
	selected := make([]selectedSkillImport, 0, len(i.Body.Candidates))
	seen := make(map[string]struct{}, len(i.Body.Candidates))
	for _, selection := range i.Body.Candidates {
		selection.ID = strings.TrimSpace(selection.ID)
		if selection.ID == "" || !isSemVer(selection.Version) {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		if _, exists := seen[selection.ID]; exists {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		seen[selection.ID] = struct{}{}
		imported, exists := packagesByID[selection.ID]
		if !exists {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		selected = append(selected, selectedSkillImport{request: selection, imported: imported})
	}
	prepared := make([]preparedSkillImport, 0, len(i.Body.Candidates))
	cleanupVersions := true
	defer func() {
		if !cleanupVersions {
			return
		}
		for _, item := range prepared {
			_ = internalSkills.RemoveVersion(global.Config.DataDirectory, item.version.ContentPath)
		}
	}()
	for _, candidate := range selected {
		selection := candidate.request
		scope := constants.SkillScopePrivate
		if i.WakerID == "" {
			scope = constants.SkillScopePublic
		}
		imported := candidate.imported
		packageID, versionID := util.New(), util.New()
		relative, digest, copyErr := internalSkills.CopyVersion(global.Config.DataDirectory, packageID, selection.Version, imported.Directory)
		if copyErr != nil {
			return nil, ginx.WithStack(copyErr)
		}
		description := strings.TrimSpace(selection.Description)
		if description == "" {
			description = imported.Description
		}
		skill := models.SkillPackage{
			ID: packageID, Name: imported.Name, Description: description,
			Scope: scope, Source: batch.Source.Kind,
			OwnerWakerID: i.WakerID, CurrentVersionID: versionID,
		}
		if skill.Source == constants.SkillSourceGit {
			skill.GitURL = batch.Source.Location
			skill.GitRef = batch.Source.Ref
			skill.GitSubpath = candidateGitSubpath(imported.RelativePath)
		}
		if scope == constants.SkillScopePublic {
			skill.OwnerWakerID = ""
		}
		prepared = append(prepared, preparedSkillImport{
			skill: skill,
			version: models.SkillVersion{
				ID: versionID, SkillPackageID: packageID, Version: selection.Version,
				Name: imported.Name, Description: description, ContentPath: relative,
				ContentDigest: digest, Operation: "import",
			},
			relation: models.WakerSkill{
				ID: util.New(), WakerID: i.WakerID, SkillPackageID: packageID, Enabled: true,
			},
		})
	}
	err = global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			for _, item := range prepared {
				if err := global.SkillControllerManager.CreatePackage(transactionContext, item.skill); err != nil {
					return err
				}
				if err := global.SkillControllerManager.CreateVersion(transactionContext, item.version, true, item.version.ID); err != nil {
					return err
				}
				if item.relation.WakerID != "" {
					if err := global.SkillControllerManager.UpsertWakerSkill(transactionContext, item.relation); err != nil {
						return err
					}
				}
			}
			return nil
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	cleanupVersions = false
	if err := os.RemoveAll(batch.Root); err != nil {
		logx.Errorf("remove installed skill batch failed: token=%s error=%v", batch.Token, err)
	}
	created := make([]models.SkillPackage, 0, len(prepared))
	for _, item := range prepared {
		created = append(created, item.skill)
	}
	notifyResourceChanged(ctx, "")
	return created, nil
}

func candidateGitSubpath(candidatePath string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(candidatePath)))
}

func isSemVer(value string) bool {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
